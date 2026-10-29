// Package plugin supervises plugin processes and their private CoreAPI callback
// listeners. Business I/O remains in the plugin workers.
package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type Manifest struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Executable string            `json:"executable"`
	SHA256     string            `json:"sha256"`
	Side       string            `json:"side"`
	Protocol   *pb.ProtocolOffer `json:"protocol"`
}
type Config struct {
	Manifest                           Manifest
	Args, Env                          []string
	RunDir, StateDir, HostInstanceID   string
	Policy                             Policy
	Core                               prpc.CoreAPIHandler
	Values                             *structpb.Struct
	CredentialLifetime, StartupTimeout time.Duration
}
type Snapshot struct {
	InstanceID, Name, Version, Side, State string
	PID                                    int
	Protocol                               *pb.ProtocolAgreement
}

type managedProcess interface {
	Client() (goplugin.ClientProtocol, error)
	Kill()
	ReattachConfig() *goplugin.ReattachConfig
}

type Instance struct {
	mu              sync.Mutex
	stopOnce        sync.Once
	stopErr         error
	handlerMu       sync.Mutex
	handlers        sync.WaitGroup
	admissionClosed bool
	snapshot        Snapshot
	process         managedProcess
	lifecycle       sdk.Lifecycle
	grant           *grant
	server          *http.Server
	serveDone       chan error
	clientDone      chan struct{}
	done            chan struct{}
	dir             string
	stateLock       *os.File
	cleanup         sync.Once
}

// A failed Launch may reach its deadline before owned cleanup completes. The
// error keeps only the completion channel, not the entire stopped instance.
// Supervisors join it before closing shared state or attempting a replacement.
type launchCleanupError struct {
	cause error
	done  <-chan struct{}
}

func (e *launchCleanupError) Error() string { return e.cause.Error() }
func (e *launchCleanupError) Unwrap() error { return e.cause }

// Launch's context is the host lifetime, not a short RPC request. Initialization
// has its own bounded deadline. It starts CoreAPI before launching the process.
// On a failed startup deadline, cleanup can continue after Launch returns; the
// instance keeps its writer lease until its process and callbacks have joined.
func Launch(ctx context.Context, cfg Config) (instance *Instance, err error) {
	if err = validateConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.CredentialLifetime <= 0 {
		cfg.CredentialLifetime = 12 * time.Hour
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 10 * time.Second
	}
	if err = privateDirectory(cfg.RunDir); err != nil {
		return nil, err
	}
	if err = privateDirectory(cfg.StateDir); err != nil {
		return nil, err
	}
	lock, err := lockStateDirectory(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	lockTransferred := false
	defer func() {
		if err != nil && !lockTransferred {
			_ = lock.Close()
		}
	}()
	dir, err := os.MkdirTemp(cfg.RunDir, "p-")
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithDeadline(ctx, time.Now().Add(cfg.CredentialLifetime))
	token := randomID()
	id := "plugin_" + randomID()
	policy := Policy{Side: cfg.Policy.Side, Capabilities: append([]string(nil), cfg.Policy.Capabilities...), AuthorizeScope: cfg.Policy.AuthorizeScope}
	for _, scope := range cfg.Policy.Scopes {
		if scope == nil {
			cancel()
			os.RemoveAll(dir)
			return nil, errors.New("nil plugin scope")
		}
		policy.Scopes = append(policy.Scopes, proto.Clone(scope).(*ppb.Scope))
	}
	grant := &grant{token: token, instance: id, host: cfg.HostInstanceID, deadline: time.Now().Add(cfg.CredentialLifetime), valid: true, policy: policy, lifetime: lifetime, cancel: cancel}
	i := &Instance{snapshot: Snapshot{InstanceID: id, Name: cfg.Manifest.Name, Version: cfg.Manifest.Version, Side: cfg.Manifest.Side, State: "starting"}, grant: grant, dir: dir, stateLock: lock, done: make(chan struct{})}
	lockTransferred = true
	startup, stopStartup := context.WithTimeout(lifetime, cfg.StartupTimeout)
	defer stopStartup()
	defer func() {
		if err != nil {
			// Failed startup still owns its process, callbacks and writer lease.
			// Its deadline bounds the caller, not the lifetime of cleanup.
			deadline, _ := startup.Deadline()
			wait, cancelWait := context.WithDeadline(ctx, deadline)
			defer cancelWait()
			i.beginStop(false)
			select {
			case <-i.done:
			case <-wait.Done():
				err = &launchCleanupError{cause: err, done: i.done}
			}
		}
	}()
	listener, err := connection.ListenUnix(filepath.Join(dir, "core.sock"), uint32(os.Getuid()))
	if err != nil {
		return nil, err
	}
	rpcServer := connect.NewServer(grant.interceptor)
	prpc.RegisterCoreAPIHandler(rpcServer, cfg.Core)
	rpcHandler := WithStreamCancellation(connection.NewRPCHandler(rpcServer))
	handler := i.coreHandler(rpcHandler)
	i.server = connection.NewHTTPServer(lifetime, handler)
	i.serveDone = make(chan error, 1)
	go func() { i.serveDone <- i.server.Serve(listener) }()
	command := exec.Command(cfg.Manifest.Executable, append(append([]string(nil), cfg.Args...), "--side="+cfg.Manifest.Side)...)
	// The child inherits the same flock description. A crashed host therefore
	// cannot release the writer lease before its old worker actually exits.
	command.ExtraFiles = []*os.File{lock}
	// Filtered environment: credentials are delivered only through Init.
	command.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}, cfg.Env...)
	// In go-plugin v1.8, TempDir alone only affects custom RunnerFunc paths.
	// The default command runner needs the documented child socket directory
	// environment variable as well; never let it fall back to shared /tmp.
	command.Env = append(command.Env, goplugin.EnvUnixSocketDir+"="+dir)
	checksum, _ := hex.DecodeString(cfg.Manifest.SHA256)
	i.process = goplugin.NewClient(&goplugin.ClientConfig{HandshakeConfig: sdk.Handshake, Plugins: goplugin.PluginSet{sdk.DispenseName: sdk.ClientPlugin()}, Cmd: command,
		SecureConfig: &goplugin.SecureConfig{Checksum: checksum, Hash: sha256.New()}, AutoMTLS: true, AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		UnixSocketConfig: &goplugin.UnixSocketConfig{TempDir: dir}, StartTimeout: cfg.StartupTimeout, SkipHostEnv: true,
		Logger: hclog.NewNullLogger(), Stderr: io.Discard, SyncStdout: io.Discard, SyncStderr: io.Discard,
		GRPCDialOptions: []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(connection.MaxMessageBytes), grpc.MaxCallSendMsgSize(connection.MaxMessageBytes))},
	})
	// Client() has a go-plugin handshake timeout. The cleanup owner kills its
	// process and joins this goroutine even if the startup caller has timed out.
	type started struct {
		client goplugin.ClientProtocol
		err    error
	}
	startDone := make(chan started, 1)
	i.clientDone = make(chan struct{})
	go func() {
		defer close(i.clientDone)
		client, e := i.process.Client()
		startDone <- started{client, e}
	}()
	var control goplugin.ClientProtocol
	select {
	case result := <-startDone:
		control, err = result.client, result.err
	case <-startup.Done():
		return nil, startup.Err()
	}
	if err != nil {
		return nil, errors.New("plugin handshake failed")
	}
	address := i.process.ReattachConfig()
	if address == nil || address.Addr == nil || address.Addr.Network() != "unix" {
		return nil, errors.New("plugin must use a private Unix socket")
	}
	if rel, e := filepath.Rel(dir, address.Addr.String()); e != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == "../" {
		return nil, errors.New("plugin socket escaped its instance directory")
	}
	if err = os.Chmod(address.Addr.String(), 0600); err != nil {
		return nil, err
	}
	i.snapshot.PID = address.Pid
	raw, err := control.Dispense(sdk.DispenseName)
	if err != nil {
		return nil, errors.New("plugin dispense failed")
	}
	client, ok := raw.(sdk.Lifecycle)
	if !ok {
		return nil, errors.New("plugin returned an invalid lifecycle adapter")
	}
	i.lifecycle = client
	info, err := client.GetInfo(startup)
	if err != nil {
		return nil, err
	}
	if info.Name != cfg.Manifest.Name || info.Version != cfg.Manifest.Version || (info.Side != "both" && info.Side != cfg.Manifest.Side) {
		return nil, fmt.Errorf("%w: %w", ErrIncompatible, connect.NewError(connect.CodeFailedPrecondition, "plugin manifest mismatch"))
	}
	agreement, err := version.Negotiate(cfg.Manifest.Protocol, info.Protocol, cfg.Manifest.Version)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrIncompatible, version.WithComponent(err, "plugin"))
	}
	req := &ppb.InitRequest{Config: cfg.Values, CoreApi: &ppb.CoreAPIEndpoint{Address: filepath.Join(dir, "core.sock"), Token: token}, Side: cfg.Manifest.Side, Protocol: agreement, PluginInstanceId: id, HostInstanceId: cfg.HostInstanceID, StateDir: cfg.StateDir}
	if err = client.Init(startup, req); err != nil {
		return nil, err
	}
	if err = client.Start(startup); err != nil {
		return nil, err
	}
	i.snapshot.State = "ready"
	i.snapshot.Protocol = proto.Clone(agreement).(*pb.ProtocolAgreement)
	go func() {
		select {
		case <-lifetime.Done():
		case <-client.Done():
		}
		_ = i.Stop(context.Background())
	}()
	return i, nil
}
func (i *Instance) Snapshot() Snapshot {
	i.mu.Lock()
	defer i.mu.Unlock()
	result := i.snapshot
	if result.Protocol != nil {
		result.Protocol = proto.Clone(result.Protocol).(*pb.ProtocolAgreement)
	}
	return result
}
func (i *Instance) Done() <-chan struct{} { return i.done }
func (i *Instance) Health(ctx context.Context) (*pb.HealthStatus, error) {
	i.mu.Lock()
	ready := i.snapshot.State == "ready"
	i.mu.Unlock()
	if !ready {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "plugin is not ready")
	}
	return i.lifecycle.Health(ctx)
}
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	stopped := i.snapshot.State == "stopped"
	i.mu.Unlock()
	if stopped {
		return nil
	}
	i.beginStop(true)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.done:
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.stopErr
	}
}

func (i *Instance) beginStop(graceful bool) {
	i.stopOnce.Do(func() {
		i.closeAdmission()
		i.mu.Lock()
		i.snapshot.State = "stopping"
		i.mu.Unlock()
		i.grant.revoke()
		// The instance owns cleanup after a caller's deadline. Other Stop
		// callers wait independently; none block behind a cleanup mutex.
		go func() {
			var err error
			if graceful && i.lifecycle != nil {
				stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = i.lifecycle.Stop(stop)
				cancel()
			}
			i.mu.Lock()
			i.stopErr = err
			i.mu.Unlock()
			i.close()
		}()
	})
}

func (i *Instance) closeAdmission() {
	i.handlerMu.Lock()
	i.admissionClosed = true
	i.handlerMu.Unlock()
}

func (i *Instance) coreHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.handlerMu.Lock()
		if i.admissionClosed {
			i.handlerMu.Unlock()
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "PLUGIN_STOPPING", http.StatusServiceUnavailable)
			return
		}
		i.handlers.Add(1)
		i.handlerMu.Unlock()
		defer i.handlers.Done()
		stop := context.AfterFunc(i.grant.lifetime, func() { _ = r.Body.Close() })
		defer stop()
		next.ServeHTTP(w, r)
	})
}
func (i *Instance) close() {
	i.cleanup.Do(func() {
		i.closeAdmission()
		i.grant.revoke()
		if i.process != nil {
			i.process.Kill()
		} // go-plugin Kill waits for process and reader goroutines.
		if i.clientDone != nil {
			<-i.clientDone
			// Kill before Client entered Start can observe no runner. Once
			// Client has joined, a final Kill also reaps any late-started child.
			i.process.Kill()
		}
		if i.server != nil {
			_ = i.server.Close()
			<-i.serveDone
		}
		// net/http Close and Serve only join listeners/connections. A canceled
		// CoreAPI handler can still own a proxy pump or deferred cleanup. Keep
		// the writer lease and stopping state until every admitted handler exits.
		i.handlers.Wait()
		_ = os.RemoveAll(i.dir)
		if i.stateLock != nil {
			_ = i.stateLock.Close()
		}
		i.mu.Lock()
		i.snapshot.State = "stopped"
		i.mu.Unlock()
		close(i.done)
	})
}

func lockStateDirectory(dir string) (*os.File, error) {
	fd, err := unix.Open(filepath.Join(dir, "instance.lock"), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "plugin instance lock")
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		file.Close()
		return nil, errors.New("plugin instance lock has unsafe ownership or permissions")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("plugin state is already owned by another instance")
	}
	return file, nil
}
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func privateDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("plugin directories must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("plugin directory must be owned by this user with mode 0700")
	}
	return nil
}
func validateConfig(c Config) error {
	m := c.Manifest
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(m.Name) || m.Version == "" || m.Protocol == nil {
		return errors.New("invalid plugin manifest")
	}
	if (m.Side != "client" && m.Side != "server") || c.Policy.Side != m.Side {
		return errors.New("invalid plugin side")
	}
	if c.HostInstanceID == "" || c.Core == nil {
		return errors.New("host instance and CoreAPI are required")
	}
	sum, err := hex.DecodeString(m.SHA256)
	if err != nil || len(sum) != sha256.Size {
		return errors.New("plugin requires a SHA-256 checksum")
	}
	if !filepath.IsAbs(m.Executable) {
		return errors.New("plugin executable must be absolute")
	}
	info, err := os.Lstat(m.Executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return errors.New("plugin must be a non-writable regular executable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Getuid())) {
		return errors.New("plugin executable has an untrusted owner")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(m.Executable))
	if err != nil {
		return err
	}
	for {
		info, err = os.Stat(parent)
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("plugin parent directory is writable by other users: %s", parent)
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return nil
}
