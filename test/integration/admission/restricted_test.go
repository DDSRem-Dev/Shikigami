package admission_test

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/internal/server/monitor"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestRestrictedTokensUseAgentWithoutSystemInfoPermission(t *testing.T) {
	mux, err := tmux.New("")
	if err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	root, err := os.MkdirTemp("/tmp", "shiki-admit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	state, err := storage.Open(filepath.Join(root, "daemon"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projects, err := project.New(state, filepath.Join(root, "projects"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer projects.Close()
	sessions, err := session.NewManager(ctx, state, mux, session.ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sh", "-c", `printf 'RESTRICTED_SCREEN'; i=0; while [ "$i" -lt 60 ]; do sleep 1; printf 'TICK_%s\n' "$i"; i=$((i+1)); done`}})
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	authentication, err := auth.Open(filepath.Join(root, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer authentication.Close()
	service := &server.Service{Auth: authentication, Projects: projects, Sessions: sessions, Monitor: monitor.New(sessions, filepath.Join(root, "daemon")), Build: "test"}
	tlsServer := httptest.NewUnstartedServer(service.Handler())
	tlsServer.EnableHTTP2 = true
	tlsServer.StartTLS()
	defer tlsServer.Close()
	ca := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Agent.AutoStart = false
	cfg.Agent.StateDir = filepath.Join(root, "agent")
	cfg.Agent.Socket = filepath.Join(root, "run", "agent.sock")
	cfg.PluginsDir = filepath.Join(root, "plugins")
	configPath := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	agentDone := make(chan error, 1)
	go func() { agentDone <- agent.Run(agentCtx, configPath, "test") }()
	defer func() {
		stopAgent()
		select {
		case err := <-agentDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("agent did not stop")
		}
	}()
	var frontend *client.Client
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		frontend, err = client.Open(ctx, configPath, client.Options{Timeout: time.Second})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer frontend.Close()
	daemonID, err := authentication.DaemonID()
	if err != nil {
		t.Fatal(err)
	}
	add := func(name, token, pin string) *apb.ServerRecord {
		t.Helper()
		record, err := frontend.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{Config: &apb.ServerConfig{Name: name, DirectTlsAddress: strings.TrimPrefix(tlsServer.URL, "https://"), TlsServerName: "example.com", TlsCaFile: ca, DaemonId: pin}, RequestId: "add-" + name})
		if err != nil {
			t.Fatal(err)
		}
		record, err = frontend.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte(token), RequestId: "credential-" + name})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	statsToken, statsInfo, err := authentication.Create("tester", []string{"system.stats"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	statsServer := add("stats", statsToken, "")
	for range 2 { // second call re-verifies the cached connection without system.info.
		stats, err := frontend.Remote(statsServer.Config.Id).GetStats(ctx, &pb.StatsRequest{})
		if err != nil || stats.MemoryUsedBytes == nil {
			t.Fatalf("stats-only token rejected by connection admission: %v %v", stats, err)
		}
	}
	if _, err := frontend.Remote(statsServer.Config.Id).GetSystemInfo(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("system.info unexpectedly granted", err)
	}
	if _, err := frontend.Remote(statsServer.Config.Id).ListSessions(ctx, &pb.ListSessionsRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("session.list unexpectedly granted", err)
	}
	statsRecord, err := frontend.ResolveServer(ctx, statsServer.Config.Id)
	if err != nil {
		t.Fatal(err)
	}
	if statsRecord.Config.DaemonId != daemonID || statsRecord.RemoteProtocol.GetDaemonId() != daemonID || !slices.Equal(statsRecord.RemoteProtocol.GrantedPermissions, []string{"system.stats"}) || !slices.Contains(statsRecord.RemoteProtocol.Capabilities, "stats.v1") {
		t.Fatalf("negotiated identity/grants were not published: %v", statsRecord)
	}
	wire, _ := protojson.Marshal(statsRecord.RemoteProtocol)
	if strings.Contains(string(wire), statsToken) || strings.Contains(string(wire), "tester") || strings.Contains(string(wire), statsInfo.ID) {
		t.Fatal("negotiation leaked credential/account identity")
	}

	projectRecord, err := projects.Create(ctx, &pb.CreateProjectRequest{Name: "application", Path: filepath.Join(root, "projects", "app"), RequestId: "project"})
	if err != nil {
		t.Fatal(err)
	}
	sessionRecord, err := sessions.Create(ctx, &pb.CreateSessionRequest{ProjectId: projectRecord.Id, Name: "readonly", RequestId: "session"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = mux.Delete(cleanup, sessionRecord.Id)
	}()
	readerToken, _, err := authentication.Create("tester", []string{"project.list", "session.list", "session.attach"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	readerServer := add("reader", readerToken, "")
	reader := frontend.Remote(readerServer.Config.Id)
	if _, err := reader.GetSession(ctx, &pb.GetSessionRequest{Id: sessionRecord.Id}); err != nil {
		t.Fatal("read-only session lookup rejected", err)
	}
	screen, err := reader.GetSessionScreen(ctx, &pb.GetSessionRequest{Id: sessionRecord.Id})
	if err != nil || !strings.Contains(screen.GetText(), "RESTRICTED_SCREEN") {
		t.Fatalf("read-only screen failed: %v %v", screen, err)
	}
	if _, err := reader.UpdateSession(ctx, &pb.UpdateSessionRequest{Id: sessionRecord.Id, Name: "forbidden", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}}}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only rename was not denied", err)
	}
	if _, err := reader.DeleteSession(ctx, &pb.DeleteSessionRequest{Id: sessionRecord.Id}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only deletion was not denied", err)
	}
	observer, err := reader.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: sessionRecord.Id, ReadOnly: true, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := observer.Receive()
	if err != nil || ready.GetEvent().GetData()["mode"] != "observer" {
		t.Fatalf("read-only attach: %v %v", ready, err)
	}
	if _, err := reader.TakeControl(ctx, &pb.TakeControlRequest{SessionId: sessionRecord.Id, AttachmentId: ready.AttachmentId, ExpectedControlRevision: ready.GetEvent().GetData()["control_revision"]}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only takeover was not denied", err)
	}
	if _, err := reader.GetSystemInfo(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only system.info was not denied", err)
	}
	var observed strings.Builder
	for !strings.Contains(observed.String(), "TICK_0") {
		frame, err := observer.Receive()
		if err != nil {
			t.Fatal("unrelated permission denial canceled authorized observer", err)
		}
		observed.Write(frame.GetData())
	}
	observer.Close()
	wrong := add("wrong-identity", statsToken, "daemon_wrong")
	if _, err := frontend.Remote(wrong.Config.Id).GetStats(ctx, &pb.StatsRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("authenticated identity mismatch was not rejected", err)
	}
	if err := authentication.Revoke(statsInfo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := frontend.Remote(statsServer.Config.Id).GetStats(ctx, &pb.StatsRequest{}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal("revoked cached credential was admitted", err)
	}
}
