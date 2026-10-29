package agent

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func (r *Resources) restoreForwards(ctx context.Context, l sdk.Lifecycle) error {
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	snapshot, err := r.store.View()
	if err != nil {
		return stateError(err)
	}
	for id := range snapshot.Buckets["forwards"] {
		spec, err := forwardSpec(snapshot, id)
		if err != nil {
			return err
		}
		if !spec.Enabled {
			if err = r.finishStoppedForward(id); err != nil {
				return err
			}
			continue
		}
		if !spec.Automatic {
			r.applyForward(ctx, spec, l)
		}
	}
	return nil
}

// Idle listeners have no open CoreAPI stream to observe route invalidation.
// The authenticated connection monitor therefore also stops their runtimes,
// retaining enabled intent for a later confirmed route generation.
func (r *Resources) suspendForwards(ctx context.Context, server string, cause error) {
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	states, err := r.service.ListForwards(ctx, &pb.Empty{})
	if err != nil {
		return
	}
	r.suspendForwardStates(ctx, server, cause, states.Forwards)
}

// The caller holds forwardMu and supplies the observed worker states.
func (r *Resources) suspendForwardStates(ctx context.Context, server string, cause error, states []*ppb.ForwardState) {
	for _, state := range states {
		if state.Spec.ServerId != server || state.BoundPort == 0 {
			continue
		}
		r.mu.Lock()
		current := r.pools[server]
		preserve := current != nil && r.forwardRoutes[state.Spec.Id] == current && current.state == "ready" && !current.bad.Load() && current.ctx.Err() == nil
		r.mu.Unlock()
		if preserve {
			// A failed probe is not necessarily a retired route. Also avoid
			// suspending a replacement listener for an older failure generation.
			continue
		}
		if r.stopForwardRuntime(ctx, state.Spec.Id) != nil {
			continue
		}
		state.BoundPort, state.Connections = 0, 0
		state.State, state.ReasonCode = "offline", "REMOTE_UNAVAILABLE"
		if connect.CodeOf(cause) == connect.CodeUnauthenticated {
			state.State, state.ReasonCode = "auth_required", "REMOTE_AUTH_REQUIRED"
		}
		r.forwardStates.Store(state.Spec.Id, state)
		delete(r.forwardRoutes, state.Spec.Id)
		r.publish("forward", state.Spec.Id, server, state.Spec.Revision)
	}
}

// A new authenticated route may restore desired listeners. It never reuses an
// old stream, connection ID, buffered bytes, or application's TCP session.
func (r *Resources) reconcileForwards(ctx context.Context, server string) {
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	r.mu.Lock()
	route := r.pools[server]
	var client *connect.Client
	if route != nil {
		client = route.client
	}
	ready := route != nil && client != nil && !route.bad.Load() && route.state == "ready"
	r.mu.Unlock()
	if !ready {
		return
	}
	states, err := r.service.ListForwards(ctx, &pb.Empty{})
	if err != nil {
		return
	}
	sessionErrors := map[string]error{}
	for _, state := range states.Forwards {
		spec := state.Spec
		if spec.ServerId != server {
			continue
		}
		if !spec.Enabled {
			if r.stopForwardRuntime(ctx, spec.Id) == nil {
				_ = r.finishStoppedForward(spec.Id)
			}
			continue
		}
		if spec.Automatic {
			continue
		}
		if spec.SessionId != "" {
			sessionErr, checked := sessionErrors[spec.SessionId]
			if !checked {
				check, stop := context.WithTimeout(ctx, 5*time.Second)
				session, err := rpc.NewShikiDaemonClient(client).GetSession(FreshRemoteContext(check), &pb.GetSessionRequest{Id: spec.SessionId})
				stop()
				sessionErr = err
				if err == nil && (session == nil || session.Id != spec.SessionId) {
					sessionErr = rpcError(connect.CodeFailedPrecondition, "SESSION_ID_MISMATCH")
				}
				sessionErrors[spec.SessionId] = sessionErr
			}
			if connect.CodeOf(sessionErr) == connect.CodeNotFound {
				_ = r.disableDeletedSessionForward(ctx, spec)
				continue
			}
			if sessionErr != nil {
				continue
			}
		}
		if route.bad.Load() {
			return
		}
		if state.BoundPort != 0 {
			continue
		}
		if state.ReasonCode == "FORWARD_BIND_FAILED" || state.ReasonCode == "REMOTE_TARGET_DENIED" {
			continue
		}
		// A graceful daemon stop can cancel an individual stream without
		// invalidating its authenticated HTTP/2 pool. After Resolve has verified
		// that route again, a completed stream failure must not permanently
		// suppress the desired listener. Rebinding does not open a remote stream:
		// only a fresh application connection can do that, with fresh admission.
		// Keep other failures limited to one attempt per route generation.
		if r.forwardRoutes[spec.Id] == route && state.ReasonCode != "FORWARD_STREAM_FAILED" {
			continue
		}
		r.applyForward(ctx, spec, nil)
		r.publish("forward", spec.Id, spec.ServerId, spec.Revision)
	}
}

func (c *pluginCore) authorizePort(scope *ppb.Scope) error {
	if scope == nil || scope.ServerId == "" || scope.ProjectId != "" || scope.MappingId != "" {
		return rpcError(connect.CodePermissionDenied, "PORT_SCOPE_DENIED")
	}
	if manager := c.resources.service.plugins.Load(); manager != nil {
		if values, _, e := manager.Settings("port"); e == nil {
			if cfg, e := schema.Parse(values, "client"); e == nil && cfg.AutoForward {
				snapshot, e := c.resources.store.View()
				if e != nil {
					return stateError(e)
				}
				if _, e = storedServer(snapshot, scope.ServerId); e == nil {
					return nil
				}
			}
		}
	}
	snapshot, err := c.resources.store.View()
	if err != nil {
		return stateError(err)
	}
	for id := range snapshot.Buckets["forwards"] {
		spec, err := forwardSpec(snapshot, id)
		if err != nil {
			return err
		}
		if spec.Enabled && spec.ServerId == scope.ServerId && spec.SessionId == scope.SessionId {
			return nil
		}
	}
	return rpcError(connect.CodePermissionDenied, "PORT_SCOPE_DENIED")
}
func (c *pluginCore) authorizeForward(ctx context.Context, scope *ppb.Scope, frame *pb.PortData) error {
	if c.id != "port" || frame == nil || frame.Type != pb.PortData_CONNECT {
		return rpcError(connect.CodePermissionDenied, "PORT_SCOPE_DENIED")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.authorizePort(scope); err != nil {
		return err
	}
	snapshot, err := c.resources.store.View()
	if err != nil {
		return stateError(err)
	}
	for id := range snapshot.Buckets["forwards"] {
		spec, err := forwardSpec(snapshot, id)
		if err != nil {
			return err
		}
		if spec.Enabled && spec.ServerId == scope.ServerId && spec.SessionId == scope.SessionId && spec.RemoteAddress == frame.RemoteAddress && int32(spec.RemotePort) == frame.Port && spec.SessionId == frame.SessionId && spec.Automatic == frame.Automatic && spec.SourceIdentity == frame.SourceIdentity {
			if spec.Automatic {
				manager := c.resources.service.plugins.Load()
				if manager == nil {
					return unavailableWorker()
				}
				values, _, e := manager.Settings("port")
				if e != nil {
					return e
				}
				cfg, e := schema.Parse(values, "client")
				if e != nil || !cfg.AutomaticAllowed(spec.RemotePort) {
					return rpcError(connect.CodePermissionDenied, "AUTOMATIC_POLICY_CHANGED")
				}
			}
			if spec.SessionId != "" && !spec.Automatic {
				check, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				client, err := c.resources.Resolve(check, spec.ServerId)
				if err != nil {
					return err
				}
				session, err := rpc.NewShikiDaemonClient(client).GetSession(FreshRemoteContext(check), &pb.GetSessionRequest{Id: spec.SessionId})
				if err != nil {
					return safeRemoteError(err)
				}
				if session == nil || session.Id != spec.SessionId {
					return rpcError(connect.CodePermissionDenied, "PORT_SESSION_UNAVAILABLE")
				}
			}
			return nil
		}
	}
	return rpcError(connect.CodePermissionDenied, "PORT_ENDPOINT_NOT_GRANTED")
}

func (r *Resources) disableDeletedSessionForward(ctx context.Context, spec *ppb.ForwardSpec) error {
	r.mu.Lock()
	err := r.commitForward(func(snapshot *storage.Snapshot) error {
		current, e := forwardSpec(snapshot, spec.Id)
		if e != nil {
			return e
		}
		current.Enabled = false
		current.Revision = newID("rev_")
		return snapshot.Put("forwards", current.Id, current)
	})
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err = r.stopForwardRuntime(ctx, spec.Id); err != nil {
		return err
	}
	return r.finishStoppedForward(spec.Id)
}
