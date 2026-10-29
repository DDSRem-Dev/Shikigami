package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/proto"
)

const createNamespace = "session.create"

type ManagerOptions struct {
	User                   string
	ClaudeCommand          []string
	MaxSessions, MaxClaude int
	Terminal               TerminalOptions
	AutoRestart            RestartPolicy
	AutoCleanup            CleanupPolicy
}

type Manager struct {
	store                  *storage.Store
	tmux                   *tmux.Client
	opts                   ManagerOptions
	ctx                    context.Context
	cancel                 context.CancelFunc
	operations             sync.Mutex
	mu                     sync.Mutex
	bridges                map[string]*Bridge
	closed                 bool
	restartWG              sync.WaitGroup
	activityMu             sync.Mutex
	activity               map[string]int64
	lastActivityFlush      time.Time
	lastCleanupObservation time.Time
	cleanupSeen            map[string]bool
	cleanupInvalidated     atomic.Bool
	crashNotified          map[string]string // runtime process identity; protected by operations
}

func NewManager(ctx context.Context, store *storage.Store, mux *tmux.Client, opts ManagerOptions) (*Manager, error) {
	if store == nil || mux == nil || opts.User == "" {
		return nil, errors.New("session store, tmux and run user required")
	}
	if len(opts.ClaudeCommand) == 0 || !filepath.IsAbs(opts.ClaudeCommand[0]) {
		return nil, errors.New("Claude command must contain a resolved executable")
	}
	if opts.MaxSessions <= 0 {
		opts.MaxSessions = 10
	}
	if opts.MaxClaude <= 0 {
		opts.MaxClaude = 5
	}
	opts.ClaudeCommand = append([]string(nil), opts.ClaudeCommand...)
	if err := opts.AutoCleanup.validate(); err != nil {
		return nil, err
	}
	if err := opts.AutoRestart.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{store: store, tmux: mux, opts: opts, ctx: ctx, cancel: cancel, bridges: map[string]*Bridge{}, cleanupSeen: map[string]bool{}, activity: map[string]int64{}, crashNotified: map[string]string{}}
	// Exit observation is required even when automatic restart is disabled.
	m.restartWG.Go(m.watchProcesses)
	return m, nil
}

func (m *Manager) Create(ctx context.Context, request *pb.CreateSessionRequest) (*pb.Session, error) {
	if request == nil || request.RequestId == "" || len(request.RequestId) > 128 {
		return nil, rpcError(connect.CodeInvalidArgument, "request_id required, maximum 128 bytes")
	}
	request = proto.Clone(request).(*pb.CreateSessionRequest)
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := m.recover(ctx); err != nil {
		return nil, err
	}
	var result *pb.Session
	var replayError error
	var replay bool
	err = m.store.Update(func(state *storage.Snapshot) (transactionError error) {
		record, exists, err := state.Request(createNamespace, request.RequestId, request)
		if err != nil {
			return storageError(err)
		}
		if exists {
			replay = true
			switch record.Status {
			case "succeeded":
				result = new(pb.Session)
				return json.Unmarshal(record.Result, result)
			case "failed":
				code := connect.CodeFailedPrecondition
				if err := code.UnmarshalText([]byte(record.ErrorCode)); err != nil {
					return err
				}
				replayError = rpcError(code, "original session creation failed")
				return nil
			default:
				replayError = rpcError(connect.CodeUnavailable, "session creation pending recovery")
				return nil
			}
		}
		// Persist deterministic admission failures alongside the request, so a
		// later quota/name change cannot turn the same failed request into a create.
		defer func() {
			if transactionError == nil {
				return
			}
			code := connect.CodeOf(transactionError)
			switch code {
			case connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodePermissionDenied, connect.CodeAlreadyExists, connect.CodeResourceExhausted, connect.CodeFailedPrecondition:
				replay, replayError = true, transactionError
				transactionError = state.CompleteRequest(createNamespace, request.RequestId, "", nil, code.String())
			}
		}()
		if err := storage.ValidateName(request.Name); err != nil {
			return rpcError(connect.CodeInvalidArgument, "invalid session name")
		}
		if err := validateMetadata(request.Description, request.Tags, request.Metadata); err != nil {
			return err
		}
		project := new(pb.Project)
		if err := state.Get(storage.ProjectsBucket, request.ProjectId, project); err != nil {
			return storageError(err)
		}
		directory, err := sessionDirectory(project.Path, request.WorkingDir)
		if err != nil {
			return err
		}
		count, claudeCount := 0, 0
		for _, data := range state.Buckets[storage.SessionsBucket] {
			var other pb.Session
			if err := json.Unmarshal(data, &other); err != nil {
				return err
			}
			if other.ProjectId == request.ProjectId && other.Name == request.Name {
				return rpcError(connect.CodeAlreadyExists, "session name already exists in project")
			}
			// Failed records remain only when process cleanup is unconfirmed;
			// those resources still consume a session slot.
			count++
			if other.ClaudeStatus == "running" || other.ClaudeStatus == "starting" || other.ClaudeStatus == "unknown" {
				claudeCount++
			}
		}
		autoStart := request.AutoStartClaude == nil || *request.AutoStartClaude
		if autoStart {
			if err := m.checkClaude(); err != nil {
				return err
			}
		}
		if count >= m.opts.MaxSessions || autoStart && claudeCount >= m.opts.MaxClaude {
			return rpcError(connect.CodeResourceExhausted, "session or Claude process quota reached")
		}
		id := "sess_" + rand.Text()
		if _, exists := state.Buckets[storage.SessionsBucket][id]; exists {
			return rpcError(connect.CodeAlreadyExists, "generated session ID collision; retry with a new request")
		}
		status := "stopped"
		if autoStart {
			status = "starting"
		}
		result = &pb.Session{Id: id, ProjectId: project.Id, Name: request.Name, WorkingDir: directory, Description: request.Description, Tags: request.Tags, Metadata: request.Metadata, AutoStartClaude: &autoStart, CreatedAt: time.Now().Unix(), LastActive: time.Now().Unix(), Lifecycle: "creating", ClaudeStatus: status, TmuxSession: "shiki-" + id}
		if err := state.Put(storage.SessionsBucket, id, result); err != nil {
			return err
		}
		record.ResourceID = id
		state.Requests[createNamespace+"\x00"+request.RequestId] = record
		return nil
	})
	if err != nil {
		return nil, storageError(err)
	}
	if replay {
		return result, replayError
	}
	var command []string
	if result.GetAutoStartClaude() {
		command = m.opts.ClaudeCommand
	}
	pane, createErr := m.tmux.Create(ctx, result.Id, result.WorkingDir, command)
	if createErr != nil {
		result.Lifecycle, result.ClaudeStatus = "failed", "stopped"
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, inspectErr := m.tmux.Inspect(cleanup, result.Id)
		cancel()
		if !errors.Is(inspectErr, tmux.ErrNotFound) {
			result.ClaudeStatus = "unknown"
		}
		code := connect.CodeFailedPrecondition
		if errors.Is(createErr, context.Canceled) {
			code = connect.CodeCanceled
		} else if errors.Is(createErr, context.DeadlineExceeded) {
			code = connect.CodeDeadlineExceeded
		}
		if err := m.completeCreation(request.RequestId, result, code.String(), errors.Is(inspectErr, tmux.ErrNotFound)); err != nil {
			return nil, storageError(err)
		}
		return nil, connect.NewError(code, "tmux session creation failed").WithCause(createErr)
	}
	result.Lifecycle = "ready"
	applyPane(result, pane)
	if err := m.completeCreation(request.RequestId, result, "", false); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, storageError(errors.Join(err, m.tmux.Delete(cleanup, result.Id)))
	}
	return proto.Clone(result).(*pb.Session), nil
}

func (m *Manager) completeCreation(requestID string, value *pb.Session, errorCode string, cleanupConfirmed bool) error {
	return m.store.Update(func(state *storage.Snapshot) error {
		if cleanupConfirmed {
			delete(state.Buckets[storage.SessionsBucket], value.Id)
		} else if err := state.Put(storage.SessionsBucket, value.Id, value); err != nil {
			return err
		}
		return state.CompleteRequest(createNamespace, requestID, value.Id, value, errorCode)
	})
}

func sessionDirectory(root, requested string) (string, error) {
	if requested == "" {
		requested = root
	} else if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	canonical, handle, err := project.OpenWorkingDirectory(root, filepath.Clean(requested))
	if err != nil {
		return "", err
	}
	if err = handle.Close(); err != nil {
		return "", storageError(err)
	}
	return canonical, nil
}

func (m *Manager) checkClaude() error {
	if _, err := exec.LookPath(m.opts.ClaudeCommand[0]); err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, "configured Claude executable is missing or not executable").WithCause(err)
	}
	return nil
}

// Tie external lifecycle operations to shutdown as well as the initiating RPC.
// Close cancels m.ctx before taking operations, so it can wake an in-flight
// tmux command instead of waiting indefinitely behind that command's lock.
func (m *Manager) operationContext(parent context.Context) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	if m.ctx.Err() != nil {
		return nil, nil, rpcError(connect.CodeUnavailable, "session manager stopped")
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() { stop(); cancel() }, nil
}

func storageError(err error) error {
	var rpcErr *connect.Error
	switch {
	case errors.As(err, &rpcErr):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, os.ErrNotExist):
		return rpcError(connect.CodeNotFound, "resource or directory not found")
	case errors.Is(err, os.ErrPermission):
		return rpcError(connect.CodePermissionDenied, "resource permission denied")
	case errors.Is(err, storage.ErrRequestMismatch):
		return rpcError(connect.CodeInvalidArgument, "request_id reused with different parameters")
	default:
		return connect.NewError(connect.CodeUnavailable, "session state or process operation unavailable").WithCause(err)
	}
}

func applyPane(session *pb.Session, pane tmux.Pane) {
	session.TmuxWindow, session.TmuxPane = pane.WindowID, pane.PaneID
	if session.ClaudeStatus == "stopped" {
		session.ClaudeStatus = "stopped"
		return
	}
	if pane.Dead {
		if pane.ExitSignalUnknown {
			session.ClaudeStatus = "unknown"
		} else if pane.ExitSignal != 0 {
			session.ClaudeStatus = "crashed"
		} else if pane.ExitStatus == nil {
			session.ClaudeStatus = "unknown"
		} else if *pane.ExitStatus == 0 {
			session.ClaudeStatus = "stopped"
		} else {
			session.ClaudeStatus = "crashed"
		}
	} else if session.ClaudeStatus != "unknown" {
		session.ClaudeStatus = "running"
	}
}

func validateMetadata(description string, tags []string, metadata map[string]string) error {
	if len(description) > 4096 || len(tags) > 64 || len(metadata) > 64 {
		return rpcError(connect.CodeResourceExhausted, "session metadata limit exceeded")
	}
	for _, tag := range tags {
		if len(tag) > 128 {
			return rpcError(connect.CodeInvalidArgument, "tag exceeds 128 bytes")
		}
	}
	for key, value := range metadata {
		if key == "" || len(key) > 128 || len(value) > 4096 {
			return rpcError(connect.CodeInvalidArgument, "invalid session metadata field")
		}
	}
	return nil
}

func (m *Manager) Get(ctx context.Context, request *pb.GetSessionRequest) (*pb.Session, error) {
	if request == nil || !storage.ValidResourceID(request.Id, "sess") {
		return nil, rpcError(connect.CodeInvalidArgument, "valid session ID required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, err
	}
	result := new(pb.Session)
	if err := state.Get(storage.SessionsBucket, request.Id, result); err != nil {
		return nil, storageError(err)
	}
	if result.Lifecycle == "ready" {
		pane, err := m.tmux.Inspect(ctx, result.Id)
		if errors.Is(err, tmux.ErrNotFound) {
			result.ClaudeStatus = "stopped"
			result.TmuxPane, result.TmuxWindow = "", ""
		} else if err != nil {
			return nil, err
		} else {
			applyPane(result, pane)
			if !pane.Dead && result.ClaudeStatus == "running" {
				result.ClaudePid = int32(pane.PID)
			}
		}
	}
	m.mu.Lock()
	bridge := m.bridges[result.Id]
	m.mu.Unlock()
	if bridge != nil {
		count, controller, revision := bridge.State()
		result.ClientCount = int32(count)
		result.IsConnected = count > 0
		result.ControllerAttachmentId = controller
		result.ControlRevision = revision
	}
	m.decorateCleanup(ctx, state, result)
	m.activityMu.Lock()
	result.LastActive = max(result.LastActive, m.activity[result.Id]/1000)
	m.activityMu.Unlock()
	return result, nil
}

func (m *Manager) List(ctx context.Context, request *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	if request == nil {
		request = &pb.ListSessionsRequest{}
	}
	if request.User != "" && request.User != m.opts.User {
		return nil, rpcError(connect.CodePermissionDenied, "user filter must match daemon owner")
	}
	state, err := m.store.View()
	if err != nil {
		return nil, err
	}
	result := new(pb.ListSessionsResponse)
	for id := range state.Buckets[storage.SessionsBucket] {
		s, err := m.Get(ctx, &pb.GetSessionRequest{Id: id})
		if err != nil {
			return nil, err
		}
		if request.Status != "" && request.Status != s.ClaudeStatus {
			continue
		}
		matches := true
		for _, tag := range request.Tags {
			found := false
			for _, actual := range s.Tags {
				if tag == actual {
					found = true
					break
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if matches {
			result.Sessions = append(result.Sessions, s)
		}
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		a, b := result.Sessions[i], result.Sessions[j]
		if a.IsConnected != b.IsConnected {
			return a.IsConnected
		}
		if a.LastActive != b.LastActive {
			return a.LastActive > b.LastActive
		}
		return a.Id < b.Id
	})
	return result, nil
}

func (m *Manager) Update(ctx context.Context, request *pb.UpdateSessionRequest) (*pb.Session, error) {
	if request == nil || request.UpdateMask == nil || len(request.UpdateMask.Paths) == 0 {
		return nil, rpcError(connect.CodeInvalidArgument, "update_mask required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateMetadata(request.Description, request.Tags, request.Metadata); err != nil {
		return nil, err
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	request = proto.Clone(request).(*pb.UpdateSessionRequest)
	result := new(pb.Session)
	err = m.store.Update(func(state *storage.Snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := state.Get(storage.SessionsBucket, request.Id, result); err != nil {
			return storageError(err)
		}
		for _, field := range request.UpdateMask.Paths {
			switch field {
			case "name":
				if err := storage.ValidateName(request.Name); err != nil {
					return rpcError(connect.CodeInvalidArgument, "invalid session name")
				}
				for id, data := range state.Buckets[storage.SessionsBucket] {
					if id == result.Id {
						continue
					}
					var other pb.Session
					if err := json.Unmarshal(data, &other); err != nil {
						return err
					}
					if other.ProjectId == result.ProjectId && other.Name == request.Name {
						return rpcError(connect.CodeAlreadyExists, "session name already exists in project")
					}
				}
				result.Name = request.Name
			case "description":
				result.Description = request.Description
			case "tags":
				result.Tags = append([]string(nil), request.Tags...)
			case "metadata":
				result.Metadata = request.Metadata
			default:
				return rpcError(connect.CodeInvalidArgument, "only name, description, tags and metadata may be updated")
			}
		}
		return state.Put(storage.SessionsBucket, result.Id, result)
	})
	if err != nil {
		return nil, storageError(err)
	}
	return result, nil
}

func (m *Manager) Terminal(id string) (*Bridge, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	state, err := m.store.View()
	if err != nil {
		return nil, err
	}
	value := new(pb.Session)
	if err := state.Get(storage.SessionsBucket, id, value); err != nil {
		return nil, storageError(err)
	}
	if value.Lifecycle != "ready" {
		return nil, rpcError(connect.CodeFailedPrecondition, "session is not ready")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, rpcError(connect.CodeUnavailable, "session manager stopped")
	}
	if bridge := m.bridges[id]; bridge != nil {
		return bridge, nil
	}
	bridge := NewBridge(m.ctx, id, func(ctx context.Context, id string, rows, cols uint32) (Terminal, error) {
		return m.tmux.Attach(ctx, id, rows, cols)
	}, m.opts.Terminal)
	bridge.activity = func() { m.recordActivity(id) }
	m.bridges[id] = bridge
	return bridge, nil
}

func (m *Manager) Close() error {
	m.cancel()
	m.restartWG.Wait()
	m.operations.Lock()
	defer m.operations.Unlock()
	m.mu.Lock()
	m.closed = true
	bridges := m.bridges
	m.bridges = map[string]*Bridge{}
	m.mu.Unlock()
	var err error
	for _, bridge := range bridges {
		err = errors.Join(err, bridge.Close())
	}
	err = errors.Join(err, m.flushActivity(time.Now(), true))
	return err
}
