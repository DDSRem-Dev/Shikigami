package agent

import (
	"context"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	pluginpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func (r *Resources) serverUsed(snapshot *storage.Snapshot, id string) bool {
	for key := range snapshot.Buckets["mappings"] {
		m := new(pluginpb.Mapping)
		if snapshot.Get("mappings", key, m) != nil || m.ServerId == id {
			return true
		}
	}
	for key := range snapshot.Buckets["forwards"] {
		spec, err := forwardSpec(snapshot, key)
		if err != nil || spec.ServerId == id {
			return true
		}
	}
	return r.service.Registry.ServerTerminalCount(id) > 0
}
func (s *Service) UpsertServer(_ context.Context, in *agentpb.UpsertServerRequest) (*agentpb.ServerRecord, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil || in.Config == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "SERVER_CONFIG_REQUIRED")
	}
	candidate := proto.Clone(in.Config).(*agentpb.ServerConfig)
	if err = validateServer(candidate); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := new(agentpb.ServerRecord)
	replay, err := r.mutate("UpsertServer", in.RequestId, in, out, func(snapshot *storage.Snapshot) error {
		if candidate.Id == "" {
			if in.ExpectedRevision != "" {
				return rpcError(connect.CodeAborted, "REVISION_CONFLICT")
			}
			candidate.Id = newID("srv_")
			out.Config = candidate
		} else {
			previous, err := storedServer(snapshot, candidate.Id)
			if err != nil {
				return err
			}
			if err = checkRevision(previous.Revision, in.ExpectedRevision); err != nil {
				return err
			}
			if previous.Config.DaemonId != candidate.DaemonId && r.serverUsed(snapshot, candidate.Id) {
				return rpcError(connect.CodeFailedPrecondition, "SERVER_IN_USE")
			}
			proto.Merge(out, previous)
			out.Config = candidate
		}
		for id := range snapshot.Buckets["servers"] {
			other, err := storedServer(snapshot, id)
			if err != nil {
				return err
			}
			if id != candidate.Id && other.Config.Name == candidate.Name {
				return rpcError(connect.CodeAlreadyExists, "SERVER_NAME_EXISTS")
			}
		}
		out.Revision = newID("rev_")
		clearServerRuntime(out)
		return snapshot.Put("servers", candidate.Id, out)
	})
	if err != nil {
		return nil, err
	}
	if !replay {
		r.retireLocked(out.Config.Id)
		r.publish("server", out.Config.Id, out.Config.Id, out.Revision)
		r.notify()
	}
	r.decorateLocked(out)
	return out, nil
}
func (s *Service) RemoveServer(_ context.Context, in *agentpb.RemoveServerRequest) (*pb.Empty, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := new(pb.Empty)
	replay, err := r.mutate("RemoveServer", in.GetRequestId(), in, out, func(snapshot *storage.Snapshot) error {
		record, err := storedServer(snapshot, in.ServerId)
		if err != nil {
			return err
		}
		if err = checkRevision(record.Revision, in.ExpectedRevision); err != nil {
			return err
		}
		if r.serverUsed(snapshot, in.ServerId) {
			return rpcError(connect.CodeFailedPrecondition, "SERVER_IN_USE")
		}
		delete(snapshot.Buckets["servers"], in.ServerId)
		p := new(agentpb.Preferences)
		if snapshot.Get("preferences", "current", p) == nil && p.DefaultServerId == in.ServerId {
			p.DefaultServerId = ""
			p.Revision = newID("rev_")
			return snapshot.Put("preferences", "current", p)
		}
		return nil
	})
	if err == nil && !replay {
		r.retireLocked(in.ServerId)
		r.publish("server", in.ServerId, in.ServerId, "")
	}
	return out, err
}
func (s *Service) SetServerCredential(_ context.Context, in *agentpb.SetServerCredentialRequest) (*agentpb.ServerRecord, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if len(in.GetToken()) > 16384 {
		return nil, rpcError(connect.CodeInvalidArgument, "INVALID_CREDENTIAL")
	}
	if _, err = connection.BearerToken(string(in.GetToken())); err != nil {
		return nil, rpcError(connect.CodeInvalidArgument, "INVALID_CREDENTIAL")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := new(agentpb.ServerRecord)
	created := ""
	replay, err := r.mutate("SetServerCredential", in.GetRequestId(), in, out, func(snapshot *storage.Snapshot) error {
		record, err := storedServer(snapshot, in.ServerId)
		if err != nil {
			return err
		}
		if err = checkRevision(record.CredentialRevision, in.ExpectedCredentialRevision); err != nil {
			return err
		}
		created = newID("cred_")
		if err = storage.AtomicWrite(r.credentialDir, created, in.Token); err != nil {
			return err
		}
		proto.Merge(out, record)
		out.CredentialPresent = true
		out.CredentialRevision = created
		clearServerRuntime(out)
		return snapshot.Put("servers", in.ServerId, out)
	})
	// Keep a possible uncertain commit's immutable credential file. Only the
	// committed revision can ever be loaded; unused files do not restore old keys.
	_ = created
	if err != nil {
		return nil, err
	}
	if !replay {
		r.retireLocked(in.ServerId)
		r.publish("server", in.ServerId, in.ServerId, out.Revision)
		r.notify()
	}
	r.decorateLocked(out)
	return out, nil
}
func (s *Service) GetPreferences(context.Context, *pb.Empty) (*agentpb.Preferences, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	out := new(agentpb.Preferences)
	if err = snapshot.Get("preferences", "current", out); err != nil {
		return nil, stateError(err)
	}
	return out, nil
}
func (s *Service) SetPreferences(_ context.Context, in *agentpb.SetPreferencesRequest) (*agentpb.Preferences, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil || in.Values == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "PREFERENCES_REQUIRED")
	}
	detach, ok := normalizeDetachKey(in.Values.DetachKey)
	if !ok {
		return nil, rpcError(connect.CodeInvalidArgument, "DETACH_KEY_UNSUPPORTED")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := new(agentpb.Preferences)
	_, err = r.mutate("SetPreferences", in.RequestId, in, out, func(snapshot *storage.Snapshot) error {
		old := new(agentpb.Preferences)
		if err := snapshot.Get("preferences", "current", old); err != nil {
			return err
		}
		if err := checkRevision(old.Revision, in.Values.Revision); err != nil {
			return err
		}
		if in.Values.DefaultServerId != "" {
			if _, err := storedServer(snapshot, in.Values.DefaultServerId); err != nil {
				return err
			}
		}
		proto.Merge(out, in.Values)
		out.DetachKey = detach
		out.Revision = newID("rev_")
		return snapshot.Put("preferences", "current", out)
	})
	return out, err
}

func normalizeDetachKey(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "ctrl-q", "ctrl+q", "^q":
		return "ctrl-q", true
	case "ctrl-]", "ctrl+]", "^]":
		return "ctrl-]", true
	case "none":
		return "none", true
	default:
		return "", false
	}
}

// Persistent records and deduplication receipts never own observed connection
// state. The returned response is decorated only after the transaction ends.
func clearServerRuntime(record *agentpb.ServerRecord) {
	record.ConnectionState, record.ReasonCode, record.ConnectionPath = "", "", ""
	record.RemoteProtocol = nil
	record.ConnectionAttempts = nil
}
func hasServerRuntime(record *agentpb.ServerRecord) bool {
	return record.ConnectionState != "" || record.ReasonCode != "" || record.ConnectionPath != "" || record.RemoteProtocol != nil || len(record.ConnectionAttempts) > 0
}
