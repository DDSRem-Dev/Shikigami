package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func TestAgentTLSProjectSessionTerminalAndRevocation(t *testing.T) {
	mux, err := tmux.New("")
	if err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	state, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projects, err := project.New(state, filepath.Join(t.TempDir(), "projects"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer projects.Close()
	sessions, err := session.NewManager(ctx, state, mux, session.ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sh", "-c", `printf '\033[?1049hRPC_SCREEN_世界'; read value; printf '\nRPC_REPLY:%s\n' "$value"; sleep 30`}, Terminal: session.TerminalOptions{MinRedrawInterval: time.Nanosecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	authentication, err := auth.Open(filepath.Join(t.TempDir(), "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer authentication.Close()
	token, tokenInfo, err := authentication.Create("tester", []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Auth: authentication, Projects: projects, Sessions: sessions, Build: "test", StartedAt: time.Now()}
	remote := httptest.NewUnstartedServer(service.Handler())
	remote.EnableHTTP2 = true
	remote.StartTLS()
	defer remote.Close()
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	endpoint, err := connection.NewTLSClient(remote.URL, connection.TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	credential, err := connection.BearerToken(token)
	if err != nil {
		t.Fatal(err)
	}
	trusted := connection.NewRPCClient(endpoint, credential)
	readers := map[string]*connect.Client{}
	for id, permission := range map[string]string{"file_reader": "file.read", "session_reader": "session.attach"} {
		value, _, err := authentication.Create("tester", []string{permission}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		bearer, err := connection.BearerToken(value)
		if err != nil {
			t.Fatal(err)
		}
		readers[id] = connection.NewRPCClient(endpoint, bearer)
	}
	remoteRPC := rpc.NewShikiDaemonClient(trusted)
	if _, err := remoteRPC.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1"}, RequiredCapabilities: []string{"terminal.v1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRPC.StatFile(ctx, &pb.StatFileRequest{ProjectId: "proj_missing", Path: "file"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled file plugin must fail closed: %v", err)
	}

	ipcDir, err := os.MkdirTemp("/tmp", "shiki-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(ipcDir)
	socket := filepath.Join(ipcDir, "agent.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	local := agent.NewService(ctx, uint32(os.Getuid()), "test", func(_ context.Context, id string) (*connect.Client, error) {
		if reader := readers[id]; reader != nil {
			return reader, nil
		}
		if id != "server_test" {
			return nil, connect.NewError(connect.CodeNotFound, "unknown server")
		}
		return trusted, nil
	})
	ipcServer := connection.NewHTTPServer(ctx, local.Handler(local.RPCServer()))
	serverDone := make(chan error, 1)
	go func() { serverDone <- ipcServer.Serve(listener) }()
	defer func() { _ = ipcServer.Close(); <-serverDone }()
	ipc, err := connection.NewUnixClient(socket, uint32(os.Getuid()), "shiki-agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ipc.Close()
	localClient := connection.NewRPCClient(ipc)
	frontend := arpc.NewAgentClient(localClient)
	proxy := rpc.NewShikiDaemonClient(localClient)
	openFrontend := func(route ...string) context.Context {
		t.Helper()
		hello, err := frontend.Handshake(ctx, &apb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1}})
		if err != nil {
			t.Fatal(err)
		}
		request, info := connect.NewClientContext(ctx)
		info.RequestHeader().Set(agent.InstanceHeader, hello.InstanceId)
		info.RequestHeader().Set(agent.FrontendHeader, hello.FrontendId)
		id := "server_test"
		if len(route) != 0 {
			id = route[0]
		}
		info.RequestHeader().Set(agent.ServerHeader, id)
		return request
	}
	firstContext, secondContext := openFrontend(), openFrontend()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projectRecord, err := proxy.CreateProject(firstContext, &pb.CreateProjectRequest{Name: "application", Path: root, RequestId: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "frontend"), 0700); err != nil {
		t.Fatal(err)
	}
	fileContext, sessionContext := openFrontend("file_reader"), openFrontend("session_reader")
	directories, err := proxy.ListProjectDirectories(fileContext, &pb.ListProjectDirectoriesRequest{ProjectId: projectRecord.Id})
	if err != nil || len(directories.GetDirectories()) != 1 || directories.Directories[0] != "frontend" {
		t.Fatalf("directory picker through IPC/TLS: %v %v", directories, err)
	}
	if _, err := proxy.ListProjectDirectories(sessionContext, &pb.ListProjectDirectoriesRequest{ProjectId: projectRecord.Id}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("directory permission: %v", err)
	}
	if _, err := proxy.ListFiles(fileContext, &pb.ListFilesRequest{ProjectId: projectRecord.Id}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("picker exposed sync file inventory: %v", err)
	}
	created, err := proxy.CreateSession(firstContext, &pb.CreateSessionRequest{ProjectId: projectRecord.Id, Name: "main", RequestId: "session"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := mux.Delete(cleanup, created.Id); err != nil {
			t.Error(err)
		}
	}()
	controller, err := proxy.TerminalStream(firstContext)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	if err := controller.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: created.Id, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := controller.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if ready.GetEvent().GetType() != pb.SessionEvent_READY || ready.GetEvent().Data["mode"] != "controller" {
		t.Fatal("missing controller READY")
	}
	terminalContains(t, controller, "RPC_SCREEN_世界")
	logs, err := proxy.GetSessionLogs(sessionContext, &pb.SessionLogsRequest{SessionId: created.Id, MaxLines: 20})
	if err != nil || logs.GetSessionId() != created.Id || logs.State == "unavailable" {
		t.Fatalf("retained logs through IPC/TLS: %v %v", logs, err)
	}
	if _, err := proxy.GetSessionLogs(fileContext, &pb.SessionLogsRequest{SessionId: created.Id}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("log permission: %v", err)
	}

	observer, err := proxy.TerminalStream(secondContext)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: created.Id, ReadOnly: true, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}}); err != nil {
		t.Fatal(err)
	}
	observed, err := observer.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if observed.GetEvent().Data["mode"] != "observer" {
		t.Fatal("read-only attachment acquired control")
	}
	terminalContains(t, observer, "RPC_SCREEN_世界")
	terminalContains(t, controller, "RPC_SCREEN_世界")
	if err := observer.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("forbidden\n")}}); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = observer.Receive()
		if err != nil {
			break
		}
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("observer input should preserve PermissionDenied through agent: %v", err)
	}
	if err := controller.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("allowed\n")}}); err != nil {
		t.Fatal(err)
	}
	terminalContains(t, controller, "RPC_REPLY:allowed")
	pane, err := mux.Inspect(ctx, created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := authentication.Revoke(tokenInfo.ID); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = controller.Receive()
		if err != nil {
			break
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("revocation failed to interrupt terminal")
	}
	after, err := mux.Inspect(ctx, created.Id)
	if err != nil || after.Dead || pane.PID != after.PID {
		t.Fatalf("revocation ended remote process: %+v %v", after, err)
	}
	if _, err := proxy.ListSessions(firstContext, &pb.ListSessionsRequest{}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("revoked credential allowed another call: %v", err)
	}
}

func terminalContains(t *testing.T, stream rpc.ShikiDaemonTerminalStreamClientStream, marker string) {
	t.Helper()
	var data []byte
	for !bytes.Contains(data, []byte(marker)) {
		output, err := stream.Receive()
		if err != nil {
			t.Fatalf("waiting for %q: %v; received %q", marker, err, data)
		}
		data = append(data, output.GetData()...)
	}
}

// This fixture uses a real HTTP/2 stream and a deterministic PTY byte source.
// Unlike tmux screen coalescing, it can reliably fill the HTTP/2 receive window.
type terminalPipe struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *terminalPipe) Read(data []byte) (int, error)  { return p.reader.Read(data) }
func (p *terminalPipe) Write(data []byte) (int, error) { return len(data), nil }
func (p *terminalPipe) Resize(uint32, uint32) error    { return nil }
func (p *terminalPipe) Close() error                   { _ = p.writer.Close(); return p.reader.Close() }

type terminalRPCProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	bridge *session.Bridge
	done   chan error
}

func (p *terminalRPCProbe) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	subscription, err := p.bridge.Open(ctx, first.GetOpen(), false)
	if err != nil {
		return err
	}
	control, ok := ctx.Value(terminalControlKey{}).(terminalIO)
	if !ok {
		return errors.New("missing transport controls")
	}
	err = serveTerminal(ctx, stream, p.bridge, subscription, control)
	p.done <- err
	return err
}

type observedSend struct {
	connect.ServerStream
	started, completed *atomic.Int64
}

func (s *observedSend) Send(message any) error {
	s.started.Add(1)
	err := s.ServerStream.Send(message)
	s.completed.Add(1)
	return err
}
func terminalRPCFixture(t *testing.T) (rpc.ShikiDaemonClient, *terminalPipe, *terminalRPCProbe, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	reader, writer := io.Pipe()
	pty := &terminalPipe{reader: reader, writer: writer}
	bridge := session.NewBridge(t.Context(), "sess-backpressure", func(context.Context, string, uint32, uint32) (session.Terminal, error) { return pty, nil }, session.TerminalOptions{QueueSize: 2048})
	t.Cleanup(func() { bridge.Close() })
	probe := &terminalRPCProbe{bridge: bridge, done: make(chan error, 1)}
	started, completed := new(atomic.Int64), new(atomic.Int64)
	rpcServer := connect.NewServer(func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			return next(ctx, spec, &observedSend{ServerStream: stream, started: started, completed: completed})
		}
	})
	rpc.RegisterShikiDaemonHandler(rpcServer, probe)
	server := httptest.NewUnstartedServer(connection.NewRPCHandler(rpcServer, terminalTransport))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	endpoint, err := connection.NewTLSClient(server.URL, connection.TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint)), pty, probe, started, completed
}
func openProbeTerminal(t *testing.T, ctx context.Context, client rpc.ShikiDaemonClient) rpc.ShikiDaemonTerminalStreamClientStream {
	t.Helper()
	stream, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess-backpressure", ReadOnly: true, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil || ready.GetEvent().GetType() != pb.SessionEvent_READY {
		t.Fatalf("READY: %v %v", ready, err)
	}
	return stream
}
func TestTerminalInputFailureInterruptsBackpressuredSend(t *testing.T) {
	client, pty, probe, started, completed := terminalRPCFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	stream := openProbeTerminal(t, ctx, client)
	defer stream.Close()
	produced := make(chan error, 1)
	go func() {
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for range 512 {
			if _, err := pty.writer.Write(chunk); err != nil {
				produced <- err
				return
			}
		}
		produced <- nil
	}()
	select {
	case err := <-produced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("PTY source did not finish filling the output queue")
	}
	// Observe a Send that remains outstanding while the client intentionally
	// does not read. The queue is large enough that it has not overflowed.
	deadline := time.Now().Add(4 * time.Second)
	var candidate int64
	var since time.Time
	blocked := false
	for time.Now().Before(deadline) {
		begun, ended := started.Load(), completed.Load()
		if begun > ended {
			if candidate != begun {
				candidate = begun
				since = time.Now()
			} else if time.Since(since) > 50*time.Millisecond {
				blocked = true
				break
			}
		} else {
			candidate = 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("test did not reach HTTP/2 Send backpressure")
	}
	if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("forbidden")}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-probe.done:
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("input failure cause lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("input failure left the sender blocked on HTTP/2 flow control")
	}
}
func TestTerminalBridgeFailurePreservesEndStreamStatus(t *testing.T) {
	client, pty, _, _, _ := terminalRPCFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream := openProbeTerminal(t, ctx, client)
	defer stream.Close()
	if err := pty.writer.CloseWithError(io.ErrUnexpectedEOF); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := stream.Receive()
		if err == nil {
			continue
		}
		if connect.CodeOf(err) != connect.CodeUnavailable {
			t.Fatalf("bridge failure lost its final Connect status: %v", err)
		}
		break
	}
}
