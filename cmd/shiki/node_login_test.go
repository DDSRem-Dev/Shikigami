package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"google.golang.org/protobuf/proto"
)

// These CLI regressions use generated NodeControl RPCs over the real private
// Unix/HTTP2 transport. The status sequence models the SDK publishing NeedsLogin
// before the control plane has supplied an interactive URL or accepted a key.
func TestNodeLoginWaitsForPendingControlPlane(t *testing.T) {
	for _, authKey := range []bool{false, true} {
		name := "interactive_url"
		if authKey {
			name = "auth_key_online"
		}
		t.Run(name, func(t *testing.T) {
			pending := pendingLoginStatus()
			last := proto.Clone(pending).(*apb.NodeStatus)
			want := "https://login.example.invalid/test-only"
			if authKey {
				last.State, last.ReasonCode, last.LoginExpiresAt = "online", "", 0
				last.Addresses = []string{"100.64.0.9"}
				want = `"state": "online"`
			} else {
				last.LoginUrl = want
			}
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pending, last}, authKey: authKey}
			socket := nodeLoginSocket(t, probe)
			args := []string{"--wait-timeout", "2s"}
			if authKey {
				path := filepath.Join(t.TempDir(), "key")
				if err := os.WriteFile(path, []byte("unit-test-auth-key"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--auth-key-file", path)
			}
			out, stderr, err := runNodeLogin(t.Context(), socket, args...)
			var result struct {
				State    string `json:"state"`
				LoginURL string `json:"login_url"`
			}
			decodeErr := json.Unmarshal([]byte(out), &result)
			completed := result.LoginURL == want
			if authKey {
				completed = result.State == "online"
			}
			if err != nil || decodeErr != nil || !completed {
				t.Fatalf("pending login did not complete: %v, output=%s", err, out)
			}
			if stderr != "" || strings.Contains(out, "unit-test-auth-key") {
				t.Fatal("unexpected diagnostics or auth-key disclosure")
			}
			probe.checkCalls(t, 2)
		})
	}
}

func TestNodeLoginStopsForTerminalStatus(t *testing.T) {
	for _, status := range []*apb.NodeStatus{
		{State: "denied", ReasonCode: "NODE_APPROVAL_REQUIRED"},
		{State: "denied", ReasonCode: "INVALID_AUTH_KEY"},
		{State: "expired", ReasonCode: "NODE_KEY_EXPIRED"},
		{State: "needs_login", ReasonCode: "LOGIN_CANCELED"},
		{State: "needs_login", ReasonCode: "LOGIN_EXPIRED"},
		{State: "offline", ReasonCode: "LOGIN_START_FAILED"},
		{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED"},
		{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED", LoginId: "expired-attempt", LoginExpiresAt: 1},
	} {
		t.Run(status.ReasonCode+"/"+status.LoginId, func(t *testing.T) {
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{status}}
			socket := nodeLoginSocket(t, probe)
			_, _, err := runNodeLogin(t.Context(), socket, "--wait-timeout", "2s")
			if err == nil || !strings.Contains(err.Error(), "node login "+status.State+": "+status.ReasonCode) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("terminal result was lost or waited out: %v", err)
			}
			probe.checkCalls(t, 1)
		})
	}
}

func TestNodeLoginPendingWaitIsBounded(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		name := "wait_timeout"
		if parentDeadline {
			name = "earlier_parent_deadline"
		}
		t.Run(name, func(t *testing.T) {
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pendingLoginStatus()}}
			socket := nodeLoginSocket(t, probe)
			ctx := t.Context()
			wait := "350ms"
			if parentDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 350*time.Millisecond)
				defer cancel()
				wait = "2s"
			}
			_, _, err := runNodeLogin(ctx, socket, "--wait-timeout", wait)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("pending login did not preserve its deadline: %v", err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.begins != 1 || probe.cancels != 0 || probe.reads < 2 || probe.reads > 4 {
				t.Fatalf("login was repeated/canceled or polling was unbounded: begins=%d cancels=%d reads=%d", probe.begins, probe.cancels, probe.reads)
			}
		})
	}
}

func pendingLoginStatus() *apb.NodeStatus {
	return &apb.NodeStatus{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED", LoginId: "login-test", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}
}

func runNodeLogin(ctx context.Context, socket string, args ...string) (string, string, error) {
	var out, stderr bytes.Buffer
	command := newRoot(nil, &out, &stderr)
	command.SetArgs(append([]string{"--json", "node", "--socket", socket, "login"}, args...))
	err := command.ExecuteContext(ctx)
	return out.String(), stderr.String(), err
}

type nodeLoginProbe struct {
	arpc.UnimplementedNodeControlHandler
	mu                     sync.Mutex
	statuses               []*apb.NodeStatus
	authKey                bool
	begins, reads, cancels int
}

func (p *nodeLoginProbe) authorize(ctx context.Context, negotiate bool) error {
	uid, ok := connection.PeerUID(ctx)
	if !ok || uid != uint32(os.Getuid()) {
		return errors.New("test node: missing owner UID")
	}
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok || (!negotiate && info.RequestHeader().Get("Shiki-Node-Control-Instance-Id") != "node-login-test") {
		return errors.New("test node: missing negotiated instance")
	}
	return nil
}
func (p *nodeLoginProbe) Negotiate(ctx context.Context, _ *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	if err := p.authorize(ctx, true); err != nil {
		return nil, err
	}
	return &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"node.login.v1"}}, InstanceId: "node-login-test"}, nil
}
func (p *nodeLoginProbe) BeginLogin(ctx context.Context, request *apb.BeginLoginRequest) (*apb.NodeStatus, error) {
	if err := p.authorize(ctx, false); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.begins++
	if request.RequestId == "" || (p.authKey && string(request.GetAuthKey()) != "unit-test-auth-key") || (!p.authKey && !request.GetInteractive()) {
		return nil, errors.New("test node: unexpected login request")
	}
	return &apb.NodeStatus{State: "authenticating", LoginId: "login-test", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}, nil
}
func (p *nodeLoginProbe) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	if err := p.authorize(ctx, false); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result := proto.Clone(p.statuses[min(p.reads, len(p.statuses)-1)]).(*apb.NodeStatus)
	p.reads++
	return result, nil
}
func (p *nodeLoginProbe) CancelLogin(ctx context.Context, _ *apb.CancelLoginRequest) (*apb.NodeStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancels++
	return nil, errors.New("test node: CLI must not cancel an accepted login")
}
func (p *nodeLoginProbe) checkCalls(t *testing.T, reads int) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.begins != 1 || p.reads != reads || p.cancels != 0 {
		t.Fatalf("unexpected login mutations/polls: begins=%d reads=%d cancels=%d", p.begins, p.reads, p.cancels)
	}
}
func nodeLoginSocket(t *testing.T, probe *nodeLoginProbe) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-node-login-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "node.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	rpc := connect.NewServer()
	arpc.RegisterNodeControlHandler(rpc, probe)
	server := connection.NewHTTPServer(t.Context(), connection.NewRPCHandler(rpc))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-done })
	return socket
}
