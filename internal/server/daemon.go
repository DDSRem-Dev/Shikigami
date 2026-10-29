package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server/monitor"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

const ShutdownTimeout = 10 * time.Second

type DaemonOptions struct {
	Build string
	// Zero retains the production plugin credential lifetime. This explicit
	// runtime dependency is not a configuration-file or environment setting.
	PluginCredentialLifetime time.Duration
}
type DaemonInfo struct {
	DaemonID           string   `json:"daemon_id"`
	AdminSocket        string   `json:"admin_socket"`
	DirectTLSAddress   string   `json:"direct_tls_address,omitempty"`
	TailscaleState     string   `json:"tailscale_state"`
	TailscaleAddresses []string `json:"tailscale_addresses,omitempty"`
	RemoteReady        bool     `json:"remote_ready"`
}

// Runtime owns listeners, node identity, session bridges and resource state.
// The tmux server is independent and deliberately survives Runtime.Close.
type Runtime struct {
	Service    *Service
	Ready      <-chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        config.Daemon
	state      *storage.Store
	pidClaimed bool
	node       *node.Node
	mu         sync.Mutex
	servers    []*http.Server
	info       DaemonInfo
	failure    error
	closeErr   error
	ready      chan struct{}
	readyOnce  sync.Once
	closeOnce  sync.Once
	closed     chan struct{}
	wg         sync.WaitGroup
}

func StartDaemon(parent context.Context, cfg *config.Daemon, options DaemonOptions) (runtime *Runtime, err error) {
	if options.PluginCredentialLifetime < 0 {
		return nil, errors.New("plugin credential lifetime cannot be negative")
	}
	if cfg == nil {
		return nil, errors.New("daemon configuration is required")
	}
	copy := *cfg
	copy.Claude.Args = append([]string(nil), cfg.Claude.Args...)
	if err = copy.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	if err = copy.CheckRunUser(); err != nil {
		return nil, err
	}
	if err = parent.Err(); err != nil {
		return nil, err
	}
	if copy.Server.DirectTLS.Enabled {
		if _, err = connection.LoadTLSCertificate(copy.Server.DirectTLS.CertFile, copy.Server.DirectTLS.KeyFile); err != nil {
			return nil, fmt.Errorf("load direct TLS certificate: %w", err)
		}
	}
	state, err := storage.Open(copy.StateDir, storage.Options{Validate: storage.ValidateDaemon(copy.RunUser)})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	r := &Runtime{ctx: ctx, cancel: cancel, cfg: copy, state: state, ready: make(chan struct{}), closed: make(chan struct{}), Service: &Service{Build: options.Build, StartedAt: time.Now(), ClaudeExecutable: copy.Claude.Executable}}
	r.Ready = r.ready
	defer func() {
		if err != nil {
			err = errors.Join(err, r.Close())
		}
	}()
	socket := filepath.Join(copy.StateDir, "run", "admin.sock")
	if err = claimDaemonSocket(copy.StateDir, socket); err != nil {
		return nil, err
	}
	r.pidClaimed = true
	// PluginsDir contains installed executables, possibly a managed link into
	// a root-owned release. Manifest/executable verification owns artifact trust;
	// mutable plugin state and sockets use separate private state directories.
	r.Service.Auth, err = auth.Open(filepath.Join(copy.StateDir, "auth"), copy.RunUser)
	if err != nil {
		return nil, err
	}
	r.info.DaemonID, err = r.Service.Auth.DaemonID()
	if err != nil {
		return nil, err
	}
	r.info.AdminSocket = socket
	r.info.TailscaleState = "disabled"
	r.Service.Projects, err = project.New(state, copy.ProjectsDir, copy.RunUser)
	if err != nil {
		return nil, err
	}
	mux, err := tmux.New("")
	if err != nil {
		return nil, err
	}
	command := copy.Claude.Executable
	if resolved, resolveErr := exec.LookPath(command); resolveErr == nil {
		command = resolved
	} else if !filepath.IsAbs(command) {
		// Retain a missing executable as a dependency failure for autostart;
		// shell-only sessions and diagnostics must still be available.
		command = filepath.Join(copy.StateDir, "missing-dependencies", command)
	}
	r.Service.Sessions, err = session.NewManager(ctx, state, mux, session.ManagerOptions{User: copy.RunUser, ClaudeCommand: append([]string{command}, copy.Claude.Args...), MaxSessions: copy.ResourceLimits.MaxSessions, MaxClaude: copy.ResourceLimits.MaxConcurrentClaude, AutoCleanup: session.CleanupPolicy{Enabled: copy.AutoCleanup.Enabled, IdleThreshold: time.Duration(copy.AutoCleanup.IdleThreshold), WarnBefore: time.Duration(copy.AutoCleanup.WarnBeforeCleanup)}, AutoRestart: session.RestartPolicy{Enabled: copy.Claude.AutoRestart.Enabled, MaxAttempts: copy.Claude.AutoRestart.MaxAttempts, Backoff: time.Duration(copy.Claude.AutoRestart.Backoff), StableWindow: time.Duration(copy.Claude.AutoRestart.StableWindow)}})
	if err != nil {
		return nil, err
	}
	recoverCtx, stopRecover := context.WithTimeout(ctx, 5*time.Second)
	_, err = r.Service.Sessions.Recover(recoverCtx)
	stopRecover()
	if err != nil {
		return nil, err
	}
	r.Service.Monitor = monitor.New(r.Service.Sessions, copy.StateDir)
	if err = r.startPlugins(options); err != nil {
		return nil, err
	}
	r.Service.Health = r.health
	adminRPC := connect.NewServer()
	arpc.RegisterDaemonReadinessHandler(adminRPC, newDaemonReadiness(r))
	if copy.Tailscale.Enabled {
		r.info.TailscaleState = "starting"
		r.node, err = node.Open(ctx, node.Options{StateDir: copy.Tailscale.StateDir, Hostname: copy.Tailscale.Hostname, ControlURL: copy.Tailscale.ControlURL})
		if err != nil {
			return nil, err
		}
		arpc.RegisterNodeControlHandler(adminRPC, node.NewControl(r.node, uint32(os.Geteuid()), options.Build))
	} else {
		arpc.RegisterNodeControlHandler(adminRPC, newDisabledNodeControl(options.Build))
	}
	admin, err := connection.ListenUnix(socket, uint32(os.Geteuid()))
	if err != nil {
		return nil, err
	}
	r.serve(connection.NewHTTPServer(ctx, connection.NewRPCHandler(adminRPC)), admin, false)
	if copy.Server.DirectTLS.Enabled {
		listener, listenErr := net.Listen("tcp", copy.Server.DirectTLS.Address)
		if listenErr != nil {
			return nil, listenErr
		}
		r.info.DirectTLSAddress = listener.Addr().String()
		r.serve(connection.NewHTTPServer(ctx, r.Service.Handler()), listener, true)
		r.markReady()
	}
	// NodeControl is already listening. Authentication cannot hold up local
	// administration or an explicitly configured TLS listener.
	if r.node != nil {
		r.wg.Add(1)
		go r.serveNode()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	context.AfterFunc(ctx, func() { _ = r.Close() })
	return r, nil
}

func (r *Runtime) serve(server *http.Server, listener net.Listener, tlsEnabled bool) {
	r.mu.Lock()
	r.servers = append(r.servers, server)
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		var err error
		if tlsEnabled {
			err = connection.ServeTLS(server, listener, r.cfg.Server.DirectTLS.CertFile, r.cfg.Server.DirectTLS.KeyFile)
		} else {
			err = server.Serve(listener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) && r.ctx.Err() == nil {
			r.mu.Lock()
			r.failure = errors.Join(r.failure, err)
			r.mu.Unlock()
			r.cancel()
		}
	}()
}

func (r *Runtime) serveNode() {
	defer r.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var server *http.Server
	var serverDone chan error
	var cancel context.CancelFunc
	stop := func() {
		if cancel != nil {
			cancel()
			cancel = nil
		}
		if server != nil {
			_ = server.Close()
			server = nil
		}
		serverDone = nil
	}
	defer stop()
	for {
		status := r.node.Status()
		if status.State == "online" && server == nil {
			listener, err := r.node.Listen(uint16(r.cfg.Server.Port))
			if err == nil {
				server, cancel = nodeHTTPServer(r.ctx, r.Service.Handler())
				// The node loop owns this server. It closes it when node
				// authorization ends, including already-established long streams.
				serverDone = make(chan error, 1)
				r.wg.Add(1)
				go func(active *http.Server, done chan error) { defer r.wg.Done(); done <- active.Serve(listener) }(server, serverDone)
				r.markReady()
			}
		} else if status.State != "online" {
			stop()
		}
		r.mu.Lock()
		r.info.TailscaleState = status.State
		r.info.TailscaleAddresses = append([]string(nil), status.Addresses...)
		r.info.RemoteReady = r.info.DirectTLSAddress != "" || server != nil
		r.mu.Unlock()
		select {
		case <-r.ctx.Done():
			return
		case <-serverDone:
			stop()
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
			}
		case <-ticker.C:
		}
	}
}

func nodeHTTPServer(parent context.Context, handler http.Handler) (*http.Server, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	return connection.NewHTTPServer(ctx, handler), cancel
}

func (r *Runtime) markReady() {
	r.mu.Lock()
	r.info.RemoteReady = true
	r.mu.Unlock()
	r.readyOnce.Do(func() { close(r.ready) })
}
func (r *Runtime) Info() DaemonInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.info
	copy.TailscaleAddresses = append([]string(nil), r.info.TailscaleAddresses...)
	return copy
}
func (r *Runtime) Done() <-chan struct{} { return r.closed }
func (r *Runtime) Wait() error {
	<-r.ctx.Done()
	err := r.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.failure, err)
}

func (r *Runtime) Close() error {
	r.closeOnce.Do(func() { r.cancel(); go r.cleanup() })
	timer := time.NewTimer(ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-r.closed:
		return r.closeErr
	case <-timer.C:
		return errors.New("daemon shutdown exceeded 10 seconds; resources remain locked until cleanup finishes")
	}
}
func (r *Runtime) cleanup() {
	defer close(r.closed)
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	r.mu.Lock()
	servers := append([]*http.Server(nil), r.servers...)
	r.info.RemoteReady = false
	r.mu.Unlock()
	var wg sync.WaitGroup
	var errMu sync.Mutex
	join := func(err error) {
		if err != nil {
			errMu.Lock()
			r.closeErr = errors.Join(r.closeErr, err)
			errMu.Unlock()
		}
	}
	for _, server := range servers {
		wg.Go(func() {
			err := server.Shutdown(ctx)
			if err != nil {
				join(err)
				join(server.Close())
			}
		})
	}
	if r.Service.Supervisor != nil {
		join(r.Service.Supervisor.Close())
	}
	if r.Service.Sessions != nil {
		join(r.Service.Sessions.Close())
	}
	seen := map[*plugin.Instance]bool{}
	for _, worker := range []*plugin.Instance{r.Service.FileWorker, r.Service.PortWorker} {
		if worker != nil && !seen[worker] {
			seen[worker] = true
			join(worker.Stop(ctx))
		}
	}
	if r.node != nil {
		join(r.node.Close())
	}
	wg.Wait()
	r.wg.Wait()
	if r.Service.Projects != nil {
		join(r.Service.Projects.Close())
	}
	if r.Service.Auth != nil {
		join(r.Service.Auth.Close())
	}
	if r.pidClaimed {
		pid := filepath.Join(r.cfg.StateDir, "run", "daemon.pid")
		if err := os.Remove(pid); err != nil && !errors.Is(err, os.ErrNotExist) {
			join(err)
		}
	}
	if r.state != nil {
		join(r.state.Close())
	}
}

func (r *Runtime) health() *pb.HealthStatus {
	info := r.Info()
	result := &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY, Message: "core ready", Details: map[string]string{"tailscale": info.TailscaleState, "sync": "not_configured", "port": "not_configured"}}
	if r.Service.Supervisor != nil {
		if plugins, err := r.Service.Supervisor.List(); err != nil {
			result.Status = pb.HealthStatus_DEGRADED
			result.Details["plugins"] = "state_unavailable"
		} else {
			for _, worker := range plugins.Plugins {
				result.Details[worker.PluginId] = worker.State
				if worker.Enabled && worker.State != "ready" {
					result.Status = pb.HealthStatus_DEGRADED
				}
			}
		}
	}
	if r.ctx.Err() != nil {
		return &pb.HealthStatus{Status: pb.HealthStatus_UNHEALTHY, Message: "daemon stopping"}
	}
	if !info.RemoteReady {
		result.Status = pb.HealthStatus_DEGRADED
		result.Message = "local administration ready; remote transport waiting for login"
	}
	if _, err := exec.LookPath(r.cfg.Claude.Executable); err != nil {
		result.Status = pb.HealthStatus_DEGRADED
		result.Message = "core ready; Claude executable unavailable; shell-only sessions remain available"
		result.Details["claude"] = "unavailable"
	} else {
		result.Details["claude"] = "executable_found; login_not_checked"
	}
	return result
}
