package terminalstate_test

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
)

// The shell stands in for a TUI application. All terminal lifecycle, bytes,
// permissions, RESET handling and sizing use the real production components.
const screenProgram = `stty -echo
printf '\033[2J\033[HAPP_READY'
while IFS= read -r op; do
 case "$op" in
 M) printf '\033[?1049l\033[0m\033[2J\033[HMAIN_SCREEN\033[6;10H\033[31;44m红Z\033[0m\033[9;13H\033[?25h' ;;
 A) printf '\033[?1049h\033[0m\033[2J\033[HALT_SCREEN\033[4;6H\033[32;45m界Q\033[0m\033[8;12H\033[?25l' ;;
 R) set -- $(stty size); printf '\033[0m\033[2J\033[HALT_RESIZED_%s_%s\033[4;6H\033[32;45m界Q\033[0m\033[%s;%sHEDGE\033[8;12H\033[?25l' "$1" "$2" "$1" "$(( $2 - 3 ))" ;;
 E) printf '\033[?1049l\033[?25h' ;;
 esac
done`

type terminalFixture struct {
	identities          map[int]processIdentity
	ctx                 context.Context
	front               *client.Client
	serverID, sessionID string
	mux                 *tmux.Client
	pane                tmux.Pane
	root                string
}

func fixture(t *testing.T) *terminalFixture {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shiki-vt-")
	if err != nil {
		t.Fatal(err)
	}
	var completedFixture *terminalFixture
	t.Cleanup(func() {
		if completedFixture != nil {
			completedFixture.verifyCleanup(t)
		} else if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(scenarioContext, 60*time.Second)
	t.Cleanup(cancel)
	mux, err := tmux.New("")
	if err != nil {
		t.Fatal("tmux required", err)
	}
	state, err := storage.Open(filepath.Join(root, "daemon"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	projects, err := project.New(state, filepath.Join(root, "projects"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projects.Close(); err != nil {
			t.Error(err)
		}
	})
	sessions, err := session.NewManager(ctx, state, mux, session.ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sh", "-c", screenProgram}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sessions.Close(); err != nil {
			t.Error(err)
		}
	})
	authentication, err := auth.Open(filepath.Join(root, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { authentication.Close() })
	service := &server.Service{Auth: authentication, Projects: projects, Sessions: sessions, Monitor: monitor.New(sessions, filepath.Join(root, "daemon")), Build: "test"}
	tlsServer := httptest.NewUnstartedServer(service.Handler())
	tlsServer.EnableHTTP2 = true
	tlsServer.StartTLS()
	t.Cleanup(tlsServer.Close)
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
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx, configPath, "test") }()
	t.Cleanup(func() {
		stopAgent()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("agent did not stop")
		}
	})
	var front *client.Client
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		front, err = client.Open(ctx, configPath, client.Options{Timeout: time.Second})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { front.Close() })
	token, _, err := authentication.Create("tester", []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	record, err := front.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{Config: &apb.ServerConfig{Name: "terminal", DirectTlsAddress: strings.TrimPrefix(tlsServer.URL, "https://"), TlsServerName: "example.com", TlsCaFile: ca}, RequestId: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = front.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte(token), RequestId: "token"}); err != nil {
		t.Fatal(err)
	}
	p, err := projects.Create(ctx, &pb.CreateProjectRequest{Name: "terminal", Path: filepath.Join(root, "projects", "app"), RequestId: "project"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessions.Create(ctx, &pb.CreateSessionRequest{ProjectId: p.Id, Name: "screen", RequestId: "session"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mux.Delete(ctx, s.Id); err != nil {
			t.Error(err)
		}
	})
	pane, err := mux.Inspect(ctx, s.Id)
	if err != nil {
		t.Fatal(err)
	}
	f := &terminalFixture{ctx: ctx, front: front, serverID: record.Config.Id, sessionID: s.Id, mux: mux, pane: pane, root: root}
	completedFixture = f
	f.track(t)
	return f
}
