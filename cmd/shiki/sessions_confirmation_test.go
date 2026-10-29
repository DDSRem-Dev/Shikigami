//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
)

type sessionConfirmationRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu       sync.Mutex
	actions  []string
	sessions []string
}

func (p *sessionConfirmationRemote) GetProject(_ context.Context, request *pb.GetProjectRequest) (*pb.Project, error) {
	return &pb.Project{Id: request.Id}, nil
}

func (p *sessionConfirmationRemote) GetSession(_ context.Context, request *pb.GetSessionRequest) (*pb.Session, error) {
	return &pb.Session{Id: request.Id, ProjectId: "proj_fixture", Name: "active-task", ClaudeStatus: "running"}, nil
}

func (p *sessionConfirmationRemote) record(action string, request *pb.SessionActionRequest) (*pb.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.actions = append(p.actions, action)
	p.sessions = append(p.sessions, request.SessionId)
	return &pb.Session{Id: request.SessionId, ProjectId: "proj_fixture", Name: "active-task"}, nil
}

func (p *sessionConfirmationRemote) StopClaude(_ context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return p.record("stop-claude", request)
}

func (p *sessionConfirmationRemote) RestartClaude(_ context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return p.record("restart-claude", request)
}

func (p *sessionConfirmationRemote) check(t *testing.T, command string, count int) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.actions) != count || len(p.sessions) != count {
		t.Fatalf("mutations = %v for sessions %v; want %d", p.actions, p.sessions, count)
	}
	if count == 1 && (p.actions[0] != command || p.sessions[0] != "sess_fixture") {
		t.Fatalf("wrong mutation or scope: %v %v", p.actions, p.sessions)
	}
}

func TestSessionProcessConfirmationNoninteractive(t *testing.T) {
	for _, command := range []string{"stop-claude", "restart-claude"} {
		for _, tc := range []struct {
			name, project string
			flags         []string
			wantError     string
			mutations     int
		}{
			{name: "missing confirmation", project: "proj_fixture", wantError: "--yes"},
			{name: "explicit false", project: "proj_fixture", flags: []string{"--yes=false"}, wantError: "--yes"},
			{name: "confirmed", project: "proj_fixture", flags: []string{"--yes"}, mutations: 1},
			{name: "wrong project", project: "proj_other", flags: []string{"--yes"}, wantError: "does not belong"},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				remote := &sessionConfirmationRemote{}
				path := managementFixture(t, &managementProbe{remote: remote})
				args := []string{"sessions", command, "sess_fixture", "--server", "server", "--project", tc.project}
				output, err := runManagement(t, path, append(args, tc.flags...)...)
				if tc.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantError) || output != "" {
						t.Errorf("unconfirmed/invalid action: output %q, error %v; want %q", output, err, tc.wantError)
					}
				} else if err != nil || !strings.Contains(output, "sess_fixture") {
					t.Errorf("confirmed action: output %q, error %v", output, err)
				}
				remote.check(t, command, tc.mutations)
			})
		}
	}
}

func TestSessionProcessConfirmationInteractive(t *testing.T) {
	for _, command := range []string{"stop-claude", "restart-claude"} {
		for _, answer := range []string{"yes", "no", ""} {
			t.Run(command+"/answer="+answer, func(t *testing.T) {
				remote := &sessionConfirmationRemote{}
				path := managementFixture(t, &managementProbe{remote: remote})
				master, slave, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				defer slave.Close()
				if _, err = master.Write([]byte(answer + "\n")); err != nil {
					t.Fatal(err)
				}
				var diagnostics bytes.Buffer
				root := newRoot(slave, slave, &diagnostics)
				root.SetArgs([]string{"--config", path, "sessions", command, "sess_fixture", "--server", "server", "--project", "proj_fixture"})
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				err = root.ExecuteContext(ctx)
				count := 0
				if answer == "yes" {
					count = 1
					if err != nil {
						t.Errorf("confirmed action failed: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "canceled") {
					t.Errorf("declined action returned %v", err)
				}
				for _, text := range []string{"active-task", "server", "proj_fixture", "pane process", "project files remain", "type yes"} {
					if !strings.Contains(diagnostics.String(), text) {
						t.Errorf("confirmation omits %q: %s", text, diagnostics.String())
					}
				}
				remote.check(t, command, count)
			})
		}
	}
}

func TestSessionProcessConfirmationHelp(t *testing.T) {
	for _, command := range []string{"stop-claude", "restart-claude"} {
		output, err := runManagement(t, "/missing/config", "sessions", command, "--help")
		if err != nil || !strings.Contains(output, "--yes") {
			t.Errorf("%s does not document confirmation: %s %v", command, output, err)
		}
	}
}
