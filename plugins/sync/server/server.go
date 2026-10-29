package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"google.golang.org/protobuf/types/known/structpb"
)

type Worker struct {
	mu           sync.Mutex
	core         sdk.CoreAPI
	stateDir     string
	maxFileBytes int64
	lifetime     context.Context
	cancel       context.CancelFunc
	closed       bool
	active       sync.WaitGroup
	operations   [3]int
	stopped      chan struct{}
	stopErr      error
	trees        map[string]*filetree.Tree
	roots        map[string]string
}

func New() *Worker { return &Worker{trees: map[string]*filetree.Tree{}, roots: map[string]string{}} }
func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "sync", Version: buildinfo.Version, Side: "server", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.FileCapability}}}
}
func ValidateConfig(config *structpb.Struct) error { return schema.ValidateSyncServer(config) }

func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Core == nil || in.Side != "server" || !filepath.IsAbs(in.StateDir) {
		return errors.New("invalid server sync initialization")
	}
	config, err := schema.ParseSyncServer(in.Config)
	if err != nil {
		return err
	}
	if err := storage.EnsurePrivateDir(in.StateDir); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.core != nil {
		return errors.New("already initialized")
	}
	w.core = in.Core
	w.stateDir = in.StateDir
	w.maxFileBytes = config.MaxAutoSyncSize
	return nil
}
func (w *Worker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.core == nil || w.closed || (w.lifetime != nil && w.lifetime.Err() != nil) {
		return errors.New("server sync is not initialized")
	}
	if w.lifetime == nil {
		w.lifetime, w.cancel = context.WithCancel(ctx)
	}
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.stopped == nil {
		w.closed = true
		if w.cancel != nil {
			w.cancel()
		}
		w.stopped = make(chan struct{})
		go func() {
			w.active.Wait()
			w.mu.Lock()
			defer w.mu.Unlock()
			for _, tree := range w.trees {
				w.stopErr = errors.Join(w.stopErr, tree.Close())
			}
			close(w.stopped)
		}()
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
func (w *Worker) Health(context.Context) (*pb.HealthStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := pb.HealthStatus_HEALTHY
	if w.lifetime == nil || w.closed || w.lifetime.Err() != nil {
		state = pb.HealthStatus_UNHEALTHY
	}
	return &pb.HealthStatus{Status: state}, nil
}
func (w *Worker) tree(ctx context.Context, projectID string) (*filetree.Tree, error) {
	if !pb.ValidResourceID(projectID, "proj") {
		return nil, connect.NewError(connect.CodeInvalidArgument, "INVALID_PROJECT_ID")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	core := w.core
	w.mu.Unlock()
	project, err := core.GetProject(ctx, &ppb.Scope{ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	if project.Id != projectID || !filepath.IsAbs(project.Path) {
		return nil, connect.NewError(connect.CodePermissionDenied, "PROJECT_ROOT_INVALID")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, connect.NewError(connect.CodeCanceled, "SYNC_STOPPING")
	}
	tree := w.trees[projectID]
	if tree != nil && w.roots[projectID] != project.Path {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "PROJECT_ROOT_CHANGED")
	}
	if tree == nil {
		tree, err = filetree.Open(project.Path, filetree.Options{ProjectID: projectID, StateDir: filepath.Join(w.stateDir, "projects", projectID), MaxFileBytes: w.maxFileBytes, HistoryBytes: max(64<<20, w.maxFileBytes), OnSkipped: func(ctx context.Context, reason string) {
			logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, _ = core.Log(logCtx, &pb.LogRecord{Level: "warn", Message: reason})
		}})
		if err != nil {
			return nil, err
		}
		w.trees[projectID] = tree
		w.roots[projectID] = project.Path
	}
	return tree, nil
}
func (w *Worker) ListFiles(ctx context.Context, in *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	ctx, done, err := w.begin(ctx, metadataOperation)
	if err != nil {
		return nil, err
	}
	defer done()
	tree, err := w.tree(ctx, in.GetProjectId())
	if err != nil {
		return nil, err
	}
	return tree.ListFiles(ctx, in)
}
func (w *Worker) StatFile(ctx context.Context, in *pb.StatFileRequest) (*pb.FileInfo, error) {
	ctx, done, err := w.begin(ctx, metadataOperation)
	if err != nil {
		return nil, err
	}
	defer done()
	tree, err := w.tree(ctx, in.GetProjectId())
	if err != nil {
		return nil, err
	}
	return tree.StatFile(ctx, in)
}
func (w *Worker) ReadFile(ctx context.Context, in *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	ctx, done, err := w.begin(ctx, transferOperation)
	if err != nil {
		return err
	}
	defer done()
	tree, err := w.tree(ctx, in.GetProjectId())
	if err != nil {
		return err
	}
	return tree.ReadFile(ctx, in, send)
}
func (w *Worker) DeleteFile(ctx context.Context, in *pb.DeleteFileRequest) error {
	ctx, done, err := w.begin(ctx, metadataOperation)
	if err != nil {
		return err
	}
	defer done()
	tree, err := w.tree(ctx, in.GetProjectId())
	if err != nil {
		return err
	}
	return tree.DeleteFile(ctx, in)
}
func (w *Worker) WatchFiles(ctx context.Context, in *pb.WatchFilesRequest, send func(*pb.FileEvent) error) error {
	ctx, done, err := w.begin(ctx, watchOperation)
	if err != nil {
		return err
	}
	defer done()
	tree, err := w.tree(ctx, in.GetProjectId())
	if err != nil {
		return err
	}
	return tree.WatchFiles(ctx, in, send)
}
func (w *Worker) WriteFile(ctx context.Context, receive func(context.Context) (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	ctx, done, err := w.begin(ctx, transferOperation)
	if err != nil {
		return nil, err
	}
	defer done()
	headerCtx, cancelHeader := context.WithTimeout(ctx, limits.FileFirstFrameTimeout)
	first, err := receive(headerCtx)
	cancelHeader()
	if err != nil {
		return nil, err
	}
	if first.GetHeader() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "FILE_HEADER_REQUIRED")
	}
	tree, err := w.tree(ctx, first.GetHeader().ProjectId)
	if err != nil {
		return nil, err
	}
	initial := true
	return tree.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if initial {
			initial = false
			return first, nil
		}
		return receive(ctx)
	})
}

var _ sdk.Implementation = (*Worker)(nil)
var _ sdk.FileWorkerService = (*Worker)(nil)
