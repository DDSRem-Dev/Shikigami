package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

func readinessExecutable(t *testing.T, output string, code int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-fixture")
	source := "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '2.1.310 (Claude Code)\\n'; exit 0; fi\n[ \"$1 $2 $3\" = 'auth status --json' ] || exit 9\nprintf '%s' '" + strings.ReplaceAll(output, "'", "'\\''") + "'\nprintf 'private-test-stderr' >&2\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(path, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeLoginDiagnosticKeepsOnlyStrictBoolean(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		code         int
		want         string
	}{
		{"signed-in", `{"loggedIn":true,"account":"private-test-account","token":"private-test-token"}`, 0, "reported_logged_in"},
		{"signed-out", `{"loggedIn":false}`, 0, "reported_logged_out"},
		{"signed-out-exit-one", `{"loggedIn":false}`, 1, "reported_logged_out"},
		{"true-failed", `{"loggedIn":true}`, 1, "unknown"},
		{"unsupported", `{"loggedIn":false}`, 2, "unknown"},
		{"string", `{"loggedIn":"true"}`, 0, "unknown"},
		{"null", `{"loggedIn":null}`, 0, "unknown"},
		{"missing", `{"other":true}`, 0, "unknown"},
		{"duplicate", `{"loggedIn":false,"logged\u0049n":true}`, 0, "unknown"},
		{"trailing", `{"loggedIn":true} {}`, 0, "unknown"},
		{"oversized", `{"loggedIn":true,"extra":"` + strings.Repeat("x", 4096) + `"}`, 0, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeLoginState(t.Context(), readinessExecutable(t, tc.output, tc.code)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if got := claudeLoginState(t.Context(), path); got != "unknown" || time.Since(started) > 5*time.Second {
		t.Fatal("login probe did not enforce its deadline", got)
	}
}

func TestDaemonReadinessIsLocalBoundAndReadOnly(t *testing.T) {
	ctx := t.Context()
	root, err := os.MkdirTemp("/tmp", "shiki-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	state, err := storage.Open(filepath.Join(root, "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	identity, err := auth.Open(filepath.Join(root, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer identity.Close()
	executable := readinessExecutable(t, `{"loggedIn":true,"account":"private-test-account","token":"private-test-token"}`, 0)
	cfg := config.DefaultDaemon("tester", filepath.Join(root, "state"))
	cfg.Claude.Executable = executable
	r := &Runtime{ctx: ctx, state: state, cfg: *cfg, info: DaemonInfo{RemoteReady: true}, Service: &Service{Auth: identity, Build: "test", ClaudeExecutable: executable}}
	handler := newDaemonReadiness(r)
	rpcServer := connect.NewServer()
	arpc.RegisterDaemonReadinessHandler(rpcServer, handler)
	listener, err := connection.ListenUnix(filepath.Join(root, "admin.sock"), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connection.NewHTTPServer(ctx, connection.NewRPCHandler(rpcServer))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { server.Close(); <-done }()
	endpoint, err := connection.NewUnixClient(listener.Addr().String(), uint32(os.Getuid()), "readiness-test")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	api := arpc.NewDaemonReadinessClient(connection.NewRPCClient(endpoint))
	if _, err := api.GetStatus(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("unnegotiated status accepted", err)
	}
	offer := &pb.ProtocolOffer{Major: 1, Capabilities: []string{"daemon.readiness.v1"}, RequiredCapabilities: []string{"daemon.readiness.v1"}}
	hello, err := api.Negotiate(ctx, offer)
	if err != nil || hello.InstanceId == "" || hello.Protocol.GetBuildVersion() != "test" || hello.Protocol.GetDaemonId() == "" {
		t.Fatal("invalid readiness negotiation", hello, err)
	}
	call, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set(readinessInstanceHeader, hello.InstanceId)
	status, err := api.GetStatus(call, &pb.Empty{})
	if err != nil || status.State != "local_application_ready" || status.ClaudeLoginState != "reported_logged_in" || status.ExternalPeerAcceptance != "pending" || status.System.DaemonId != hello.Protocol.DaemonId {
		t.Fatal(status, err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "private-test") || strings.Contains(string(encoded), "login_not_checked") {
		t.Fatal("readiness leaked CLI output or contradicted its login result")
	}
	before, err := os.ReadFile(filepath.Join(root, "state", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.GetStatus(call, &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "state", "state.json"))
	if string(before) != string(after) {
		t.Fatal("readiness rewrote business state")
	}
	info.RequestHeader().Set(readinessInstanceHeader, "stale")
	if _, err = api.GetStatus(call, &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("stale instance accepted", err)
	}
	info.RequestHeader().Set(readinessInstanceHeader, hello.InstanceId)
	info.RequestHeader().Set("Authorization", "Bearer private-test-token")
	if _, err = api.GetStatus(call, &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("Authorization accepted on local readiness", err)
	}
	info.RequestHeader().Delete("Authorization")
	state.Close()
	status, err = api.GetStatus(call, &pb.Empty{})
	if err != nil || status.State != "degraded" || status.ReasonCode != "STATE_UNAVAILABLE" || status.Health.Status != pb.HealthStatus_DEGRADED {
		t.Fatal("closed state store claimed readiness", status, err)
	}
	// A non-UDS transport has no authenticated owner UID, even when the service
	// is deliberately mounted by a test. Production never registers it remotely.
	remote := httptest.NewUnstartedServer(connection.NewRPCHandler(rpcServer))
	remote.EnableHTTP2 = true
	remote.StartTLS()
	defer remote.Close()
	remoteEndpoint, err := connection.NewTLSClient(remote.URL, connection.TLSOptions{RootCAs: remote.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs})
	if err != nil {
		t.Fatal(err)
	}
	defer remoteEndpoint.Close()
	if _, err := arpc.NewDaemonReadinessClient(connection.NewRPCClient(remoteEndpoint)).Negotiate(ctx, offer); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("non-owner transport accepted", err)
	}
}
