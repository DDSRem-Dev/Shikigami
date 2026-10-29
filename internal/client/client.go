// Package client is the frontend's IPC client. It never creates a tsnet node,
// connects to a daemon directly, or edits background resource state.
package client

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

const DefaultConfig = "~/.shiki/config.yaml"
const instanceHeader = "shiki-agent-instance-id"
const frontendHeader = "shiki-frontend-id"
const serverHeader = "shiki-server-id"

type Options struct {
	RecoverCrashLoop bool
	ExplicitStart    bool
	AutoStart        bool
	Timeout          time.Duration
	AgentBinary      string
}
type Client struct {
	endpoint           *connection.Endpoint
	instance, frontend string
	agent              arpc.AgentClient
	lifetime           context.Context
	cancel             context.CancelFunc
	once               sync.Once
	timeout            time.Duration
	capabilities       []string
	ConfigPath         string
	Config             *config.Client
}

func ConfigPath(path string) (string, error) {
	if path == "" {
		path = DefaultConfig
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if strings.HasPrefix(path, "~") {
		return "", errors.New("only ~/ path expansion is supported")
	}
	return filepath.Abs(path)
}
func LoadConfig(path string) (string, *config.Client, error) {
	path, e := ConfigPath(path)
	if e != nil {
		return "", nil, e
	}
	cfg, e := config.LoadClient(path)
	if errors.Is(e, os.ErrNotExist) {
		cfg = config.DefaultClient()
		e = cfg.NormalizeAndValidate()
	}
	return path, cfg, e
}
func Open(ctx context.Context, path string, opts Options) (*Client, error) {
	if opts.RecoverCrashLoop && (!opts.AutoStart || !opts.ExplicitStart) {
		return nil, errors.New("crash-loop recovery requires an explicit agent start")
	}
	path, cfg, e := LoadConfig(path)
	if e != nil {
		return nil, e
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	c, e := dial(ctx, path, cfg, opts.Timeout)
	if e == nil && opts.RecoverCrashLoop {
		status, statusErr := c.Agent().GetStatus(ctx, &pb.Empty{})
		if statusErr != nil {
			c.Close()
			return nil, statusErr
		}
		if status.ReasonCode != "AGENT_CRASH_LOOP" {
			c.Close()
			return nil, errors.New("agent is not in a recoverable crash loop; inspect agent status and logs")
		}
		_, e = c.Agent().Shutdown(ctx, &apb.ShutdownRequest{ExpectedInstanceId: c.InstanceID(), RequestId: rand.Text()})
		if e == nil {
			wait, stop := context.WithTimeout(ctx, 10*time.Second)
			e = c.WaitStopped(wait)
			stop()
		}
		c.Close()
		if e != nil {
			return nil, e
		}
		c, e = startAndConnect(ctx, path, cfg, opts)
		return startupResult(ctx, c, e, opts)
	}
	if e == nil {
		return startupResult(ctx, c, nil, opts)
	}
	if !opts.AutoStart || (!cfg.Agent.AutoStart && !opts.ExplicitStart) || !agentUnavailable(e) {
		return nil, e
	}
	c, e = startAndConnect(ctx, path, cfg, opts)
	return startupResult(ctx, c, e, opts)
}

func startupResult(ctx context.Context, c *Client, err error, opts Options) (*Client, error) {
	if err != nil || !opts.AutoStart {
		return c, err
	}
	status, err := c.Agent().GetStatus(ctx, &pb.Empty{})
	if err == nil && strings.HasPrefix(status.ReasonCode, "AGENT_CRASH_") {
		guidance := "inspect agent status and logs; preserve and repair the startup record, then stop and start the agent"
		switch status.ReasonCode {
		case "AGENT_CRASH_LOOP":
			guidance = "inspect agent status and logs; after correcting the cause, use shiki agent start --recover-crash-loop"
		case "AGENT_CRASH_CLOCK_ROLLBACK":
			guidance = "correct the system clock to at least the recorded startup time, then stop and start the agent"
		}
		err = connect.NewError(connect.CodeFailedPrecondition, status.ReasonCode+": "+guidance)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func dial(ctx context.Context, path string, cfg *config.Client, timeout time.Duration) (*Client, error) {
	endpoint, e := connection.NewUnixClient(cfg.Agent.Socket, uint32(os.Getuid()), "shiki-agent")
	if e != nil {
		return nil, e
	}
	lifetime, cancel := context.WithCancel(context.Background())
	c := &Client{endpoint: endpoint, lifetime: lifetime, cancel: cancel, timeout: timeout, ConfigPath: path, Config: cfg}
	raw := arpc.NewAgentClient(connection.NewRPCClient(endpoint))
	probe, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	probe, _ = connect.NewClientContext(probe)
	device, _ := os.Hostname()
	hello, e := raw.Handshake(probe, &apb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.control.v1", "agent.events.v1", "agent.startup.v1"}, RequiredCapabilities: []string{"agent.control.v1"}}, ClientBuild: buildinfo.Version, DeviceName: device})
	if e != nil {
		cancel()
		endpoint.Close()
		return nil, version.WithComponent(e, "agent")
	}
	if hello.InstanceId == "" || hello.FrontendId == "" || hello.Protocol == nil || hello.Protocol.Major != 1 || hello.Protocol.Minor != 0 || !slices.Contains(hello.Protocol.Capabilities, "agent.control.v1") {
		cancel()
		endpoint.Close()
		return nil, errors.New("agent returned an invalid handshake")
	}
	c.instance, c.frontend = hello.InstanceId, hello.FrontendId
	c.capabilities = slices.Clone(hello.Protocol.Capabilities)
	c.agent = arpc.NewAgentClient(connection.NewRPCClient(endpoint, c.identity("")))
	status, e := c.agent.GetStatus(probe, &pb.Empty{})
	if e != nil {
		c.Close()
		return nil, e
	}
	if status.InstanceId != c.instance {
		c.Close()
		return nil, errors.New("agent changed instance during handshake")
	}
	switch status.State {
	case "ready", "degraded":
	case "starting", "stopping":
		c.Close()
		return nil, connect.NewError(connect.CodeUnavailable, "agent is not ready")
	default:
		c.Close()
		return nil, connect.NewError(connect.CodeDataLoss, "agent returned an invalid readiness state")
	}
	return c, nil
}
func (c *Client) StartupPolicy(ctx context.Context) (*apb.StartupPolicy, error) {
	if !slices.Contains(c.capabilities, "agent.startup.v1") {
		return nil, connect.NewError(connect.CodeUnimplemented, "agent does not support startup policy reporting")
	}
	return c.agent.GetStartupPolicy(ctx, &pb.Empty{})
}
func (c *Client) Agent() arpc.AgentClient { return c.agent }
func (c *Client) Remote(serverID string) rpc.ShikiDaemonClient {
	return rpc.NewShikiDaemonClient(connection.NewRPCClient(c.endpoint, c.identity(serverID)))
}
func (c *Client) InstanceID() string { return c.instance }
func (c *Client) FrontendID() string { return c.frontend }
func (c *Client) Close() error {
	var result error
	c.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if c.agent != nil {
			_, result = c.agent.ReleaseFrontend(ctx, &pb.Empty{})
		}
		c.cancel()
		c.endpoint.Close()
	})
	return result
}
func (c *Client) identity(server string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			var cancel context.CancelFunc
			if spec.StreamType == connect.StreamTypeUnary {
				timeout := c.timeout
				if timeout == 5*time.Second && lifecycleProcedure(spec.Procedure) {
					timeout = 15 * time.Second
				}
				ctx, cancel = context.WithTimeout(ctx, timeout)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			stop := context.AfterFunc(c.lifetime, cancel)
			ctx, info := connect.NewClientContext(ctx) // Never inherit caller headers or CallInfo.
			info.RequestHeader().Set(instanceHeader, c.instance)
			info.RequestHeader().Set(frontendHeader, c.frontend)
			if server != "" {
				info.RequestHeader().Set(serverHeader, server)
			}
			stream, e := next(ctx, spec)
			if e != nil {
				stop()
				cancel()
				return nil, e
			}
			return &ownedStream{ClientStream: stream, release: func() { stop(); cancel() }}, nil
		}
	}
}

type ownedStream struct {
	connect.ClientStream
	once    sync.Once
	release func()
}

func (s *ownedStream) Close() error { e := s.ClientStream.Close(); s.once.Do(s.release); return e }
func (s *ownedStream) Receive(message any) error {
	e := s.ClientStream.Receive(message)
	if e != nil {
		s.once.Do(s.release)
	}
	return e
}
func agentUnavailable(e error) bool {
	return errors.Is(e, os.ErrNotExist) || connection.IsRetryableNetworkError(e) || connect.CodeOf(e) == connect.CodeUnavailable
}

func lifecycleProcedure(procedure string) bool {
	switch procedure {
	case rpc.ShikiDaemonCreateSessionProcedure, rpc.ShikiDaemonStopClaudeProcedure, rpc.ShikiDaemonRestartClaudeProcedure, rpc.ShikiDaemonDeleteSessionProcedure, rpc.ShikiDaemonRestoreSnapshotProcedure,
		arpc.AgentShutdownProcedure, arpc.AgentManagePluginProcedure, arpc.AgentPutMappingProcedure, arpc.AgentRemoveMappingProcedure, arpc.AgentCreateForwardProcedure, arpc.AgentStopForwardProcedure:
		return true
	default:
		return false
	}
}
