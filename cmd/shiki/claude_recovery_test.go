//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type crashRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu                              sync.Mutex
	state                           string
	emitCrash                       bool
	unknown, normalStop             bool
	restarts, attachments, logReads int
	restored                        func() bool
}

func (p *crashRemote) GetSession(context.Context, *pb.GetSessionRequest) (*pb.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.restored() {
		return nil, errors.New("recovery queried before raw restoration")
	}
	return &pb.Session{Id: "sess_crash", ProjectId: "proj_test", Name: "crashed-session", ClaudeStatus: p.state, TmuxSession: "shiki-sess_crash"}, nil
}
func (p *crashRemote) RestartClaude(_ context.Context, r *pb.SessionActionRequest) (*pb.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.SessionId != "sess_crash" || !p.restored() {
		return nil, errors.New("invalid restart scope/terminal mode")
	}
	p.restarts++
	p.state = "running"
	p.emitCrash = false
	return &pb.Session{Id: r.SessionId, ProjectId: "proj_test", Name: "crashed-session", ClaudeStatus: p.state}, nil
}
func (p *crashRemote) GetSessionLogs(_ context.Context, r *pb.SessionLogsRequest) (*pb.SessionLogs, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.SessionId != "sess_crash" || r.MaxLines != 2000 || !p.restored() {
		return nil, errors.New("invalid log request")
	}
	p.logReads++
	code := int32(7)
	return &pb.SessionLogs{SessionId: r.SessionId, Text: "RETAINED_OUTPUT\x1b[2J", State: "available", Truncated: true, ExitCode: &code}, nil
}
func (p *crashRemote) TerminalStream(_ context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
	input, err := s.Receive()
	if err != nil {
		return err
	}
	if input.GetOpen().SessionId != "sess_crash" {
		return errors.New("wrong attachment")
	}
	p.mu.Lock()
	p.attachments++
	crash := p.emitCrash
	p.emitCrash = false
	p.mu.Unlock()
	if err = s.Send(&pb.TerminalOutput{SessionId: "sess_crash", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "observer", "control_revision": "revision"}}}}); err != nil {
		return err
	}
	if crash {
		event := &pb.SessionEvent{Type: pb.SessionEvent_CLAUDE_CRASHED, Data: map[string]string{"exit_code": "7"}}
		if p.unknown {
			event = &pb.SessionEvent{Type: pb.SessionEvent_CLAUDE_STOPPED, Data: map[string]string{"reason_code": "EXIT_REASON_UNKNOWN"}}
		}
		if p.normalStop {
			event = &pb.SessionEvent{Type: pb.SessionEvent_CLAUDE_STOPPED, Data: map[string]string{"reason_code": "NORMAL_EXIT", "exit_code": "0"}}
		}
		if err = s.Send(&pb.TerminalOutput{SessionId: "sess_crash", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Event{Event: event}}); err != nil {
			return err
		}
	}
	if !crash || p.normalStop {
		sequence := uint64(2)
		if crash {
			sequence = 3
		}
		if err = s.Send(&pb.TerminalOutput{SessionId: "sess_crash", AttachmentId: "attachment", Sequence: sequence, Payload: &pb.TerminalOutput_Data{Data: []byte("RECOVERED_READY")}}); err != nil {
			return err
		}
	}
	for {
		if _, err = s.Receive(); err != nil {
			return err
		}
	}
}
func TestClaudeRecoveryOutsideRawMode(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		observer, automatic, preexisting bool
		unknown, normalStop              bool
		answers                          string
		starts, logs                     int
	}{
		{name: "restart", answers: "1\nyes\n", starts: 1},
		{name: "existing crashed", preexisting: true, answers: "1\nyes\n", starts: 1},
		{name: "observer logs", observer: true, answers: "1\n3\n", logs: 1},
		{name: "manual", answers: "3\n"},
		{name: "already auto restarted", automatic: true},
		{name: "unknown exit", unknown: true, answers: "4\n"},
		{name: "normal stop remains attached", normalStop: true, automatic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
				t.Fatal(err)
			}
			fd := int(master.Fd())
			if err = unix.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			saved, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			remote := &crashRemote{state: "crashed", emitCrash: true, restored: func() bool {
				state, e := term.GetState(int(slave.Fd()))
				return e == nil && reflect.DeepEqual(saved, state)
			}}
			remote.unknown, remote.normalStop = tc.unknown, tc.normalStop
			if tc.unknown {
				remote.state = "unknown"
			}
			if tc.automatic {
				remote.state = "running"
			}
			grants := []string{"session.attach", "session.list"}
			if !tc.observer {
				grants = append(grants, "session.control")
			}
			path := managementFixture(t, &managementProbe{remote: remote, server: &apb.ServerRecord{Config: &apb.ServerConfig{Id: "server"}, RemoteProtocol: &pb.ProtocolAgreement{DaemonId: "daemon", Capabilities: []string{"terminal.logs.v1"}, GrantedPermissions: grants}}})
			ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
			defer cancel()
			c, err := client.Open(ctx, path, client.Options{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			a := &app{input: slave, output: slave, errorOutput: slave}
			session := &pb.Session{Id: "sess_crash", ProjectId: "proj_test", Name: "crashed-session", ClaudeStatus: "running"}
			if tc.preexisting {
				session.ClaudeStatus = "crashed"
			}
			done := make(chan error, 1)
			go func() {
				done <- a.runTerminal(ctx, c, "server", session, client.TerminalOptions{Input: slave, Output: slave, ReadOnly: true, DetachKey: 17}, true)
			}()
			if !tc.automatic {
				menu := smartReadPTY(t, ctx, fd, "Recovery:")
				if tc.unknown && (!strings.Contains(menu, "exit reason is unknown") || !strings.Contains(menu, "Exit code: unknown") || strings.Contains(menu, "stopped unexpectedly")) {
					t.Fatal("unknown outcome was presented as a crash", menu)
				}
				if strings.Contains(menu, "Reconnecting (") {
					t.Fatal("crash entered network retry")
				}
				if tc.observer && strings.Contains(menu, "Restart Claude Code") {
					t.Fatal("observer offered restart", menu)
				}
				if _, err = unix.Write(fd, []byte(tc.answers)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.starts > 0 || tc.automatic {
				smartReadPTY(t, ctx, fd, "RECOVERED_READY")
				if _, err = unix.Write(fd, []byte{17}); err != nil {
					t.Fatal(err)
				}
			} else if tc.logs > 0 {
				text := smartReadPTY(t, ctx, fd, "RETAINED_OUTPUT")
				if !strings.Contains(text, "not durable history") || !strings.Contains(text, "truncated: true") || strings.Contains(text, "\x1b[2J") {
					t.Fatal("log range/sanitization missing", text)
				}
			} else if !tc.unknown {
				smartReadPTY(t, ctx, fd, "attach-session -t")
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.restarts != tc.starts || remote.logReads != tc.logs || !remote.restored() {
				t.Fatalf("unexpected recovery effects: starts=%d logs=%d", remote.restarts, remote.logReads)
			}
		})
	}
}
