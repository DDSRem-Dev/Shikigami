//go:build darwin || linux

package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type dashboardProbe struct {
	serverRecord atomic.Pointer[apb.ServerRecord]
	arpc.UnimplementedAgentHandler
	subscribed  atomic.Bool
	snapshots   atomic.Int32
	logRequests chan *apb.PluginLogsRequest
}

func (p *dashboardProbe) ListPlugins(context.Context, *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	return &pb.ListPluginsResponse{Plugins: []*pb.PluginStatus{{PluginId: "sync", State: "ready"}}}, nil
}

func (p *dashboardProbe) WatchPluginLogs(_ context.Context, request *apb.PluginLogsRequest, stream arpc.AgentWatchPluginLogsServerStream) error {
	if p.logRequests != nil {
		p.logRequests <- proto.Clone(request).(*apb.PluginLogsRequest)
	}
	return stream.Send(&pb.LogRecord{Timestamp: 123, Level: "warn", Message: "measured-plugin-record"})
}

func (p *dashboardProbe) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: "frontend", Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"agent.control.v1", "agent.events.v1"}}}, nil
}
func (p *dashboardProbe) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (p *dashboardProbe) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (p *dashboardProbe) ListServers(context.Context, *pb.Empty) (*apb.ServerList, error) {
	if record := p.serverRecord.Load(); record != nil {
		return &apb.ServerList{Servers: []*apb.ServerRecord{proto.Clone(record).(*apb.ServerRecord)}}, nil
	}
	return &apb.ServerList{Servers: []*apb.ServerRecord{{Config: &apb.ServerConfig{Id: "server", Name: "test-server"}, ConnectionState: "ready", RemoteProtocol: testAgreement()}}}, nil
}
func (p *dashboardProbe) WatchEvents(ctx context.Context, _ *pb.Empty, stream arpc.AgentWatchEventsServerStream) error {
	p.subscribed.Store(true)
	if err := stream.Send(&apb.AgentEvent{InstanceId: "instance", Sequence: 1, Type: "rescan_required"}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

type dashboardRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	probe     *dashboardProbe
	inputs    chan []byte
	mutations chan proto.Message
}

func (p *dashboardRemote) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	if !p.probe.subscribed.Load() {
		return nil, errors.New("snapshot preceded event subscription")
	}
	p.probe.snapshots.Add(1)
	return &pb.ListSessionsResponse{Sessions: []*pb.Session{{Id: "sess_test", ProjectId: "proj_test", Name: "unique-session", ClaudeStatus: "running"}}}, nil
}
func (p *dashboardRemote) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	return &pb.SystemInfo{}, nil
}
func (p *dashboardRemote) ListProjects(context.Context, *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	return &pb.ListProjectsResponse{Projects: []*pb.Project{{Id: "proj_one", Name: "one"}, {Id: "proj_two", Name: "two"}}}, nil
}
func (p *dashboardRemote) CreateSession(_ context.Context, request *pb.CreateSessionRequest) (*pb.Session, error) {
	p.mutations <- proto.Clone(request)
	return &pb.Session{Id: "sess_created", ProjectId: request.ProjectId, Name: request.Name}, nil
}
func (p *dashboardRemote) UpdateSession(_ context.Context, request *pb.UpdateSessionRequest) (*pb.Session, error) {
	p.mutations <- proto.Clone(request)
	return &pb.Session{Id: request.Id, Name: request.Name}, nil
}
func (p *dashboardRemote) DeleteSession(_ context.Context, request *pb.DeleteSessionRequest) (*pb.Empty, error) {
	p.mutations <- proto.Clone(request)
	return &pb.Empty{}, nil
}
func (p *dashboardRemote) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	open, err := stream.Receive()
	if err != nil {
		return err
	}
	if open.GetOpen().SessionId != "sess_test" {
		return errors.New("wrong selected session")
	}
	if err := stream.Send(&pb.TerminalOutput{SessionId: "sess_test", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "controller", "control_revision": "revision"}}}}); err != nil {
		return err
	}
	if err := stream.Send(&pb.TerminalOutput{SessionId: "sess_test", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: []byte("HANDOFF_READY")}}); err != nil {
		return err
	}
	for {
		frame, err := stream.Receive()
		if err != nil {
			return err
		}
		if data := frame.GetData(); len(data) > 0 {
			select {
			case p.inputs <- data:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
func dashboardClient(t *testing.T, nodes ...arpc.NodeControlHandler) (*client.Client, *dashboardProbe, *dashboardRemote) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-ui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(dir, "agent.sock")
	cfg.Agent.StateDir = dir
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	probe := &dashboardProbe{}
	remote := &dashboardRemote{probe: probe, inputs: make(chan []byte, 4), mutations: make(chan proto.Message, 8)}
	server := connect.NewServer()
	arpc.RegisterAgentHandler(server, probe)
	for _, node := range nodes {
		arpc.RegisterNodeControlHandler(server, node)
	}
	rpc.RegisterShikiDaemonHandler(server, remote)
	listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := connection.NewHTTPServer(t.Context(), connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	t.Cleanup(func() { httpServer.Close(); <-done })
	c, err := client.Open(t.Context(), path, client.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, probe, remote
}
func readPTY(t *testing.T, ctx context.Context, fd int, want string) {
	t.Helper()
	var text strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buffer)
		if n > 0 {
			text.Write(buffer[:n])
			if strings.Contains(text.String(), want) {
				return
			}
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("PTY did not show %q: %s", want, text.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestDashboardTerminalHandoffRestoresReaderAndRawMode(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	c, probe, remote := dashboardClient(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	saved, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	type result struct {
		action Action
		err    error
	}
	done := make(chan result, 1)
	go func() {
		action, err := Run(ctx, c, Options{Input: slave, Output: slave, ServerID: "server", NoColor: true})
		done <- result{action, err}
	}()
	readPTY(t, ctx, fd, "unique-session")
	if _, err := unix.Write(fd, []byte("\r")); err != nil {
		t.Fatal(err)
	}
	var action Action
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		action = result.action
	case <-ctx.Done():
		t.Fatal("dashboard did not exit for attachment")
	}
	if action.Session == nil || action.Session.Id != "sess_test" || probe.snapshots.Load() == 0 {
		t.Fatal("dashboard did not load/choose real IPC session", action)
	}
	restored, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(saved, restored) {
		t.Fatal("dashboard did not restore terminal before handoff", err)
	}
	terminalDone := make(chan error, 1)
	go func() {
		terminalDone <- c.Terminal(ctx, action.ServerID, action.Session.Id, client.TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
	}()
	readPTY(t, ctx, fd, "HANDOFF_READY")
	if _, err := unix.Write(fd, []byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-remote.inputs:
		if string(input) != "x" {
			t.Fatalf("handoff input changed: %q", input)
		}
	case <-ctx.Done():
		t.Fatal("dashboard reader consumed remote terminal input")
	}
	if _, err := unix.Write(fd, []byte{17}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-terminalDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("terminal did not detach")
	}
	restored, err = term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(saved, restored) {
		t.Fatal("terminal did not restore state after detach", err)
	}
}

func TestDashboardCancellationWithoutKeyRestoresTerminal(t *testing.T) {
	c, _, remote := dashboardClient(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	saved, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, c, Options{Input: slave, Output: slave, ServerID: "server", NoColor: true})
		done <- err
	}()
	wait, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	readPTY(t, wait, fd, "unique-session")
	cancel()
	select {
	case err := <-done:
		if err != nil && strings.Contains(err.Error(), "unexpected dashboard model") {
			t.Fatal(err)
		}
	case <-wait.Done():
		t.Fatal("dashboard cancellation did not terminate")
	}
	restored, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(saved, restored) {
		t.Fatal("canceled dashboard left raw mode", err)
	}
	// Cancellation must relinquish input ownership as completely as Enter
	// handoff. An old reader must not consume the next attachment's keystroke.
	terminalDone := make(chan error, 1)
	go func() {
		terminalDone <- c.Terminal(wait, "server", "sess_test", client.TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
	}()
	readPTY(t, wait, fd, "HANDOFF_READY")
	if _, err := unix.Write(fd, []byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-remote.inputs:
		if string(input) != "x" {
			t.Fatalf("post-cancellation input changed: %q", input)
		}
	case <-wait.Done():
		t.Fatal("canceled dashboard reader consumed attachment input")
	}
	if _, err := unix.Write(fd, []byte{17}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-terminalDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-wait.Done():
		t.Fatal("post-cancellation attachment did not detach")
	}
	restored, err = term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(saved, restored) {
		t.Fatal("post-cancellation attachment did not restore terminal", err)
	}
}
