package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

// Run holds the instance lock until the listener, all RPCs, pools, node and
// resource store have closed. Handshake/GetStatus are the readiness API.
func Run(parent context.Context, configPath, build string) error {
	return RunWithRecovery(parent, configPath, build, false)
}

// RunWithRecovery resets a valid crash-loop record only after obtaining the
// instance lock. Recovery must be an explicit operator request, never automatic.
func RunWithRecovery(parent context.Context, configPath, build string, recoverLoop bool) error {
	cfg, err := config.LoadClient(configPath)
	if errors.Is(err, os.ErrNotExist) {
		cfg = config.DefaultClient()
		if err = config.SaveClient(configPath, cfg); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return runConfigOptions(parent, cfg, build, recoverLoop, RuntimeOptions{configPath: configPath})
}

// RuntimeOptions configures explicit runtime dependencies. Zero values retain
// the production defaults; these options are not persisted or read from env.
type RuntimeOptions struct {
	configPath               string // Populated only by the file-backed production entrypoint.
	PluginCredentialLifetime time.Duration
}

func RunConfig(parent context.Context, cfg *config.Client, build string) error {
	return RunConfigWithOptions(parent, cfg, build, RuntimeOptions{})
}

func RunConfigWithOptions(parent context.Context, cfg *config.Client, build string, options RuntimeOptions) error {
	return runConfigOptions(parent, cfg, build, false, options)
}

func runConfig(parent context.Context, cfg *config.Client, build string, recoverLoop bool) error {
	return runConfigOptions(parent, cfg, build, recoverLoop, RuntimeOptions{})
}

func runConfigOptions(parent context.Context, cfg *config.Client, build string, recoverLoop bool, options RuntimeOptions) error {
	if options.PluginCredentialLifetime < 0 {
		return errors.New("plugin credential lifetime cannot be negative")
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return err
	}
	lock, err := AcquireInstance(cfg.Agent.StateDir, cfg.Agent.Socket)
	if err != nil {
		return err
	}
	defer lock.Close()
	guard, diagnosticReason := beginCrashGuard(cfg.Agent.StateDir, build, recoverLoop, time.Now())
	err = runLocked(parent, cfg, build, diagnosticReason, options)
	// An abnormal process death or panic never reaches this completion record.
	if guard != nil {
		err = errors.Join(err, guard.finish(err, time.Now()))
	}
	return err
}

func runLocked(parent context.Context, cfg *config.Client, build, diagnosticReason string, options RuntimeOptions) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	service := NewService(ctx, uint32(os.Getuid()), build, nil)
	service.startup = newStartupObserver(cfg.Agent.AutoStart, options.configPath, uint32(os.Getuid()))
	service.setState("starting", "")
	// Shutdown signals the runner after its response has been written. The runner
	// closes admission before canceling active calls and persistent owners.
	stop := make(chan struct{}, 1)
	service.shutdown = func() {
		select {
		case stop <- struct{}{}:
		default:
		}
	}
	var embedded *node.Node
	var nodeFailure bool
	var control agentrpc.NodeControlHandler
	if diagnosticReason != "" {
		control = &diagnosticNode{instance: newID("node_"), reason: diagnosticReason, build: build}
	} else if cfg.Tailscale.Enabled {
		var err error
		embedded, err = node.Open(ctx, node.Options{StateDir: cfg.Tailscale.StateDir, Hostname: cfg.Tailscale.Hostname, ControlURL: cfg.Tailscale.ControlURL})
		if err == nil {
			defer embedded.Close()
			control = node.NewControl(embedded, uint32(os.Getuid()), build)
		} else {
			nodeFailure = true
			control = &diagnosticNode{instance: newID("node_"), reason: "NODE_STATE_UNAVAILABLE", build: build}
		}
	} else {
		control = &diagnosticNode{instance: newID("node_"), reason: "NODE_DISABLED", build: build}
	}
	rpcServer := service.RPCServer()
	agentrpc.RegisterNodeControlHandler(rpcServer, control)
	listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if err != nil {
		return err
	}
	server := connection.NewHTTPServer(ctx, service.Handler(rpcServer))
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	// NodeControl is served before resource import or remote handshakes.
	var resources *Resources
	if diagnosticReason != "" {
		service.setState("degraded", diagnosticReason)
	} else if nodeFailure {
		service.setState("degraded", "NODE_STATE_UNAVAILABLE")
	} else {
		resources, err = OpenResources(ctx, service, cfg, embedded)
		if err != nil {
			service.setState("degraded", "AGENT_STATE_UNAVAILABLE")
		} else {
			if err := resources.openPlugins(cfg, build, options); err != nil {
				service.setState("degraded", "PLUGIN_STATE_UNAVAILABLE")
			} else {
				service.setState("ready", "")
			}
			resources.notify()
		}
	}
	err = nil // Diagnostic startup failures remain queryable; orderly shutdown exits 0.
	select {
	case <-parent.Done():
	case <-stop:
	case err = <-served:
		served = nil
	}
	service.setState("stopping", "")
	service.handlerMu.Lock()
	service.admissionClosed = true
	service.handlerMu.Unlock()
	_ = listener.Close()
	// The Shutdown RPC has written its response, but net/http still needs to
	// finish the HTTP/2 stream after our handler returns. A hard Close here
	// races that END_STREAM and can turn accepted shutdown into unexpected EOF.
	// Gracefully drain first; ongoing event/terminal streams get a bounded
	// window before cancellation and forced close release all resources.
	drain, stopDrain := context.WithTimeout(context.Background(), time.Second)
	_ = server.Shutdown(drain)
	stopDrain()
	cancel()
	// Close cancels HTTP/2 and wakes streaming request bodies; tmux lives remotely.
	_ = server.Close()
	if served != nil {
		<-served
	}
	service.handlers.Wait()
	if resources != nil {
		_ = resources.Close()
	}
	if embedded != nil {
		_ = embedded.Close()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// A disabled or corrupt node still exposes independent diagnostic negotiation.
// The enclosing IPC server has already verified peer UID and rejected auth headers.
type diagnosticNode struct {
	agentrpc.UnimplementedNodeControlHandler
	instance, reason, build string
}

func (d *diagnosticNode) Negotiate(_ context.Context, offer *pb.ProtocolOffer) (*agentpb.NodeNegotiation, error) {
	// This diagnostic endpoint has no node instance that can perform login.
	// Keep status negotiation available without promising the login capability.
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1}, offer, d.build)
	if err != nil {
		return nil, err
	}
	return &agentpb.NodeNegotiation{Protocol: agreement, InstanceId: d.instance}, nil
}
func (d *diagnosticNode) authorize(ctx context.Context) error {
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok || len(info.RequestHeader().Values(node.NodeInstanceHeader)) != 1 || info.RequestHeader().Get(node.NodeInstanceHeader) != d.instance {
		return rpcError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
	}
	return nil
}
func (d *diagnosticNode) GetStatus(ctx context.Context, _ *pb.Empty) (*agentpb.NodeStatus, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return &agentpb.NodeStatus{State: "offline", ReasonCode: d.reason}, nil
}
func (d *diagnosticNode) BeginLogin(ctx context.Context, _ *agentpb.BeginLoginRequest) (*agentpb.NodeStatus, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return nil, rpcError(connect.CodeFailedPrecondition, d.reason)
}
func (d *diagnosticNode) CancelLogin(ctx context.Context, _ *agentpb.CancelLoginRequest) (*agentpb.NodeStatus, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return nil, rpcError(connect.CodeFailedPrecondition, d.reason)
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shiki", "config.yaml")
}
