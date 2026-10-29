package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

type loginSequence struct {
	arpc.NodeControlClient
	statuses      []*apb.NodeStatus
	starts, reads int
	keySeen       bool
}

func (s *loginSequence) BeginLogin(_ context.Context, request *apb.BeginLoginRequest) (*apb.NodeStatus, error) {
	s.starts++
	s.keySeen = len(request.GetAuthKey()) != 0
	return &apb.NodeStatus{State: "authenticating", LoginId: "login-test", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}, nil
}
func (s *loginSequence) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status := s.statuses[min(s.reads, len(s.statuses)-1)]
	s.reads++
	return status, nil
}
func TestInstallerWaitsForLoginWithoutBeginningExistingAttempt(t *testing.T) {
	for _, tc := range []struct {
		name         string
		initial      *apb.NodeStatus
		statuses     []*apb.NodeStatus
		key          []byte
		starts       int
		want, reason string
	}{
		{"delayed-url", &apb.NodeStatus{State: "needs_login"}, []*apb.NodeStatus{{State: "authenticating"}, {State: "needs_login", LoginId: "login-test", LoginExpiresAt: time.Now().Add(time.Minute).Unix(), LoginUrl: "https://private-login.test/attempt"}}, nil, 1, "needs_login", ""},
		{"existing-attempt", &apb.NodeStatus{State: "authenticating", LoginId: "existing", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}, []*apb.NodeStatus{{State: "online", Addresses: []string{"100.64.0.10"}}}, nil, 0, "online", ""},
		{"key-online", &apb.NodeStatus{State: "needs_login"}, []*apb.NodeStatus{{State: "online", Addresses: []string{"100.64.0.10"}}}, []byte("private-test-key"), 1, "online", ""},
		{"denied", &apb.NodeStatus{State: "needs_login"}, []*apb.NodeStatus{{State: "denied", ReasonCode: "INVALID_AUTH_KEY"}}, []byte("private-test-key"), 1, "denied", "INVALID_AUTH_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &loginSequence{statuses: tc.statuses}
			got, err := guideLogin(t.Context(), api, tc.initial, UserOperation{Interactive: true, Key: tc.key}, true)
			if err != nil || got.State != tc.want || got.ReasonCode != tc.reason || api.starts != tc.starts || api.keySeen != (len(tc.key) != 0) {
				t.Fatal(got, err, api.starts, api.keySeen)
			}
			if tc.name == "delayed-url" && got.LoginUrl == "" {
				t.Fatal("returned before asynchronous URL arrived")
			}
			if got.State == "online" && len(got.Addresses) != 1 {
				t.Fatal("lost actual embedded address")
			}
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := guideLogin(ctx, &loginSequence{statuses: []*apb.NodeStatus{{State: "authenticating"}}}, &apb.NodeStatus{State: "authenticating"}, UserOperation{}, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("login polling ignored caller cancellation", err)
	}
}

type installerNodeFixture struct {
	arpc.UnimplementedNodeControlHandler
	restart bool
	reads   atomic.Int32
	status  *apb.NodeStatus
}

func (n *installerNodeFixture) Negotiate(context.Context, *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	return &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 1, BuildVersion: buildinfo.Version}, InstanceId: "node-test"}, nil
}
func (n *installerNodeFixture) GetStatus(context.Context, *pb.Empty) (*apb.NodeStatus, error) {
	reads := n.reads.Add(1)
	if n.restart && reads > 1 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
	}
	if n.status != nil {
		return n.status, nil
	}
	return &apb.NodeStatus{State: "offline", ReasonCode: "NODE_DISABLED"}, nil
}

type installerAgentFixture struct {
	arpc.UnimplementedAgentHandler
	build string
}

func (a *installerAgentFixture) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	return &apb.HandshakeResponse{Protocol: &pb.ProtocolAgreement{Major: 1, BuildVersion: a.build, Capabilities: []string{"agent.control.v1"}}, InstanceId: "agent-test", FrontendId: "frontend-test"}, nil
}
func (a *installerAgentFixture) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{State: "ready", InstanceId: "agent-test"}, nil
}
func (a *installerAgentFixture) ListPlugins(context.Context, *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	return &pb.ListPluginsResponse{}, nil
}
func (a *installerAgentFixture) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}

func TestInstallerBindsBothAgentBuildAndFinalNodeInstance(t *testing.T) {
	for _, tc := range []struct {
		name, build               string
		restart, success, delayed bool
		nodeStatus                *apb.NodeStatus
		enabled, pending          bool
	}{
		{name: "current", build: buildinfo.Version, success: true},
		{name: "delayed-socket", build: buildinfo.Version, success: true, delayed: true},
		{name: "old-agent", build: "old-build"},
		{name: "node-restarted", build: buildinfo.Version, restart: true},
		{name: "agent-explicitly-disabled", build: buildinfo.Version, success: true, nodeStatus: &apb.NodeStatus{State: "offline", ReasonCode: "NODE_DISABLED"}},
		{name: "agent-disabled-but-config-enabled", build: buildinfo.Version, enabled: true, pending: true, nodeStatus: &apb.NodeStatus{State: "offline", ReasonCode: "NODE_DISABLED"}},
		{name: "agent-other-offline-reason", build: buildinfo.Version, pending: true, nodeStatus: &apb.NodeStatus{State: "offline", ReasonCode: "NODE_STATE_WRITE_FAILED"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "shiki-probe-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			cfg := config.DefaultClient()
			cfg.Agent.Socket = filepath.Join(root, "run", "agent.sock")
			cfg.Agent.StateDir = filepath.Join(root, "state")
			cfg.Tailscale.Enabled = tc.enabled
			path := filepath.Join(root, "client.yaml")
			if err = config.SaveClient(path, cfg); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			type probeResult struct {
				report *ProbeReport
				err    error
			}
			result := make(chan probeResult, 1)
			probe := func() {
				report, err := probeUser(ctx, UserOperation{Plan: Plan{Role: "client", Config: path, StateDir: cfg.Agent.StateDir}})
				result <- probeResult{report, err}
			}
			if tc.delayed {
				go probe()
				select {
				case got := <-result:
					t.Fatal("probe returned before runtime directory/socket creation", got.err)
				case <-time.After(150 * time.Millisecond):
				}
			}
			listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
			rpcServer := connect.NewServer()
			arpc.RegisterNodeControlHandler(rpcServer, &installerNodeFixture{restart: tc.restart, status: tc.nodeStatus})
			arpc.RegisterAgentHandler(rpcServer, &installerAgentFixture{build: tc.build})
			server := connection.NewHTTPServer(t.Context(), connection.NewRPCHandler(rpcServer))
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			defer func() { server.Close(); <-done }()
			if !tc.delayed {
				go probe()
			}
			got := <-result
			report, err := got.report, got.err
			if tc.success {
				if err != nil || report.State != "local_application_ready" {
					t.Fatal(report, err)
				}
			} else if tc.pending {
				if err != nil || report.State != "node_offline" {
					t.Fatal("offline node was incorrectly accepted", report, err)
				}
			} else if err == nil {
				t.Fatal("changed host was accepted", report)
			}
		})
	}
}

func TestInstallerMissingSocketWaitIsCancellableAndUnsafePathFailsClosed(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shiki-probe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Tailscale.Enabled = false
	path := filepath.Join(root, "client.yaml")
	if err = config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	request := UserOperation{Plan: Plan{Role: "client", Config: path, StateDir: cfg.Agent.StateDir}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err = probeUser(ctx, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing socket did not honor caller deadline", err)
	}
	if err = os.WriteFile(cfg.Agent.Socket, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err = probeUser(ctx, request); err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "IPC socket") {
		t.Fatal("unsafe socket path was retried or accepted", err)
	}
}

func TestPublicInstallResultRetainsReasonsAndAddressesButNoLoginMaterial(t *testing.T) {
	n := &Native{LoginURL: "https://private-login.test/secret", Report: &ProbeReport{Node: &apb.NodeStatus{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED", LoginUrl: "https://private-login.test/secret", Addresses: []string{"100.64.0.10"}}, State: "node_needs_login", Reason: "NODE_AUTH_REQUIRED"}}
	result := &Result{State: "node_needs_login"}
	n.Describe(result)
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "private-login") || result.NodeReason != "NODE_AUTH_REQUIRED" || len(result.NodeAddresses) != 1 || result.ExternalPeerAcceptance != "pending" {
		t.Fatal("invalid public bootstrap report", string(data))
	}
}
