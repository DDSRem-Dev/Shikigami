//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"
)

func TestActiveSessionChoicesRespectKnownGrants(t *testing.T) {
	for _, tc := range []struct {
		name      string
		agreement *pb.ProtocolAgreement
		want      []string
	}{
		{"legacy", nil, []string{"observe", "manage", "exit"}},
		{"observer", &pb.ProtocolAgreement{DaemonId: "daemon", GrantedPermissions: []string{"session.attach"}}, []string{"observe", "manage", "exit"}},
		{"writer", &pb.ProtocolAgreement{DaemonId: "daemon", GrantedPermissions: []string{"session.attach", "session.create", "session.write"}}, []string{"observe", "create", "manage", "exit"}},
		{"no attach", &pb.ProtocolAgreement{DaemonId: "daemon"}, []string{"manage", "exit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, choice := range activeSessionChoices(&pb.Session{Name: "main", ProjectId: "proj_one"}, tc.agreement) {
				got = append(got, choice.action)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("choices %v, want %v", got, tc.want)
			}
		})
	}
}

type smartRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu        sync.Mutex
	created   *pb.CreateSessionRequest
	opened    *pb.TerminalOpen
	forbidden atomic.Int32
}

func (p *smartRemote) GetProject(_ context.Context, r *pb.GetProjectRequest) (*pb.Project, error) {
	return &pb.Project{Id: r.Id, Name: "test-project"}, nil
}

func (p *smartRemote) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{Sessions: []*pb.Session{{Id: "sess_main", Name: "dashboard-main", ProjectId: "proj_one", ClientCount: 1}}}, nil
}

func (p *smartRemote) ListProjectSessions(_ context.Context, r *pb.ListProjectSessionsRequest) (*pb.ListProjectSessionsResponse, error) {
	if r.ProjectId != "proj_one" {
		return nil, errors.New("wrong project")
	}
	return &pb.ListProjectSessionsResponse{Sessions: []*pb.Session{{Id: "sess_main", Name: "main", ProjectId: "proj_one", ClientCount: 1}}}, nil
}
func (p *smartRemote) GetSession(_ context.Context, r *pb.GetSessionRequest) (*pb.Session, error) {
	return &pb.Session{Id: r.Id, Name: "main", ProjectId: "proj_one", ClientCount: 1}, nil
}
func (p *smartRemote) CreateSession(_ context.Context, r *pb.CreateSessionRequest) (*pb.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created = proto.Clone(r).(*pb.CreateSessionRequest)
	return &pb.Session{Id: "sess_new", Name: r.Name, ProjectId: r.ProjectId, WorkingDir: r.WorkingDir}, nil
}
func (p *smartRemote) TerminalStream(_ context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
	first, err := s.Receive()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.opened = proto.Clone(first.GetOpen()).(*pb.TerminalOpen)
	readOnly := p.opened.ReadOnly
	p.mu.Unlock()
	mode := "controller"
	if readOnly {
		mode = "observer"
	}
	id := first.GetOpen().SessionId
	if err = s.Send(&pb.TerminalOutput{SessionId: id, AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": mode, "control_revision": "revision"}}}}); err != nil {
		return err
	}
	if err = s.Send(&pb.TerminalOutput{SessionId: id, AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: []byte("SMART_READY")}}); err != nil {
		return err
	}
	for {
		f, e := s.Receive()
		if e != nil {
			return e
		}
		if readOnly && (len(f.GetData()) > 0 || f.GetResize() != nil || f.GetControl() != nil) {
			p.forbidden.Add(1)
		}
	}
}
func smartReadPTY(t *testing.T, ctx context.Context, fd int, want string) string {
	t.Helper()
	var output strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			output.Write(buf[:n])
			if strings.Contains(output.String(), want) {
				return output.String()
			}
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %q: %s", want, output.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestSmartActiveSessionPTY(t *testing.T) {
	for _, action := range []string{"observe", "create", "manage", "exit"} {
		t.Run(action, func(t *testing.T) {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			remote := &smartRemote{}
			probe := &managementProbe{remote: remote, mapping: &ppb.Mapping{Id: "map", ServerId: "server", ProjectId: "proj_one", LocalRoot: cwd, DefaultSession: "sess_main", Revision: "original"}, server: &apb.ServerRecord{Config: &apb.ServerConfig{Id: "server", DaemonId: "daemon_test"}, RemoteProtocol: &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_test", GrantedPermissions: []string{"session.attach", "session.create", "session.write"}}}}
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
			var logCalls atomic.Int32
			probe.logCheck = func(r *apb.PluginLogsRequest) error {
				logCalls.Add(1)
				if action == "manage" {
					if r.Request.Follow || r.Request.Tail != 10 {
						return errors.New("unbounded dashboard logs")
					}
					return nil
				}
				current, e := term.GetState(int(slave.Fd()))
				if e != nil || !reflect.DeepEqual(saved, current) {
					return errors.New("logs requested before terminal restoration")
				}
				if r.Request.Follow || r.Request.Tail != 20 {
					return errors.New("unbounded or following diagnostic logs")
				}
				return nil
			}
			path := managementFixture(t, probe)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			root := newRoot(slave, slave, slave)
			root.SetArgs([]string{"--config", path, "--verbose"})
			done := make(chan error, 1)
			go func() { done <- root.ExecuteContext(ctx) }()
			menu := smartReadPTY(t, ctx, fd, "Action:")
			for _, want := range []string{"read-only", "Create a new session", "Manage sessions", "Exit"} {
				if !strings.Contains(menu, want) {
					t.Fatalf("missing %s: %s", want, menu)
				}
			}
			answer := map[string]string{"observe": "1\n", "create": "2\nfrontend\nfrontend\nReact UI\n", "manage": "3\n", "exit": "4\n"}[action]
			if _, err = unix.Write(fd, []byte(answer)); err != nil {
				t.Fatal(err)
			}
			if action == "manage" {
				smartReadPTY(t, ctx, fd, "dashboard-main")
				if _, err = unix.Write(fd, []byte("q")); err != nil {
					t.Fatal(err)
				}
			} else if action != "exit" {
				before := smartReadPTY(t, ctx, fd, "SMART_READY")
				if strings.Contains(before, "PLUGIN_READY") || logCalls.Load() != 0 {
					t.Fatal("plugin log output entered raw terminal", before)
				}
				if _, err = unix.Write(fd, []byte{'x', 17}); err != nil {
					t.Fatal(err)
				}
				after := smartReadPTY(t, ctx, fd, "Detached; the remote session continues.")
				if !strings.Contains(after, "PLUGIN_READY") || strings.Contains(after, "before terminal restoration") {
					t.Fatal("missing safe post-detach logs", after)
				}
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			restored, err := term.GetState(int(slave.Fd()))
			if err != nil || !reflect.DeepEqual(saved, restored) {
				t.Fatal("terminal state not restored", err)
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if action == "exit" || action == "manage" {
				if remote.opened != nil || remote.created != nil || (action == "exit" && logCalls.Load() != 0) {
					t.Fatal("exit changed session")
				}
			} else {
				wantID := "sess_main"
				if action == "create" {
					wantID = "sess_new"
				}
				if remote.opened == nil || remote.opened.SessionId != wantID || remote.opened.ReadOnly != (action == "observe") {
					t.Fatalf("wrong terminal Open: %v", remote.opened)
				}
				if remote.forbidden.Load() != 0 {
					t.Fatal("read-only attachment sent input, resize or control")
				}
				if logCalls.Load() != 2 {
					t.Fatal("missing finite plugin requests", logCalls.Load())
				}
			}
			if action == "create" {
				r := remote.created
				if r == nil || r.ProjectId != "proj_one" || r.Name != "frontend" || r.WorkingDir != "frontend" || r.Description != "React UI" || r.RequestId == "" {
					t.Fatal("creation lost fields/scope", fmt.Sprint(r))
				}
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.mapping.DefaultSession != "sess_main" || probe.mapping.Revision != "original" {
				t.Fatal("active menu changed established directory default")
			}
		})
	}
}

func TestVerbosePreservesJSONAndNoninteractiveContracts(t *testing.T) {
	probe := &managementProbe{remote: &statsRemote{}}
	var calls atomic.Int32
	probe.logCheck = func(*apb.PluginLogsRequest) error { calls.Add(1); return nil }
	path := managementFixture(t, probe)
	output, err := runManagement(t, path, "--verbose", "--no-color", "stats", "--server", "server")
	if err != nil || !json.Valid([]byte(output)) || calls.Load() != 0 {
		t.Fatalf("verbose polluted management JSON: %q %v", output, err)
	}
	for _, args := range [][]string{{"--verbose"}, {"--verbose", "attach", "sess_main"}, {"--verbose", "dashboard"}} {
		var out bytes.Buffer
		root := newRoot(nil, &out, &out)
		root.SetArgs(append([]string{"--config", "/missing"}, args...))
		if err := root.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "terminal") || out.Len() != 0 {
			t.Fatalf("invalid noninteractive mode: %v %q %v", args, out.String(), err)
		}
	}
}
