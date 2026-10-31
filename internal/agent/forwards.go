package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

func normalizeForward(spec *ppb.ForwardSpec) error {
	if spec != nil {
		if spec.Automatic {
			if spec.SessionId == "" || !validForwardSource(spec.SourceIdentity) {
				return rpcError(connect.CodeInvalidArgument, "INVALID_AUTOMATIC_SOURCE")
			}
		} else if spec.SourceIdentity != "" {
			return rpcError(connect.CodeInvalidArgument, "INVALID_MANUAL_SOURCE")
		}
	}
	if spec == nil || !validID(spec.ServerId, "srv_") || spec.RemotePort < 1 || spec.RemotePort > 65535 || spec.LocalPort > 65535 || (spec.SessionId != "" && !storage.ValidResourceID(spec.SessionId, "sess")) {
		return rpcError(connect.CodeInvalidArgument, "INVALID_FORWARD_SPEC")
	}
	if spec.LocalAddress == "" {
		spec.LocalAddress = "127.0.0.1"
	}
	if spec.RemoteAddress == "" {
		spec.RemoteAddress = "127.0.0.1"
	}
	local, err := schema.Loopback(spec.LocalAddress)
	if err != nil {
		return rpcError(connect.CodeInvalidArgument, "LOCAL_LOOPBACK_REQUIRED")
	}
	remote, err := schema.Loopback(spec.RemoteAddress)
	if err != nil {
		return rpcError(connect.CodeInvalidArgument, "REMOTE_LOOPBACK_REQUIRED")
	}
	spec.LocalAddress = local.String()
	spec.RemoteAddress = remote.String()
	return nil
}
func requestBytes(id string, in proto.Message) ([]byte, error) {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") {
		return nil, rpcError(connect.CodeInvalidArgument, "REQUEST_ID_REQUIRED")
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(in)
	if err != nil {
		return nil, rpcError(connect.CodeInvalidArgument, "INVALID_REQUEST")
	}
	return b, nil
}
func forwardSpec(snapshot *storage.Snapshot, id string) (*ppb.ForwardSpec, error) {
	v := new(ppb.ForwardSpec)
	if err := snapshot.Get("forwards", id, v); errors.Is(err, os.ErrNotExist) {
		return nil, rpcError(connect.CodeNotFound, "FORWARD_NOT_FOUND")
	} else if err != nil {
		return nil, stateError(err)
	}
	return v, nil
}
func (r *Resources) forwardRequest(method, id string, encoded []byte) (storage.RequestRecord, bool, error) {
	if r.faulted.Load() || r.ctx.Err() != nil {
		return storage.RequestRecord{}, false, rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
	}
	snapshot, err := r.store.View()
	if err != nil {
		return storage.RequestRecord{}, false, stateError(err)
	}
	record, exists, err := snapshot.Request(method, id, encoded)
	return record, exists, stateError(err)
}

// Desired state is committed before acquiring a listener. The pending request
// journal survives a crash between binding and recording the observed result.
func (s *Service) CreateForward(ctx context.Context, in *apb.CreateForwardRequest) (*ppb.ForwardState, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil || in.Spec == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "FORWARD_SPEC_REQUIRED")
	}
	encoded, err := requestBytes(in.RequestId, in)
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	record, exists, err := r.forwardRequest("CreateForward", in.RequestId, encoded)
	if err != nil {
		return nil, err
	}
	out := new(ppb.ForwardState)
	if exists {
		if err = json.Unmarshal(record.Result, out); err != nil {
			return nil, stateError(err)
		}
		if record.Status != "pending" {
			return out, nil
		}
	} else {
		spec := proto.Clone(in.Spec).(*ppb.ForwardSpec)
		if spec.Id != "" || spec.Revision != "" || spec.Automatic || spec.SourceIdentity != "" {
			return nil, rpcError(connect.CodeInvalidArgument, "FORWARD_ID_IS_HOST_ASSIGNED")
		}
		if spec.LocalAddress == "" {
			if manager := s.plugins.Load(); manager != nil {
				values, _, e := manager.Settings("port")
				if e == nil {
					cfg, e := schema.Parse(values, "client")
					if e == nil {
						spec.LocalAddress = cfg.BindAddress
					}
				}
			}
		}
		if err = normalizeForward(spec); err != nil {
			return nil, err
		}
		verify, stop := context.WithTimeout(ctx, 5*time.Second)
		client, verified, e := r.ResolveSnapshot(verify, spec.ServerId)
		if e == nil {
			var agreement *pb.ProtocolAgreement
			agreement, e = rpc.NewShikiDaemonClient(client).NegotiateProtocol(FreshRemoteContext(verify), &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.PortCapability, sdk.PortCheckCapability}, RequiredCapabilities: []string{sdk.PortCapability, sdk.PortCheckCapability}})
			if e == nil && (agreement == nil || agreement.Major != 1 || agreement.Minor != 0 || (!slices.Contains(agreement.Capabilities, sdk.PortCapability) || !slices.Contains(agreement.Capabilities, sdk.PortCheckCapability))) {
				e = rpcError(connect.CodeFailedPrecondition, "PORT_CAPABILITY_UNAVAILABLE")
			}
		}
		if e == nil && spec.SessionId != "" {
			var session *pb.Session
			session, e = rpc.NewShikiDaemonClient(client).GetSession(FreshRemoteContext(verify), &pb.GetSessionRequest{Id: spec.SessionId})
			if e == nil && (session == nil || session.Id != spec.SessionId) {
				e = rpcError(connect.CodeFailedPrecondition, "SESSION_ID_MISMATCH")
			}
		}
		if e == nil {
			_, e = rpc.NewShikiDaemonClient(client).CheckPort(FreshRemoteContext(verify), &pb.CheckPortRequest{RemoteAddress: spec.RemoteAddress, Port: int32(spec.RemotePort), SessionId: spec.SessionId})
		}
		stop()
		if e != nil {
			return nil, safeRemoteError(e)
		}
		spec.Id = newID("fwd_")
		spec.Revision = newID("rev_")
		spec.Enabled = true
		out = &ppb.ForwardState{Spec: spec, State: "starting"}
		r.mu.Lock()
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		err = r.commitForward(func(snapshot *storage.Snapshot) error {
			current, e := storedServer(snapshot, spec.ServerId)
			if e != nil {
				return e
			}
			// A successful remote check belongs to that exact authenticated
			// server and credential generation, not merely the same server ID.
			if !proto.Equal(current, verified) {
				return rpcError(connect.CodeAborted, "SERVER_CHANGED")
			}
			if len(snapshot.Buckets["forwards"]) >= 256 {
				return rpcError(connect.CodeResourceExhausted, "FORWARD_LIMIT")
			}
			if len(snapshot.Requests) >= 10000 {
				return rpcError(connect.CodeResourceExhausted, "REQUEST_HISTORY_LIMIT")
			}
			pending, _, e := snapshot.Request("CreateForward", in.RequestId, encoded)
			if e != nil {
				return e
			}
			pending.ResourceID = spec.Id
			pending.Result, e = json.Marshal(out)
			if e != nil {
				return e
			}
			snapshot.Requests["CreateForward\x00"+in.RequestId] = pending
			return snapshot.Put("forwards", spec.Id, spec)
		})
		r.mu.Unlock()
		if err != nil {
			return nil, stateError(err)
		}
	}
	snapshot, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	spec, err := forwardSpec(snapshot, out.Spec.Id)
	if connect.CodeOf(err) == connect.CodeNotFound {
		out.State = "stopped"
		out.BoundPort = 0
		out.ReasonCode = "FORWARD_ALREADY_STOPPED"
	} else if err != nil {
		return nil, err
	} else if !spec.Enabled {
		out.Spec = spec
		out.State = "stopped"
		out.BoundPort = 0
	} else {
		out = r.applyForward(r.ctx, spec, nil)
	}
	if err = r.commitForward(func(snapshot *storage.Snapshot) error {
		return snapshot.CompleteRequest("CreateForward", in.RequestId, out.Spec.Id, out, "")
	}); err != nil {
		return nil, stateError(err)
	}
	r.publish("forward", out.Spec.Id, out.Spec.ServerId, out.Spec.Revision)
	return out, nil
}
func (r *Resources) applyForward(parent context.Context, spec *ppb.ForwardSpec, worker sdk.Lifecycle) *ppb.ForwardState {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	out := &ppb.ForwardState{Spec: proto.Clone(spec).(*ppb.ForwardSpec), State: "error", ReasonCode: "PORT_PLUGIN_UNAVAILABLE"}
	invoked := false
	call := func(ctx context.Context, l sdk.Lifecycle) error {
		invoked = true
		result, err := l.Port().StartForward(ctx, spec)
		if err == nil {
			if result == nil || result.Spec == nil || !proto.Equal(result.Spec, spec) {
				return rpcError(connect.CodeDataLoss, "INVALID_FORWARD_WORKER_STATE")
			}
			out = proto.Clone(result).(*ppb.ForwardState)
		}
		return err
	}
	var err error
	if worker != nil {
		err = call(ctx, worker)
	} else if manager := r.service.plugins.Load(); manager != nil {
		err = manager.Call(ctx, "port", sdk.PortWorkerCapability, call)
	} else {
		err = unavailableWorker()
	}
	if err != nil {
		out.BoundPort = 0
		out.Connections = 0
		if invoked {
			out.ReasonCode = "FORWARD_BIND_FAILED"
		}
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			out.State = "auth_required"
			out.ReasonCode = "REMOTE_AUTH_REQUIRED"
		}
	}
	r.forwardStates.Store(spec.Id, proto.Clone(out).(*ppb.ForwardState))
	r.mu.Lock()
	route := r.pools[spec.ServerId]
	r.mu.Unlock()
	if r.forwardRoutes == nil {
		r.forwardRoutes = map[string]*remotePool{}
	}
	r.forwardRoutes[spec.Id] = route
	return out
}

func (s *Service) StopForward(ctx context.Context, in *apb.StopForwardRequest) (*pb.Empty, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil || !validID(in.ForwardId, "fwd_") {
		return nil, rpcError(connect.CodeInvalidArgument, "FORWARD_ID_REQUIRED")
	}
	encoded, err := requestBytes(in.RequestId, in)
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	r.forwardMu.Lock()
	defer r.forwardMu.Unlock()
	record, exists, err := r.forwardRequest("StopForward", in.RequestId, encoded)
	if err != nil {
		return nil, err
	}
	if exists && record.Status != "pending" {
		return &pb.Empty{}, nil
	}
	if !exists {
		r.mu.Lock()
		err = r.commitForward(func(snapshot *storage.Snapshot) error {
			spec, e := forwardSpec(snapshot, in.ForwardId)
			if e != nil {
				return e
			}
			if e = checkRevision(spec.Revision, in.ExpectedRevision); e != nil {
				return e
			}
			pending, _, e := snapshot.Request("StopForward", in.RequestId, encoded)
			if e != nil {
				return e
			}
			pending.ResourceID = spec.Id
			pending.Result = []byte("{}")
			if spec.Automatic {
				if e = snapshot.Put("forward_suppressions", automaticKey(spec), spec.SourceIdentity); e != nil {
					return e
				}
			}
			snapshot.Requests["StopForward\x00"+in.RequestId] = pending
			spec.Enabled = false
			spec.Revision = newID("rev_")
			return snapshot.Put("forwards", spec.Id, spec)
		})
		r.mu.Unlock()
		if err != nil {
			return nil, stateError(err)
		}
	}
	if err = r.stopForwardRuntime(r.ctx, in.ForwardId); err != nil {
		return nil, err
	}
	if err = r.finishStoppedForward(in.ForwardId); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}
func (r *Resources) stopForwardRuntime(parent context.Context, id string) error {
	manager := r.service.plugins.Load()
	if manager == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	err := manager.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
		return l.Port().StopForward(ctx, &ppb.ForwardID{Id: id})
	})
	if err == nil {
		return nil
	}
	list, e := manager.List()
	if e == nil {
		for _, p := range list.Plugins {
			if p.PluginId == "port" && p.State == "stopped" {
				return nil
			}
		}
	}
	return err
}
func (r *Resources) finishStoppedForward(id string) error {
	server, revision := "", ""
	r.mu.Lock()
	err := r.commitForward(func(snapshot *storage.Snapshot) error {
		spec, e := forwardSpec(snapshot, id)
		if e != nil && connect.CodeOf(e) != connect.CodeNotFound {
			return e
		}
		if spec != nil {
			if spec.Enabled {
				return rpcError(connect.CodeAborted, "FORWARD_REENABLED")
			}
			server, revision = spec.ServerId, spec.Revision
			delete(snapshot.Buckets["forwards"], id)
		}
		for key, record := range snapshot.Requests {
			if strings.HasPrefix(key, "StopForward\x00") && record.ResourceID == id && record.Status == "pending" {
				if e = snapshot.CompleteRequest("StopForward", strings.TrimPrefix(key, "StopForward\x00"), id, &pb.Empty{}, ""); e != nil {
					return e
				}
			}
		}
		return nil
	})
	r.mu.Unlock()
	if err != nil {
		return stateError(err)
	}
	r.forwardStates.Delete(id)
	delete(r.forwardRoutes, id)
	r.publish("forward", id, server, revision)
	return nil
}

func (s *Service) ListForwards(ctx context.Context, _ *pb.Empty) (*ppb.ForwardList, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	snapshot, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	actual := map[string]*ppb.ForwardState{}
	manager := s.plugins.Load()
	available := false
	if manager != nil {
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = manager.Call(call, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			list, e := l.Port().ListForwards(ctx)
			if e == nil {
				for _, v := range list.Forwards {
					if v != nil && v.Spec != nil {
						actual[v.Spec.Id] = v
					}
				}
			}
			return e
		})
		cancel()
		available = err == nil
	}
	out := &ppb.ForwardList{}
	for id := range snapshot.Buckets["forwards"] {
		spec, e := forwardSpec(snapshot, id)
		if e != nil {
			return nil, e
		}
		state := &ppb.ForwardState{Spec: spec, State: "stopped"}
		if spec.Enabled {
			state.State = "error"
			state.ReasonCode = "PORT_PLUGIN_UNAVAILABLE"
			if cached, ok := r.forwardStates.Load(id); ok {
				state = proto.Clone(cached.(*ppb.ForwardState)).(*ppb.ForwardState)
				state.Spec = spec
			}
			if !available {
				state.BoundPort = 0
				state.Connections = 0
				state.State = "error"
				state.ReasonCode = "PORT_PLUGIN_UNAVAILABLE"
			} else if runtime := actual[id]; runtime != nil && runtime.State != "stopped" && proto.Equal(runtime.Spec, spec) {
				state = proto.Clone(runtime).(*ppb.ForwardState)
			} else {
				state.BoundPort = 0
				state.Connections = 0
			}
		}
		// This snapshot can predate StopForward or a recovery transition. Only
		// lifecycle owners update the cache; a read must not resurrect a removed
		// entry or replace a newer control result after a slow worker query.
		out.Forwards = append(out.Forwards, state)
	}
	sort.Slice(out.Forwards, func(i, j int) bool { return out.Forwards[i].Spec.Id < out.Forwards[j].Spec.Id })
	if proto.Size(out) > connection.MaxMessageBytes {
		return nil, rpcError(connect.CodeResourceExhausted, "FORWARD_LIST_TOO_LARGE")
	}
	return out, nil
}
func (s *Service) activeForwardTasks(ctx context.Context) uint32 {
	list, err := s.ListForwards(ctx, &pb.Empty{})
	if err != nil {
		return 0
	}
	var count uint32
	for _, v := range list.Forwards {
		if v.BoundPort != 0 {
			count++
		}
	}
	return count
}

// An uncertain durable write ends task lifetimes and exposes degraded status;
// accepting more mutations could otherwise act on an uncommitted desired view.
func (r *Resources) commitForward(change func(*storage.Snapshot) error) error {
	err := r.store.Update(change)
	if err != nil {
		var ce *connect.Error
		if !errors.As(err, &ce) && !errors.Is(err, storage.ErrRequestMismatch) {
			r.faulted.Store(true)
			r.service.setState("degraded", "AGENT_STATE_UNAVAILABLE")
			r.cancel()
		}
	}
	return err
}

func validForwardSource(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
