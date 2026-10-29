package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"google.golang.org/protobuf/proto"
)

type Worker struct {
	mu                       sync.Mutex
	configure                sync.Mutex
	core                     sdk.CoreAPI
	stateDir, instance, host string
	store                    *storage.Store
	tasks                    map[string]*task
	lifetime                 context.Context
	cancel                   context.CancelFunc
	wake                     chan struct{}
	done                     chan struct{}
	mappingsDone             chan struct{}
	closed                   bool
	stopped                  chan struct{}
	stopErr                  error
	mappingFeedFailed        bool
	interval                 time.Duration
	maxFileBytes             int64
}
type task struct {
	op           sync.Mutex
	runMu        sync.Mutex
	run          context.Context
	cancel       context.CancelFunc
	watches      sync.WaitGroup
	worker       *Worker
	data         record
	local        *filetree.Tree
	remote       sdk.FileService
	staging      string
	stageDir     *stagingDirectory
	failures     int
	retryAt      time.Time
	retryBlocked bool
	nextScan     time.Time
}

func New() *Worker { return &Worker{tasks: map[string]*task{}, wake: make(chan struct{}, 1)} }
func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "sync", Version: buildinfo.Version, Side: "client", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.SyncCapability}}}
}
func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Core == nil || !filepath.IsAbs(in.StateDir) || in.Side != "client" {
		return fail(connect.CodeInvalidArgument, "SYNC_INIT_INVALID")
	}
	if err := storage.EnsurePrivateDir(in.StateDir); err != nil {
		return fail(connect.CodeFailedPrecondition, "SYNC_STATE_UNAVAILABLE")
	}
	config, err := schema.ParseSyncClient(in.Config)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.store != nil {
		return fail(connect.CodeFailedPrecondition, "SYNC_ALREADY_INITIALIZED")
	}
	store, err := storage.Open(filepath.Join(in.StateDir, "ledger"), ledgerOptions())
	if err != nil {
		return fail(connect.CodeFailedPrecondition, "SYNC_STATE_UNAVAILABLE")
	}
	if err = store.Update(func(snapshot *storage.Snapshot) error {
		for key, value := range snapshot.Requests {
			if value.Status == "pending" {
				value.Status = "failed"
				value.ErrorCode = "RESULT_UNKNOWN_RECONCILE_REQUIRED"
				snapshot.Requests[key] = value
			}
		}
		return nil
	}); err != nil {
		store.Close()
		return err
	}
	w.interval = config.Interval
	w.maxFileBytes = config.MaxAutoSyncSize
	w.store = store
	w.core = in.Core
	w.stateDir = in.StateDir
	w.instance = in.InstanceID
	w.host = in.HostInstanceID
	return nil
}
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.store == nil || w.closed || w.mappingFeedFailed {
		return fail(connect.CodeFailedPrecondition, "SYNC_NOT_INITIALIZED")
	}
	if w.lifetime != nil {
		return nil
	}
	w.lifetime, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	w.mappingsDone = make(chan struct{})
	go w.watchMappings()
	go w.loop()
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.stopped == nil {
		// Closing admission and requesting cancellation are not proof that
		// watchers, operations and persistent writers have finished draining.
		w.closed = true
		if w.cancel != nil {
			w.cancel()
		}
		w.stopped = make(chan struct{})
		go w.finishStop()
	}
	done := w.stopped
	w.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.stopErr
	}
}

// One cleanup owner survives individual Stop deadlines. Waiting for the
// configuration lock, watchers or file operations must not block a Stop caller
// past its deadline, nor allow a later caller to skip unfinished cleanup.
func (w *Worker) finishStop() {
	w.configure.Lock()
	w.mu.Lock()
	done := w.done
	mappingsDone := w.mappingsDone
	tasks := make([]*task, 0, len(w.tasks))
	for _, t := range w.tasks {
		tasks = append(tasks, t)
	}
	store := w.store
	w.mu.Unlock()
	w.configure.Unlock()
	for _, t := range tasks {
		t.pause()
	}
	if done != nil {
		<-done
	}
	if mappingsDone != nil {
		<-mappingsDone
	}
	var err error
	for _, t := range tasks {
		t.op.Lock()
		err = errors.Join(err, t.local.Close())
		if t.stageDir != nil {
			err = errors.Join(err, t.stageDir.Close())
		}
		t.op.Unlock()
	}
	if store != nil {
		err = errors.Join(err, store.Close())
	}
	w.mu.Lock()
	w.stopErr = err
	close(w.stopped)
	w.mu.Unlock()
}
func (w *Worker) Health(context.Context) (*pb.HealthStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := pb.HealthStatus_HEALTHY
	if w.store == nil || w.closed || w.mappingFeedFailed || w.lifetime != nil && w.lifetime.Err() != nil {
		state = pb.HealthStatus_UNHEALTHY
	}
	return &pb.HealthStatus{Status: state}, nil
}
func (w *Worker) notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (w *Worker) loop() {
	defer close(w.done)
	ticker := time.NewTicker(min(w.interval, 100*time.Millisecond))
	defer ticker.Stop()
	for {
		hinted := false
		select {
		case <-w.lifetime.Done():
			return
		case <-ticker.C:
		case <-w.wake:
			hinted = true
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-w.lifetime.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		w.mu.Lock()
		tasks := make([]*task, 0, len(w.tasks))
		for _, t := range w.tasks {
			tasks = append(tasks, t)
		}
		w.mu.Unlock()
		for _, t := range tasks {
			t.runMu.Lock()
			ctx := t.run
			t.runMu.Unlock()
			if ctx == nil || ctx.Err() != nil {
				continue
			}
			t.op.Lock()
			if ctx.Err() == nil && !t.retryBlocked && (t.retryAt.IsZero() || !time.Now().Before(t.retryAt)) && (hinted || !t.retryAt.IsZero() || t.nextScan.IsZero() || !time.Now().Before(t.nextScan)) {
				err := t.reconcile(ctx)
				t.recordRetry(ctx, err)
				t.nextScan = time.Now().Add(w.interval)
			}
			t.op.Unlock()
		}
	}
}
func (w *Worker) get(id string) (*task, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		return nil, fail(connect.CodeFailedPrecondition, "SYNC_WORKER_NOT_RUNNING")
	}
	t := w.tasks[id]
	if t == nil {
		return nil, fail(connect.CodeNotFound, "MAPPING_NOT_CONFIGURED")
	}
	return t, nil
}
func scopeFor(m *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: m.ServerId, ProjectId: m.ProjectId, MappingId: m.Id}
}
func (w *Worker) ConfigureMapping(ctx context.Context, mapping *ppb.Mapping) (*ppb.SyncState, error) {
	w.configure.Lock()
	defer w.configure.Unlock()
	return w.configureMapping(ctx, mapping)
}

func (w *Worker) configureMapping(ctx context.Context, mapping *ppb.Mapping) (*ppb.SyncState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if mapping == nil || !validID(mapping.Id, "map_") || !validID(mapping.ServerId, "srv_") || !pb.ValidResourceID(mapping.ProjectId, "proj") || mapping.Revision == "" || !filepath.IsAbs(mapping.LocalRoot) {
		return nil, fail(connect.CodeInvalidArgument, "INVALID_MAPPING")
	}
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		return nil, fail(connect.CodeFailedPrecondition, "SYNC_WORKER_NOT_RUNNING")
	}
	core := w.core
	t := w.tasks[mapping.Id]
	if t == nil && len(w.tasks) >= 128 {
		w.mu.Unlock()
		return nil, fail(connect.CodeResourceExhausted, "MAPPING_TASK_LIMIT")
	}
	w.mu.Unlock()
	authoritative, err := core.GetMapping(ctx, scopeFor(mapping))
	if err != nil {
		return nil, err
	}
	if !proto.Equal(authoritative, mapping) {
		return nil, fail(connect.CodeAborted, "MAPPING_CHANGED")
	}
	if t != nil {
		// ConfigureMapping is the only mapping writer and is serialized by
		// w.configure. Check identity before waiting for a long data operation;
		// pause must be able to cancel that operation first.
		old := t.data.Mapping
		if old.Revision == mapping.Revision {
			if !proto.Equal(old, mapping) {
				return nil, fail(connect.CodeAborted, "MAPPING_REVISION_REUSED")
			}
			// A repeated Configure must not wait behind a slow data stream:
			// doing so would prevent a later pause from acquiring configure.
			snapshot, err := w.store.View()
			if err != nil {
				return nil, err
			}
			var saved record
			if err = snapshot.Get(mappingsBucket, mapping.Id, &saved); err != nil {
				return nil, err
			}
			return saved.status(), nil
		}
		if old.ServerId != mapping.ServerId || old.ProjectId != mapping.ProjectId || old.LocalRoot != mapping.LocalRoot {
			return nil, fail(connect.CodeInvalidArgument, "MAPPING_IDENTITY_IMMUTABLE")
		}
		t.pause()
		t.op.Lock()
		defer t.op.Unlock()
		old = t.data.Mapping
		if old.ServerId != mapping.ServerId || old.ProjectId != mapping.ProjectId || old.LocalRoot != mapping.LocalRoot {
			return nil, fail(connect.CodeInvalidArgument, "MAPPING_IDENTITY_IMMUTABLE")
		}
	} else {
		snapshot, err := w.store.View()
		if err != nil {
			return nil, err
		}
		if snapshot.Buckets[mappingsBucket][mapping.Id] == nil && snapshot.Buckets[archivedMappingsBucket][mapping.Id] != nil {
			return nil, fail(connect.CodeFailedPrecondition, "MAPPING_ARCHIVED")
		}
		root, err := filepath.EvalSymlinks(mapping.LocalRoot)
		if err != nil || root != mapping.LocalRoot {
			return nil, fail(connect.CodeFailedPrecondition, "MAPPING_ROOT_CHANGED")
		}
		tree, err := filetree.Open(root, filetree.Options{StateDir: filepath.Join(w.stateDir, "trees", mapping.Id), ProjectID: mapping.ProjectId, MaxFileBytes: w.maxFileBytes, HistoryBytes: max(64<<20, w.maxFileBytes), OnSkipped: func(ctx context.Context, reason string) {
			logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, _ = core.Log(logCtx, &pb.LogRecord{Level: "warn", Message: reason})
		}})
		if err != nil {
			return nil, err
		}
		staging := filepath.Join(w.stateDir, "staging", mapping.Id)
		if err = storage.EnsurePrivateDir(staging); err != nil {
			tree.Close()
			return nil, err
		}
		stageDir, err := openStagingDirectory(staging)
		if err != nil {
			tree.Close()
			return nil, err
		}
		keepStageDir := false
		defer func() {
			if !keepStageDir {
				stageDir.Close()
			}
		}()
		t = &task{worker: w, local: tree, remote: remoteFiles{core: core, scope: scopeFor(mapping)}, staging: staging, stageDir: stageDir}
		if err = snapshot.Get(mappingsBucket, mapping.Id, &t.data); err != nil && !errors.Is(err, os.ErrNotExist) {
			tree.Close()
			return nil, err
		}
		if t.data.Mapping != nil && (t.data.Mapping.ServerId != mapping.ServerId || t.data.Mapping.ProjectId != mapping.ProjectId || t.data.Mapping.LocalRoot != mapping.LocalRoot) {
			tree.Close()
			return nil, fail(connect.CodeFailedPrecondition, "PERSISTED_MAPPING_IDENTITY_CHANGED")
		}
		if t.data.Baselines == nil {
			t.data.Baselines = map[string]*ppb.FileVersion{}
			t.data.Conflicts = map[string]*ppb.Conflict{}
			t.data.Copies = map[string]string{}
			t.data.Operations = map[string]*fileOperation{}
			t.data.LegacyUnverified = map[string]bool{}
		}
		if err = verifyStages(ctx, stageDir, t.data.Operations); err != nil {
			tree.Close()
			return nil, err
		}
		if err = cleanStaging(stageDir, t.data.Operations); err != nil {
			tree.Close()
			return nil, err
		}
		t.op.Lock()
		defer t.op.Unlock()
		w.mu.Lock()
		w.tasks[mapping.Id] = t
		keepStageDir = true
		w.mu.Unlock()
	}
	t.resetRetry()
	t.data.Mapping = proto.Clone(mapping).(*ppb.Mapping)
	t.data.State = "paused"
	t.data.Reason = ""
	if mapping.SyncEnabled {
		t.data.State = "reconciling"
	}
	if err = t.persist(); err != nil {
		return nil, err
	}
	if mapping.SyncEnabled {
		t.start()
	}
	w.notify()
	return t.data.status(), nil
}

func cleanStaging(directory *stagingDirectory, operations map[string]*fileOperation) error {
	retained := map[string]bool{}
	for _, op := range operations {
		if op.Stage != "" {
			retained[op.Stage] = true
		}
	}
	return directory.Cleanup(retained)
}

func (t *task) pause() {
	t.runMu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.run = nil
	t.runMu.Unlock()
	t.watches.Wait()
}
func (t *task) start() {
	t.runMu.Lock()
	t.run, t.cancel = context.WithCancel(t.worker.lifetime)
	ctx := t.run
	t.runMu.Unlock()
	project := t.data.Mapping.ProjectId
	for _, files := range []sdk.FileService{t.local, t.remote} {
		t.watches.Add(1)
		go func(files sdk.FileService) {
			defer t.watches.Done()
			for failures := 0; ; failures++ {
				err := files.WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: project, Path: ""}, func(*pb.FileEvent) error { t.worker.notify(); return ctx.Err() })
				if ctx.Err() != nil {
					return
				}
				t.worker.notify()
				code := connect.CodeOf(err)
				if failures >= 5 || (err != nil && code != connect.CodeUnavailable && code != connect.CodeResourceExhausted && code != connect.CodeDeadlineExceeded) {
					return
				}
				timer := time.NewTimer(retryDelay(failures))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}(files)
	}
}
func (t *task) persist() error {
	if err := t.worker.update(func(snapshot *storage.Snapshot) error {
		return snapshot.Put(mappingsBucket, t.data.Mapping.Id, &t.data)
	}); err != nil {
		return err
	}
	return cleanStaging(t.stageDir, t.data.Operations)
}

func (t *task) operationContext(parent context.Context, writing bool) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stopLife := context.AfterFunc(t.worker.lifetime, cancel)
	stopRun := func() bool { return true }
	if writing {
		t.runMu.Lock()
		run := t.run
		t.runMu.Unlock()
		if run != nil {
			stopRun = context.AfterFunc(run, cancel)
		}
	}
	return ctx, func() { stopRun(); stopLife(); cancel() }
}
func (w *Worker) GetSyncState(_ context.Context, scope *ppb.Scope) (*ppb.SyncState, error) {
	t, err := w.get(scope.GetMappingId())
	if err != nil {
		return nil, err
	}
	t.op.Lock()
	defer t.op.Unlock()
	// A pending status call may have waited behind a mutation whose durable
	// commit failed. Do not advertise its now-canceled in-memory state.
	current, err := w.get(scope.GetMappingId())
	if err != nil {
		return nil, err
	}
	if current != t {
		return nil, fail(connect.CodeFailedPrecondition, "MAPPING_CHANGED")
	}

	if scope.ServerId != "" && scope.ServerId != t.data.Mapping.ServerId || scope.ProjectId != "" && scope.ProjectId != t.data.Mapping.ProjectId {
		return nil, fail(connect.CodePermissionDenied, "MAPPING_SCOPE_MISMATCH")
	}
	return t.data.status(), nil
}

var _ sdk.Implementation = (*Worker)(nil)
var _ sdk.SyncWorker = (*Worker)(nil)

func (w *Worker) update(fn func(*storage.Snapshot) error) error {
	err := w.store.Update(fn)
	if err != nil {
		var ce *connect.Error
		if !errors.As(err, &ce) {
			w.mu.Lock()
			if w.cancel != nil {
				w.cancel()
			}
			w.mu.Unlock()
		}
	}
	return err
}
