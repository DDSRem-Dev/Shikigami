package plugin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// SupervisorOptions separates durable intent from process state. Config and
// Ready execute without the supervisor lock; CoreAPI callbacks may read state.
type SupervisorOptions struct {
	StateDir string
	// PluginsDir and Side select this host's installed third-party packages.
	// Config supplies host-owned callbacks/directories; their verified manifest
	// is injected by the supervisor, never resolved as a built-in fallback.
	PluginsDir string
	Side       string
	Config     func(string, *structpb.Struct) (Config, error)
	Validate   func(string, *structpb.Struct) error
	Ready      func(context.Context, string, sdk.Lifecycle) error
	Changed    func(string)
	// PluginRegistered is called after a plugin reaches "ready" state and its
	// SettingsRegistry entry has been populated from the manifest/Info response.
	// Called without the supervisor lock; safe to call back into the supervisor.
	PluginRegistered func(id string, settings *PluginSettings)
	// PluginStopped is called when a plugin leaves "ready" (stopped, failed, etc.).
	PluginStopped func(id string)
}
type desiredPlugin struct {
	ID       string           `json:"id"`
	Enabled  bool             `json:"enabled"`
	Revision string           `json:"revision"`
	Values   *structpb.Struct `json:"values,omitempty"`
}
type supervised struct {
	logs        []*pb.LogRecord
	subscribers map[*logSubscription]bool
	wake        chan struct{}
	status      *pb.PluginStatus
	instance    *Instance
	artifact    *Manifest
	artifactErr error
	generation  uint64
}
type Supervisor struct {
	startMu   sync.Mutex
	refreshMu sync.Mutex
	registry  *PluginRegistry
	started   bool
	ctx       context.Context
	cancel    context.CancelFunc
	store     *storage.Store
	options   SupervisorOptions
	mu        sync.Mutex
	slots     map[string]*supervised
	manifests map[string]*sdk.PluginManifest
	done      sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func NewSupervisor(parent context.Context, opts SupervisorOptions) (*Supervisor, error) {
	if opts.Config == nil || opts.Validate == nil {
		return nil, errors.New("plugin configuration and validation callbacks are required")
	}
	store, err := storage.Open(opts.StateDir, storage.Options{DisableRecovery: true, Validate: validatePluginState})
	if err != nil {
		return nil, err
	}
	err = store.Update(func(state *storage.Snapshot) error {
		for _, id := range []string{"sync", "port"} {
			if _, exists := state.Buckets["plugins"][id]; !exists {
				if err := state.Put("plugins", id, &desiredPlugin{ID: id, Revision: "rev_" + rand.Text()}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		store.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Supervisor{ctx: ctx, cancel: cancel, store: store, options: opts, slots: map[string]*supervised{}, manifests: map[string]*sdk.PluginManifest{}}
	if opts.PluginsDir != "" {
		if opts.Side != "client" && opts.Side != "server" {
			cancel()
			store.Close()
			return nil, errors.New("plugin discovery requires a client or server side")
		}
		m.registry = NewPluginRegistry(opts.PluginsDir, opts.Side)
	}
	state, err := store.View()
	if err != nil {
		cancel()
		store.Close()
		return nil, err
	}
	for id := range state.Buckets["plugins"] {
		var want desiredPlugin
		if err := state.Get("plugins", id, &want); err != nil {
			cancel()
			store.Close()
			return nil, err
		}
		m.slots[id] = &supervised{wake: make(chan struct{}, 1), status: &pb.PluginStatus{PluginId: id, Enabled: want.Enabled, Revision: want.Revision, State: "stopped"}}
	}
	if err := m.refresh(); err != nil {
		cancel()
		store.Close()
		return nil, err
	}
	return m, nil
}
func validatePluginState(state *storage.Snapshot) error {
	for id, data := range state.Buckets["plugins"] {
		var want desiredPlugin
		if err := json.Unmarshal(data, &want); err != nil {
			return err
		}
		if !pluginIDPattern.MatchString(id) || want.ID != id || want.Revision == "" {
			return errors.New("invalid persistent plugin intent")
		}
	}
	return nil
}

// Manage returns the durable acceptance result. A starting response is not a
// readiness claim: callers observe List until the worker is ready or failed.
// Replaying request_id returns the original result and never repeats a restart.
func (m *Supervisor) Manage(ctx context.Context, request *pb.ManagePluginRequest) (*pb.PluginStatus, error) {
	if request == nil || request.RequestId == "" || len(request.RequestId) > 128 || !pluginIDPattern.MatchString(request.PluginId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "invalid plugin operation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.ctx.Err() != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "plugin supervisor stopped")
	}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	request = proto.Clone(request).(*pb.ManagePluginRequest)
	result := new(pb.PluginStatus)
	replay := false
	err := m.store.Update(func(state *storage.Snapshot) error {
		record, exists, err := state.Request("plugin.manage", request.RequestId, request)
		if err != nil {
			return err
		}
		if exists {
			replay = true
			return json.Unmarshal(record.Result, result)
		}
		var want desiredPlugin
		if err := state.Get("plugins", request.PluginId, &want); err != nil {
			return connect.NewError(connect.CodeNotFound, "plugin is not installed")
		}
		if want.Revision != request.ExpectedRevision {
			return connect.NewError(connect.CodeAborted, "plugin revision changed")
		}
		if request.Action != pb.ManagePluginRequest_SET_CONFIG && request.Config != nil {
			return connect.NewError(connect.CodeInvalidArgument, "config is only valid with SET_CONFIG")
		}
		switch request.Action {
		case pb.ManagePluginRequest_ENABLE:
			if err := m.validate(want.ID, want.Values); err != nil {
				return err
			}
			want.Enabled = true
		case pb.ManagePluginRequest_DISABLE:
			want.Enabled = false
		case pb.ManagePluginRequest_RESTART:
			if !want.Enabled {
				return connect.NewError(connect.CodeFailedPrecondition, "enable plugin before restarting")
			}
			if err := m.validate(want.ID, want.Values); err != nil {
				return err
			}
		case pb.ManagePluginRequest_SET_CONFIG:
			if request.Config == nil {
				return connect.NewError(connect.CodeInvalidArgument, "plugin config required")
			}
			if err := m.validate(want.ID, request.Config); err != nil {
				return err
			}
			want.Values = proto.Clone(request.Config).(*structpb.Struct)
		default:
			return connect.NewError(connect.CodeInvalidArgument, "plugin action required")
		}
		want.Revision = "rev_" + rand.Text()
		if err := state.Put("plugins", want.ID, &want); err != nil {
			return err
		}
		result.PluginId, result.Enabled, result.Revision = want.ID, want.Enabled, want.Revision
		result.State = "starting"
		if !want.Enabled {
			result.ReasonCode = "PLUGIN_STOP_REQUESTED"
		}
		return state.CompleteRequest("plugin.manage", request.RequestId, want.ID, result, "")
	})
	if errors.Is(err, storage.ErrRequestMismatch) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "request_id reused with different parameters")
	}
	if err != nil {
		return nil, err
	}
	if !replay {
		m.mu.Lock()
		slot := m.slots[request.PluginId]
		m.mu.Unlock()
		select {
		case slot.wake <- struct{}{}:
		default:
		}
		m.changed(request.PluginId)
	}
	return result, nil
}
func (m *Supervisor) List() (*pb.ListPluginsResponse, error) {
	if err := m.refresh(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, err
	}
	result := new(pb.ListPluginsResponse)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, slot := range m.slots {
		status := proto.Clone(slot.status).(*pb.PluginStatus)
		var want desiredPlugin
		if err := state.Get("plugins", id, &want); err != nil {
			return nil, err
		}
		if status.Revision != want.Revision {
			status.State = "starting"
			status.ReasonCode = ""
			status.Protocol = nil
		}
		status.Enabled, status.Revision = want.Enabled, want.Revision
		if !want.Enabled {
			status.Protocol = nil
			if slot.status.Revision != want.Revision {
				status.State = "starting"
				status.ReasonCode = "PLUGIN_STOPPING"
			}
		}
		if !builtinID(id) {
			if slot.artifact == nil {
				if want.Enabled {
					status.State, status.ReasonCode = artifactFailure(slot.artifactErr)
				}
				status.Protocol = nil
			} else {
				status.BuildVersion = slot.artifact.Version
				if slot.instance != nil && !sameArtifact(slot.artifact, &slot.instance.artifact) {
					status.State, status.ReasonCode, status.Protocol = "starting", "", nil
				}
			}
		}
		if status.State == "ready" && !instanceReady(slot.instance) {
			status.State = "failed"
			status.ReasonCode = "PLUGIN_INSTANCE_ENDED"
			status.Protocol = nil
		}
		result.Plugins = append(result.Plugins, status)
	}
	sort.Slice(result.Plugins, func(i, j int) bool { return result.Plugins[i].PluginId < result.Plugins[j].PluginId })
	return result, nil
}

// Call binds an operation to the current worker's credential lifetime. It does
// not expose mutable process state and never calls worker code under a lock.
func (m *Supervisor) Call(ctx context.Context, id, capability string, fn func(context.Context, sdk.Lifecycle) error) error {
	if !builtinID(id) {
		if err := m.refresh(); err != nil {
			return err
		}
	}
	instance := m.current(id)
	if instance == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "plugin is unavailable")
	}
	instance.mu.Lock()
	compatible := false
	for _, available := range instance.snapshot.Protocol.GetCapabilities() {
		if available == capability {
			compatible = true
		}
	}
	instance.mu.Unlock()
	if !compatible {
		return connect.NewError(connect.CodeFailedPrecondition, "plugin capability is unavailable")
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(instance.grant.lifetime, func() { cancel(); closeInput(ctx); abortOutput(ctx) })
	defer stop()
	if instance.grant.lifetime.Err() != nil {
		return connect.NewError(connect.CodeUnavailable, "plugin instance ended")
	}
	return fn(call, instance.lifecycle)
}
func (m *Supervisor) desired(id string) (desiredPlugin, error) {
	var want desiredPlugin
	state, err := m.store.View()
	if err == nil {
		err = state.Get("plugins", id, &want)
	}
	return want, err
}
func (m *Supervisor) changed(id string) {
	if m.options.Changed != nil {
		m.options.Changed(id)
	}
}
func (m *Supervisor) publish(id string, slot *supervised, want desiredPlugin, instance *Instance, state, reason string) bool {
	status := &pb.PluginStatus{PluginId: id, Enabled: want.Enabled, Revision: want.Revision, State: state, ReasonCode: reason}
	if instance != nil {
		snapshot := instance.Snapshot()
		status.BuildVersion = snapshot.Version
		status.Protocol = snapshot.Protocol
	}
	m.mu.Lock()
	if instance != nil && !builtinID(id) && !sameArtifact(slot.artifact, &instance.artifact) {
		m.mu.Unlock()
		return false
	}
	slot.status = status
	slot.instance = instance
	m.mu.Unlock()
	m.record(id, &pb.LogRecord{Timestamp: time.Now().Unix(), Level: "info", Message: "PLUGIN_STATE_CHANGED", Fields: map[string]string{"state": state, "reason_code": reason}})
	m.changed(id)
	return true
}
func (m *Supervisor) stop(slot *supervised) {
	m.mu.Lock()
	instance := slot.instance
	slot.instance = nil
	m.mu.Unlock()
	if instance != nil {
		if m.options.PluginStopped != nil {
			m.options.PluginStopped(instance.Snapshot().Name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = instance.Stop(ctx)
		cancel()
		// Stop's deadline only bounds that call. The supervisor remains the
		// owner until callbacks/process cleanup finish, before replacement or
		// closing the shared store.
		<-instance.Done()
	}
}
func (m *Supervisor) run(id string, slot *supervised) {
	defer m.done.Done()
	defer m.stop(slot)
	revision := ""
	var generation uint64
	attempts := 0
	var instance *Instance
	started := time.Time{}
	for {
		if m.ctx.Err() != nil {
			return
		}
		want, err := m.desired(id)
		if err != nil {
			m.stop(slot)
			return
		}
		artifact, artifactErr, currentGeneration := m.artifact(id)
		changed := revision != want.Revision || generation != currentGeneration
		if changed {
			revision = want.Revision
			generation = currentGeneration
			attempts = 0
			m.stop(slot)
			instance = nil
		}
		if !want.Enabled {
			if m.options.PluginStopped != nil {
				m.options.PluginStopped(id)
			}
			m.publish(id, slot, want, nil, "stopped", "")
			select {
			case <-m.ctx.Done():
				return
			case <-slot.wake:
				continue
			}
		}
		if !builtinID(id) && artifact == nil {
			if m.options.PluginStopped != nil {
				m.options.PluginStopped(id)
			}
			state, reason := artifactFailure(artifactErr)
			m.publish(id, slot, want, nil, state, reason)
			select {
			case <-m.ctx.Done():
				return
			case <-slot.wake:
				continue
			}
		}
		if instance == nil {
			if attempts >= 4 {
				m.publish(id, slot, want, nil, "failed", "PLUGIN_RESTART_LIMIT")
				select {
				case <-m.ctx.Done():
					return
				case <-slot.wake:
					continue
				}
			}
			if attempts > 0 {
				timer := time.NewTimer(time.Duration(1<<(attempts-1)) * 250 * time.Millisecond)
				select {
				case <-m.ctx.Done():
					timer.Stop()
					return
				case <-slot.wake:
					timer.Stop()
					continue
				case <-timer.C:
				}
			}
			m.publish(id, slot, want, nil, "starting", "")
			var cfg Config
			launchErr := m.validate(id, want.Values)
			if launchErr == nil {
				cfg, launchErr = m.options.Config(id, want.Values)
				if launchErr == nil && artifact != nil {
					cfg.Manifest = *artifact
					cfg.Policy.Capabilities = thirdPartyCallbacks(artifact.PluginManifest)
				}
			}
			if launchErr == nil {
				instance, launchErr = Launch(m.ctx, cfg)
			}
			if launchErr == nil && m.options.Ready != nil {
				ready, cancel := context.WithTimeout(m.ctx, 10*time.Second)
				launchErr = m.options.Ready(ready, id, instance.lifecycle)
				cancel()
			}
			if launchErr != nil {
				var pending *launchCleanupError
				if errors.As(launchErr, &pending) {
					<-pending.done
				}
				if instance != nil {
					_ = instance.Stop(context.Background())
					instance = nil
				}
				attempts++
				if errors.Is(launchErr, ErrIncompatible) {
					m.publish(id, slot, want, nil, "incompatible", "PLUGIN_INCOMPATIBLE")
					select {
					case <-m.ctx.Done():
						return
					case <-slot.wake:
						continue
					}
				}
				reason := "PLUGIN_START_FAILED"
				if errors.Is(launchErr, os.ErrNotExist) {
					reason = "PLUGIN_ARTIFACT_MISSING"
				}
				m.publish(id, slot, want, nil, "failed", reason)
				continue
			}
			current, err := m.desired(id)
			_, _, latestGeneration := m.artifact(id)
			if err != nil || current.Revision != want.Revision || latestGeneration != generation {
				_ = instance.Stop(context.Background())
				instance = nil
				continue
			}
			started = time.Now()
			m.mu.Lock()
			if slot.generation != generation {
				m.mu.Unlock()
				_ = instance.Stop(context.Background())
				instance = nil
				continue
			}
			m.manifests[id] = cfg.Manifest.PluginManifest
			m.mu.Unlock()
			if !m.publish(id, slot, want, instance, "ready", "") {
				_ = instance.Stop(context.Background())
				instance = nil
				continue
			}
			// Notify the SettingsRegistry after publish so the plugin's
			// config_schema and UI contributions are available to the agent.
			if m.options.PluginRegistered != nil {
				settings := buildPluginSettings(id, cfg.Manifest.PluginManifest)
				m.options.PluginRegistered(id, settings)
			}
		}
		select {
		case <-m.ctx.Done():
			return
		case <-slot.wake:
		case <-instance.Done():
			if m.options.PluginStopped != nil {
				m.options.PluginStopped(id)
			}
			m.stop(slot)
			instance = nil
			if time.Since(started) >= time.Minute {
				attempts = 0
			}
			attempts++
			m.publish(id, slot, want, nil, "failed", "PLUGIN_EXITED")
		}
	}
}
func (m *Supervisor) Close() error {
	m.closeOnce.Do(func() { m.startMu.Lock(); m.cancel(); m.startMu.Unlock(); m.done.Wait(); m.closeErr = m.store.Close() })
	return m.closeErr
}

// current is internal to the authenticated host adapters. The returned process
// remains leased by its own cancellation context even after a route is retired.
func (m *Supervisor) current(id string) *Instance {
	state, err := m.store.View()
	if err != nil {
		return nil
	}
	var want desiredPlugin
	if state.Get("plugins", id, &want) != nil || !want.Enabled {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slot := m.slots[id]
	if slot != nil && !builtinID(id) && (slot.artifact == nil || slot.instance != nil && !sameArtifact(slot.artifact, &slot.instance.artifact)) {
		return nil
	}
	if slot == nil || slot.status.Revision != want.Revision || slot.status.State != "ready" || !instanceReady(slot.instance) {
		return nil
	}
	return slot.instance
}

// The run loop can still be joining an expired worker while its last published
// status says ready. Derive admission and advertised capabilities from the live
// instance as well; this does not change durable enabled intent or its revision.
func instanceReady(instance *Instance) bool {
	if instance == nil || instance.grant == nil || instance.grant.lifetime == nil || instance.grant.lifetime.Err() != nil {
		return false
	}
	instance.grant.mu.Lock()
	valid := instance.grant.valid && time.Now().Before(instance.grant.deadline)
	instance.grant.mu.Unlock()
	if !valid {
		return false
	}
	select {
	case <-instance.Done():
		return false
	default:
	}
	instance.mu.Lock()
	ready := instance.snapshot.State == "ready"
	instance.mu.Unlock()
	return ready
}

// ReadyInstanceID exposes only the current live process identity for bounded
// diagnostic consistency checks; callers never receive a mutable worker.
func (m *Supervisor) ReadyInstanceID(id string) string {
	instance := m.current(id)
	if instance == nil || instance.grant.lifetime.Err() != nil {
		return ""
	}
	select {
	case <-instance.Done():
		return ""
	default:
	}
	return instance.Snapshot().InstanceID
}

// Start is separate from construction so hosts can bind CoreAPI callbacks and
// routing before an enabled worker begins Init during restart recovery.
func (m *Supervisor) Start() {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.started || m.ctx.Err() != nil {
		return
	}
	m.started = true
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, slot := range m.slots {
		m.done.Add(1)
		go m.run(id, slot)
	}
}
func (m *Supervisor) Settings(id string) (*structpb.Struct, string, error) {
	want, err := m.desired(id)
	if err != nil {
		return nil, "", err
	}
	if want.Values == nil {
		return &structpb.Struct{}, want.Revision, nil
	}
	return proto.Clone(want.Values).(*structpb.Struct), want.Revision, nil
}

// PluginManifest returns a detached validated declaration. Third-party metadata
// is available before enable; built-in metadata is retained after first launch.
func (m *Supervisor) PluginManifest(id string) *sdk.PluginManifest {
	if err := m.refresh(); err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return clonePluginManifest(m.manifests[id])
}

// Static, validated metadata remains authoritative; a process cannot expand its
// config schema or UI declarations after launch.
func buildPluginSettings(id string, manifest *sdk.PluginManifest) *PluginSettings {
	manifest = clonePluginManifest(manifest)
	settings := &PluginSettings{
		PluginID: id,
		Manifest: manifest,
	}
	if manifest != nil {
		settings.DisplayName = manifest.DisplayName
		settings.ConfigSchema = manifest.ConfigSchema
		settings.UI = manifest.UI
	}
	return settings
}
