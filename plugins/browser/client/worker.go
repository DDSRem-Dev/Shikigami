// Package client owns the authenticated loopback viewer. It never starts Chrome
// or opens VNC/CDP/TCP destinations supplied by a browser page.
package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/types/known/structpb"
)

var scopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type viewerConfig struct {
	maxSessions      int
	bootstrapTTL     time.Duration
	sessionTTL       time.Duration
	maxFileBytes     int64
	operationTimeout time.Duration
}

func parseConfig(in *structpb.Struct) (viewerConfig, error) {
	cfg := viewerConfig{8, 120 * time.Second, 8 * time.Hour, 64 << 20, 30 * time.Second}
	fields := in.GetFields()
	integer := func(key string, fallback, min, max int64) (int64, error) {
		v, ok := fields[key]
		if !ok {
			return fallback, nil
		}
		n, ok := v.Kind.(*structpb.Value_NumberValue)
		if !ok || n.NumberValue < float64(min) || n.NumberValue > float64(max) || float64(int64(n.NumberValue)) != n.NumberValue {
			return 0, fmt.Errorf("%s must be an integer in %d..%d", key, min, max)
		}
		return int64(n.NumberValue), nil
	}
	maxSessions, err := integer("viewer_max_sessions", int64(cfg.maxSessions), 1, 64)
	if err != nil {
		return cfg, err
	}
	bootstrap, err := integer("viewer_bootstrap_seconds", 120, 15, 600)
	if err != nil {
		return cfg, err
	}
	session, err := integer("viewer_session_seconds", 28800, 300, 86400)
	if err != nil {
		return cfg, err
	}
	maxFile, err := integer("viewer_max_file_bytes", cfg.maxFileBytes, 1, 1<<30)
	if err != nil {
		return cfg, err
	}
	operation, err := integer("viewer_operation_seconds", 30, 5, 300)
	if err != nil {
		return cfg, err
	}
	cfg.maxSessions, cfg.bootstrapTTL, cfg.sessionTTL = int(maxSessions), time.Duration(bootstrap)*time.Second, time.Duration(session)*time.Second
	cfg.maxFileBytes, cfg.operationTimeout = maxFile, time.Duration(operation)*time.Second
	return cfg, nil
}

// ValidateConfig validates only the client viewer keys. Server browser settings
// are interpreted by the server-side implementation, not by this viewer.
func ValidateConfig(in *structpb.Struct) error {
	_, err := parseConfig(in)
	return err
}

type Worker struct {
	mu          sync.Mutex
	remote      sdk.BrowserClient
	config      viewerConfig
	transferDir string
	lifetime    context.Context
	cancel      context.CancelFunc
	server      *http.Server
	host        string
	origin      string
	serveDone   chan struct{}
	serveErr    error
	closed      bool
	viewers     map[string]*viewer
	bootstrap   map[[32]byte]string
	requests    sync.WaitGroup
}

type viewer struct {
	id            string
	serverID      string
	profileID     string
	readOnly      bool
	bootstrapHash [32]byte
	cookieHash    [32]byte
	csrf          string
	expires       time.Time // guarded by Worker.mu, including bootstrap consumption
	bootstrapped  bool
	timer         *time.Timer
	ctx           context.Context
	cancel        context.CancelFunc
	fileSlot      chan struct{}
	mu            sync.Mutex
	connection    *attachment
	reconnecting  bool // at most one bounded claimant waiting for attachment cleanup
}

func New() *Worker {
	return &Worker{viewers: make(map[string]*viewer), bootstrap: make(map[[32]byte]string)}
}

var _ sdk.Implementation = (*Worker)(nil)
var _ sdk.BrowserLocalService = (*Worker)(nil)

func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "browser", Version: buildinfo.Version, Side: "client", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.BrowserViewerCapability}}}
}

func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Side != "client" || in.Core == nil || !filepath.IsAbs(in.StateDir) {
		return connect.NewError(connect.CodeInvalidArgument, "INVALID_BROWSER_CLIENT_INITIALIZATION")
	}
	core, ok := in.Core.(sdk.ExtensionCore)
	if !ok || core.Browser() == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "BROWSER_CORE_EXTENSION_UNAVAILABLE")
	}
	cfg, err := parseConfig(in.Config)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.remote != nil || w.closed {
		return errors.New("browser viewer already initialized or stopped")
	}
	transferDir := filepath.Join(in.StateDir, "viewer-transfers")
	if err := os.MkdirAll(transferDir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(transferDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("browser viewer transfer directory must be a private directory")
	}
	// These files are private, incomplete downloads left by this plugin after a
	// process interruption. No user filename is ever used as a local path.
	entries, err := os.ReadDir(transferDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "download-") && entry.Type().IsRegular() {
			if err := os.Remove(filepath.Join(transferDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	w.remote, w.config, w.transferDir = core.Browser(), cfg, transferDir
	return nil
}

func (w *Worker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.remote == nil || w.closed {
		return connect.NewError(connect.CodeFailedPrecondition, "BROWSER_VIEWER_NOT_INITIALIZED")
	}
	if w.server != nil {
		if w.lifetime.Err() != nil || w.serveErr != nil {
			return connect.NewError(connect.CodeFailedPrecondition, "BROWSER_VIEWER_LIFETIME_ENDED")
		}
		return nil
	}
	// An IP literal avoids name resolution, wildcard binding, and DNS rebinding.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	w.lifetime, w.cancel = context.WithCancel(ctx)
	w.host, w.origin = listener.Addr().String(), "http://"+listener.Addr().String()
	w.serveDone = make(chan struct{})
	w.server = &http.Server{
		Handler:           http.HandlerFunc(w.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return w.lifetime },
	}
	server, lifetime := w.server, w.lifetime
	go func() {
		defer close(w.serveDone)
		err := server.Serve(listener)
		w.mu.Lock()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.serveErr = err
		}
		w.mu.Unlock()
		w.cancel()
	}()
	go func() {
		<-lifetime.Done()
		server.Close()
	}()
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	for _, v := range w.viewers {
		v.cancel()
		if v.timer != nil {
			v.timer.Stop()
		}
	}
	w.viewers = make(map[string]*viewer)
	w.bootstrap = make(map[[32]byte]string)
	server, done := w.server, w.serveDone
	w.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	if err != nil {
		server.Close()
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	requestsDone := make(chan struct{})
	go func() { w.requests.Wait(); close(requestsDone) }()
	select {
	case <-requestsDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Health(context.Context) (*pb.HealthStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	status := pb.HealthStatus_UNHEALTHY
	if !w.closed && w.lifetime != nil && w.lifetime.Err() == nil && w.serveErr == nil {
		status = pb.HealthStatus_HEALTHY
	}
	return &pb.HealthStatus{Status: status}, nil
}

func randomSecret(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (w *Worker) OpenBrowserViewer(ctx context.Context, in *epb.OpenBrowserViewerRequest) (*epb.BrowserViewer, error) {
	if in == nil || !scopeIdentifier.MatchString(in.ServerId) || !scopeIdentifier.MatchString(in.ProfileId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "INVALID_BROWSER_VIEWER_SCOPE")
	}
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, "BROWSER_VIEWER_NOT_RUNNING")
	}
	remote, cfg := w.remote, w.config
	w.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, cfg.operationTimeout)
	status, err := remote.GetBrowserStatus(checkCtx, &epb.BrowserScope{ServerId: in.ServerId, ProfileId: in.ProfileId})
	cancel()
	if err != nil {
		return nil, err
	}
	if status == nil || status.State != "running" || status.InstanceId == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "BROWSER_NOT_RUNNING")
	}
	id, err := randomSecret(16)
	if err != nil {
		return nil, err
	}
	bootstrap, err := randomSecret(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomSecret(32)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.closed || w.lifetime.Err() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "BROWSER_VIEWER_NOT_RUNNING")
	}
	if len(w.viewers) >= cfg.maxSessions {
		return nil, connect.NewError(connect.CodeResourceExhausted, "BROWSER_VIEWER_SESSION_LIMIT")
	}
	// The RPC context belongs to the CLI. It must not own this listener/session.
	lifetime, stop := context.WithCancel(w.lifetime)
	v := &viewer{id: id, serverID: in.ServerId, profileID: in.ProfileId, readOnly: in.ReadOnly, bootstrapHash: sha256.Sum256([]byte(bootstrap)), csrf: csrf, expires: time.Now().Add(cfg.bootstrapTTL), ctx: lifetime, cancel: stop, fileSlot: make(chan struct{}, 1)}
	w.viewers[id] = v
	w.bootstrap[v.bootstrapHash] = id
	w.armExpiryLocked(v)
	return &epb.BrowserViewer{Id: id, Url: w.origin + "/#" + bootstrap, State: "pending", ExpiresAt: v.expires.Unix()}, nil
}

func (w *Worker) armExpiryLocked(v *viewer) {
	deadline := v.expires
	v.timer = time.AfterFunc(time.Until(deadline), func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.viewers[v.id] == v && v.expires.Equal(deadline) {
			w.removeViewerLocked(v)
		}
	})
}

func (w *Worker) removeViewerLocked(v *viewer) {
	delete(w.viewers, v.id)
	delete(w.bootstrap, v.bootstrapHash)
	if v.timer != nil {
		v.timer.Stop()
	}
	v.cancel()
}

func (w *Worker) CloseBrowserViewer(ctx context.Context, in *epb.CloseBrowserViewerRequest) (*epb.BrowserViewer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in == nil || in.ViewerId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "INVALID_BROWSER_VIEWER_ID")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if v := w.viewers[in.ViewerId]; v != nil {
		w.removeViewerLocked(v)
	}
	return &epb.BrowserViewer{Id: in.ViewerId, State: "closed"}, nil
}
