package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

func automaticKey(spec *ppb.ForwardSpec) string {
	return fmt.Sprintf("%s:%x", spec.ServerId, sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", spec.SessionId, spec.RemoteAddress, spec.RemotePort))))
}
func sameForwardEndpoint(a, b *ppb.ForwardSpec) bool {
	return a.ServerId == b.ServerId && a.RemoteAddress == b.RemoteAddress && a.RemotePort == b.RemotePort
}

// The worker owns discovery/policy. The host owns durable authorization and
// reconciles only a complete, successful candidate snapshot.
func (r *Resources) reconcileAutomaticForwards(ctx context.Context, server string) {
	manager := r.service.plugins.Load()
	if manager == nil {
		return
	}
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	check, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var candidates *ppb.ForwardCandidates
	values, _, err := manager.Settings("port")
	cfg, parseErr := schema.Parse(values, "client")
	if err == nil {
		err = parseErr
	}
	var remote rpc.ShikiDaemonClient
	if err == nil && cfg.AutoForward {
		client, e := r.Resolve(check, server)
		err = e
		if e == nil {
			remote = rpc.NewShikiDaemonClient(client)
			agreement, e := remote.NegotiateProtocol(FreshRemoteContext(check), &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.PortAutoCapability, sdk.PortCheckCapability}, RequiredCapabilities: []string{sdk.PortAutoCapability, sdk.PortCheckCapability}})
			err = e
			if e == nil && (agreement == nil || agreement.Major != 1 || !slices.Contains(agreement.Capabilities, sdk.PortAutoCapability) || !slices.Contains(agreement.Capabilities, sdk.PortCheckCapability)) {
				err = rpcError(connect.CodeFailedPrecondition, "AUTOMATIC_CAPABILITY_UNAVAILABLE")
			}
		}
	}
	if err == nil {
		err = manager.Call(check, "port", sdk.PortAutoCapability, func(call context.Context, l sdk.Lifecycle) error {
			var e error
			candidates, e = l.Port().DiscoverForwards(call, &ppb.Scope{ServerId: server})
			return e
		})
	}
	states, e := r.service.ListForwards(ctx, &pb.Empty{})
	if e != nil {
		return
	}
	suspend := func() { r.suspendAutomaticForwards(ctx, server, states) }
	if err != nil {
		suspend()
		return
	}
	if candidates == nil || len(candidates.Specs) > 256 {
		suspend()
		return
	}
	selected := map[string]*ppb.ForwardSpec{}
	for _, candidate := range candidates.Specs {
		if candidate == nil || candidate.Id != "" || candidate.Revision != "" || candidate.ServerId != server || !candidate.Automatic || !cfg.AutomaticAllowed(candidate.RemotePort) || candidate.LocalAddress != cfg.BindAddress || candidate.SessionId == "" || len(candidate.SourceIdentity) != 64 || normalizeForward(candidate) != nil {
			suspend()
			return
		}
		if remote == nil {
			suspend()
			return
		}
		if _, e = remote.CheckPort(FreshRemoteContext(check), &pb.CheckPortRequest{RemoteAddress: candidate.RemoteAddress, Port: int32(candidate.RemotePort), SessionId: candidate.SessionId, Automatic: true, SourceIdentity: candidate.SourceIdentity}); e != nil {
			if connect.CodeOf(e) == connect.CodePermissionDenied {
				continue
			}
			suspend()
			return
		}
		key := automaticKey(candidate)
		if old := selected[key]; old != nil && !proto.Equal(old, candidate) {
			suspend()
			return
		}
		selected[key] = candidate
	}
	for _, state := range states.Forwards {
		spec := state.Spec
		if spec.ServerId != server || !spec.Automatic {
			continue
		}
		candidate := selected[automaticKey(spec)]
		if candidate == nil || candidate.SourceIdentity != spec.SourceIdentity {
			if r.disableDeletedSessionForward(ctx, spec) != nil {
				return
			}
		}
	}
	snapshot, e := r.store.View()
	if e != nil {
		return
	}
	for key, candidate := range selected {
		var suppressed string
		if snapshot.Get("forward_suppressions", key, &suppressed) == nil && suppressed == candidate.SourceIdentity {
			continue
		}
		var existing *ppb.ForwardSpec
		for id := range snapshot.Buckets["forwards"] {
			spec, e := forwardSpec(snapshot, id)
			if e != nil {
				return
			}
			if sameForwardEndpoint(spec, candidate) {
				existing = spec
				break
			}
		}
		if existing != nil {
			if !existing.Automatic || existing.SourceIdentity != candidate.SourceIdentity {
				continue
			}
			if !existing.Enabled {
				if r.stopForwardRuntime(ctx, existing.Id) == nil {
					_ = r.finishStoppedForward(existing.Id)
				}
				continue
			}
			if existing.LocalAddress != candidate.LocalAddress || existing.LocalPort != candidate.LocalPort {
				if r.stopForwardRuntime(ctx, existing.Id) != nil {
					return
				}
				existing.LocalAddress, existing.LocalPort = candidate.LocalAddress, candidate.LocalPort
				existing.Revision = newID("rev_")
				r.mu.Lock()
				e = r.commitForward(func(current *storage.Snapshot) error { return current.Put("forwards", existing.Id, existing) })
				r.mu.Unlock()
				if e != nil {
					return
				}
				r.applyForward(ctx, existing, nil)
				r.publish("forward", existing.Id, server, existing.Revision)
				continue
			}
			bound := false
			for _, state := range states.Forwards {
				if state.Spec.Id == existing.Id && state.BoundPort != 0 {
					bound = true
					break
				}
			}
			if !bound {
				r.applyForward(ctx, existing, nil)
			}
			continue
		}
		candidate = proto.Clone(candidate).(*ppb.ForwardSpec)
		candidate.Id = newID("fwd_")
		candidate.Revision = newID("rev_")
		candidate.Enabled = true
		r.mu.Lock()
		e = r.commitForward(func(current *storage.Snapshot) error {
			if _, e := storedServer(current, server); e != nil {
				return e
			}
			if len(current.Buckets["forwards"]) >= 256 {
				return rpcError(connect.CodeResourceExhausted, "FORWARD_LIMIT")
			}
			return current.Put("forwards", candidate.Id, candidate)
		})
		r.mu.Unlock()
		if e != nil {
			return
		}
		r.applyForward(ctx, candidate, nil)
		r.publish("forward", candidate.Id, server, candidate.Revision)
		snapshot, e = r.store.View()
		if e != nil {
			return
		}
	}
}

func (r *Resources) suspendAutomaticForwards(ctx context.Context, server string, states *ppb.ForwardList) {
	for _, state := range states.Forwards {
		if state.Spec.ServerId != server || !state.Spec.Automatic {
			continue
		}
		if r.stopForwardRuntime(ctx, state.Spec.Id) == nil {
			state.State = "offline"
			state.ReasonCode = "AUTO_SCAN_UNAVAILABLE"
			state.BoundPort = 0
			state.Connections = 0
			r.forwardStates.Store(state.Spec.Id, state)
			delete(r.forwardRoutes, state.Spec.Id)
		}
	}
}
