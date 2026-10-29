package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	pluginpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

// Resources owns the persistent local model. Credentials are immutable private
// files; the transaction commits only their revision pointers and request digests.
// A crash before pointer commit leaves an unused file, never a half-written token.
type Resources struct {
	forwardMu     sync.Mutex
	forwardStates sync.Map
	forwardRoutes map[string]*remotePool
	syncStates    sync.Map
	mu            sync.Mutex
	store         *storage.Store
	credentialDir string
	ctx           context.Context
	cancel        context.CancelFunc
	service       *Service
	node          *node.Node
	pools         map[string]*remotePool
	wake          chan struct{}
	done          chan struct{}
	faulted       atomic.Bool
}

func OpenResources(parent context.Context, service *Service, cfg *config.Client, embedded *node.Node) (*Resources, error) {
	if err := storage.EnsurePrivateDir(cfg.Agent.StateDir); err != nil {
		return nil, err
	}
	dir := filepath.Join(cfg.Agent.StateDir, "resources")
	store, err := storage.Open(dir, storage.Options{DisableRecovery: true, Validate: validateAgentState})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	r := &Resources{store: store, credentialDir: filepath.Join(cfg.Agent.StateDir, "credentials"), ctx: ctx, cancel: cancel, service: service, node: embedded, pools: map[string]*remotePool{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	fail := func(err error) (*Resources, error) { cancel(); store.Close(); return nil, err }
	if err = storage.EnsurePrivateDir(r.credentialDir); err != nil {
		return fail(err)
	}
	if err = r.importConfig(cfg); err != nil {
		return fail(err)
	}
	snapshot, err := store.View()
	if err != nil {
		return fail(err)
	}
	for id := range snapshot.Buckets["servers"] {
		record, err := storedServer(snapshot, id)
		if err != nil {
			return fail(err)
		}
		if record.CredentialPresent {
			if _, err = r.readCredential(record); err != nil {
				return fail(errors.New("AGENT_CREDENTIAL_CORRUPT"))
			}
		}
	}
	service.resources.Store(r)
	service.stateMu.Lock()
	service.resolve = r.Resolve
	service.stateMu.Unlock()
	go r.maintain()
	return r, nil
}
func (r *Resources) Close() error {
	r.cancel()
	if manager := r.service.plugins.Load(); manager != nil {
		_ = manager.Close()
	}
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.pools {
		r.retireLocked(id)
	}
	return r.store.Close()
}
func (r *Resources) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (s *Service) requireResources() (*Resources, error) {
	r := s.resources.Load()
	if r == nil || r.faulted.Load() {
		s.stateMu.Lock()
		reason := s.reason
		s.stateMu.Unlock()
		if reason == crashLoopReason || reason == crashStateReason || reason == crashClockReason {
			return nil, rpcError(connect.CodeFailedPrecondition, reason)
		}
		return nil, rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
	}
	return r, nil
}
func rpcError(code connect.Code, reason string) error { return connect.NewError(code, reason) }
func stateError(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return err
	}
	if errors.Is(err, storage.ErrRequestMismatch) {
		return rpcError(connect.CodeInvalidArgument, "REQUEST_ID_REUSED")
	}
	return rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
}
func storedServer(snapshot *storage.Snapshot, id string) (*agentpb.ServerRecord, error) {
	record := new(agentpb.ServerRecord)
	if err := snapshot.Get("servers", id, record); errors.Is(err, os.ErrNotExist) {
		return nil, rpcError(connect.CodeNotFound, "SERVER_NOT_FOUND")
	} else if err != nil {
		return nil, err
	}
	return record, nil
}
func checkRevision(current, expected string) error {
	if current != expected {
		return rpcError(connect.CodeAborted, "REVISION_CONFLICT")
	}
	return nil
}

// mutate keeps both the result and desired state in one transaction. It never
// persists request bodies, including write-only credentials.
func (r *Resources) mutate(method, id string, request proto.Message, result proto.Message, change func(*storage.Snapshot) error) (bool, error) {
	if r.faulted.Load() {
		return false, rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
	}
	if r.ctx.Err() != nil {
		return false, rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") {
		return false, rpcError(connect.CodeInvalidArgument, "REQUEST_ID_REQUIRED")
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return false, rpcError(connect.CodeInvalidArgument, "INVALID_REQUEST")
	}
	defer clear(encoded)
	current, err := r.store.View()
	if err != nil {
		return false, stateError(err)
	}
	if record, replay, err := current.Request(method, id, encoded); err != nil {
		return false, stateError(err)
	} else if replay {
		return true, stateError(json.Unmarshal(record.Result, result))
	}
	replay := false
	err = r.store.Update(func(snapshot *storage.Snapshot) error {
		record, previous, err := snapshot.Request(method, id, encoded)
		if err != nil {
			return err
		}
		replay = previous
		if previous {
			return json.Unmarshal(record.Result, result)
		}
		if len(snapshot.Requests) > 10000 {
			return rpcError(connect.CodeResourceExhausted, "REQUEST_HISTORY_LIMIT")
		}
		if err = change(snapshot); err != nil {
			return err
		}
		saved := result
		if record, ok := result.(*agentpb.ServerRecord); ok {
			copy := proto.Clone(record).(*agentpb.ServerRecord)
			clearServerRuntime(copy)
			saved = copy
		}
		return snapshot.CompleteRequest(method, id, "", saved, "")
	})
	if err != nil {
		var ce *connect.Error
		if !errors.As(err, &ce) && !errors.Is(err, storage.ErrRequestMismatch) {
			r.faulted.Store(true)
			for id := range r.pools {
				r.retireLocked(id)
			}
			r.service.setState("degraded", "AGENT_STATE_UNAVAILABLE")
		}
	}
	return replay, stateError(err)
}
func (r *Resources) publish(kind, id, server, revision string) {
	r.service.Events.Publish(&agentpb.AgentEvent{Type: kind, ResourceId: id, ServerId: server, Revision: revision})
}
func (r *Resources) readCredential(record *agentpb.ServerRecord) (string, error) {
	if !record.CredentialPresent {
		return "", rpcError(connect.CodeUnauthenticated, "CREDENTIAL_REQUIRED")
	}
	if !validID(record.CredentialRevision, "cred_") {
		return "", errors.New("invalid credential revision")
	}
	raw, err := storage.ReadPrivateFile(filepath.Join(r.credentialDir, record.CredentialRevision))
	if err != nil {
		return "", err
	}
	defer clear(raw)
	token := string(raw)
	if _, err = connection.BearerToken(token); err != nil {
		return "", errors.New("invalid stored credential")
	}
	return token, nil
}
func validID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return len(value) > len(prefix)
}
func validateServer(candidate *agentpb.ServerConfig) error {
	if candidate == nil || storage.ValidateName(candidate.Name) != nil {
		return rpcError(connect.CodeInvalidArgument, "INVALID_SERVER_NAME")
	}
	cfg := config.DefaultClient()
	cfg.Servers = map[string]config.Server{"candidate": {TailscaleAddress: candidate.TailscaleAddress, DirectTLSAddress: candidate.DirectTlsAddress, TLSServerName: candidate.TlsServerName, TLSCAFile: candidate.TlsCaFile, DaemonID: candidate.DaemonId}}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return rpcError(connect.CodeInvalidArgument, "INVALID_SERVER_CONFIG")
	}
	candidate.TlsCaFile = cfg.Servers["candidate"].TLSCAFile
	if candidate.Id != "" && !validID(candidate.Id, "srv_") {
		return rpcError(connect.CodeInvalidArgument, "INVALID_SERVER_ID")
	}
	if candidate.DaemonId != "" && !validID(candidate.DaemonId, "daemon_") {
		return rpcError(connect.CodeInvalidArgument, "INVALID_DAEMON_ID")
	}
	return nil
}
func validateAgentState(snapshot *storage.Snapshot) error {
	if snapshot.Revision > 0 {
		var imported bool
		if snapshot.Get("metadata", "config_imported", &imported) != nil || !imported || snapshot.Buckets["preferences"]["current"] == nil {
			return errors.New("incomplete agent state")
		}
	}
	names := map[string]bool{}
	for id := range snapshot.Buckets["servers"] {
		record, err := storedServer(snapshot, id)
		if err != nil {
			return err
		}
		if record.Config == nil || record.Config.Id != id || record.Revision == "" || validateServer(record.Config) != nil || names[record.Config.Name] || hasServerRuntime(record) {
			return errors.New("invalid persisted server")
		}
		names[record.Config.Name] = true
		if record.CredentialPresent != (record.CredentialRevision != "") {
			return errors.New("invalid credential pointer")
		}
		if record.CredentialPresent && !validID(record.CredentialRevision, "cred_") {
			return errors.New("invalid credential revision")
		}
	}
	for key, receipt := range snapshot.Requests {
		if (strings.HasPrefix(key, "UpsertServer\x00") || strings.HasPrefix(key, "SetServerCredential\x00")) && len(receipt.Result) > 0 {
			record := new(agentpb.ServerRecord)
			if json.Unmarshal(receipt.Result, record) != nil || hasServerRuntime(record) {
				return errors.New("runtime metadata in persisted server receipt")
			}
		}
	}
	for id := range snapshot.Buckets["mappings"] {
		m := new(pluginpb.Mapping)
		if snapshot.Get("mappings", id, m) != nil || !validID(id, "map_") || m.Id != id || m.Revision == "" || snapshot.Buckets["servers"][m.ServerId] == nil || !storage.ValidResourceID(m.ProjectId, "proj") || !filepath.IsAbs(m.LocalRoot) || filepath.Clean(m.LocalRoot) != m.LocalRoot {
			return errors.New("invalid persisted mapping")
		}
	}
	for id := range snapshot.Buckets["forwards"] {
		spec, err := forwardSpec(snapshot, id)
		if err != nil || !validID(id, "fwd_") || spec.Id != id || spec.Revision == "" || snapshot.Buckets["servers"][spec.ServerId] == nil {
			return errors.New("invalid persisted forward")
		}
		canonical := proto.Clone(spec).(*pluginpb.ForwardSpec)
		if normalizeForward(canonical) != nil || !proto.Equal(canonical, spec) {
			return errors.New("invalid persisted forward endpoint")
		}
	}
	for key := range snapshot.Buckets["forward_suppressions"] {
		parts := strings.Split(key, ":")
		var source string
		if len(parts) != 2 || !validID(parts[0], "srv_") || !validForwardSource(parts[1]) || snapshot.Get("forward_suppressions", key, &source) != nil || !validForwardSource(source) {
			return errors.New("invalid persisted automatic stop suppression")
		}
	}
	p := new(agentpb.Preferences)
	if err := snapshot.Get("preferences", "current", p); err == nil {
		_, validDetach := normalizeDetachKey(p.DetachKey)
		if p.Revision == "" || !validDetach || p.DefaultServerId != "" && snapshot.Buckets["servers"][p.DefaultServerId] == nil {
			return errors.New("invalid preferences")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (r *Resources) importConfig(cfg *config.Client) error {
	current, err := r.store.View()
	if err != nil {
		return err
	}
	var imported bool
	if current.Get("metadata", "config_imported", &imported) == nil && imported {
		return nil
	}
	return r.store.Update(func(snapshot *storage.Snapshot) error {
		var imported bool
		if snapshot.Get("metadata", "config_imported", &imported) == nil && imported {
			return nil
		}
		names := map[string]string{}
		keys := make([]string, 0, len(cfg.Servers))
		for name := range cfg.Servers {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			value := cfg.Servers[name]
			id := newID("srv_")
			names[name] = id
			record := &agentpb.ServerRecord{Config: &agentpb.ServerConfig{Id: id, Name: name, TailscaleAddress: value.TailscaleAddress, DirectTlsAddress: value.DirectTLSAddress, TlsServerName: value.TLSServerName, TlsCaFile: value.TLSCAFile, DaemonId: value.DaemonID}, Revision: newID("rev_")}
			if value.TokenRef != "" {
				return errors.New("TOKEN_REF_IMPORT_REQUIRES_EXPLICIT_CREDENTIAL")
			}
			if err := snapshot.Put("servers", id, record); err != nil {
				return err
			}
		}
		for root, value := range cfg.Projects {
			normalized, err := filepath.EvalSymlinks(root)
			if err != nil {
				return errors.New("CONFIG_MAPPING_ROOT_UNAVAILABLE")
			}
			m := &pluginpb.Mapping{Id: newID("map_"), ServerId: names[value.Server], ProjectId: value.ProjectID, LocalRoot: normalized, Revision: newID("rev_"), SyncEnabled: false, DefaultSession: value.DefaultSession, SessionHints: value.SessionHints}
			if err = snapshot.Put("mappings", m.Id, m); err != nil {
				return err
			}
		}
		if err := snapshot.Put("preferences", "current", &agentpb.Preferences{DetachKey: "ctrl-q", Revision: newID("rev_")}); err != nil {
			return err
		}
		return snapshot.Put("metadata", "config_imported", true)
	})
}
func (s *Service) ListServers(context.Context, *pb.Empty) (*agentpb.ServerList, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	out := new(agentpb.ServerList)
	for id := range snap.Buckets["servers"] {
		record, err := storedServer(snap, id)
		if err != nil {
			return nil, stateError(err)
		}
		r.decorateLocked(record)
		out.Servers = append(out.Servers, record)
	}
	sort.Slice(out.Servers, func(i, j int) bool { return out.Servers[i].Config.Name < out.Servers[j].Config.Name })
	if proto.Size(out) > connection.MaxMessageBytes {
		return nil, rpcError(connect.CodeResourceExhausted, "SERVER_LIST_TOO_LARGE")
	}
	return out, nil
}
