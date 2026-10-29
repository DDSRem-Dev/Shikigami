package tunnel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type wire struct {
	ctx           context.Context
	cancel        context.CancelFunc
	input, output chan *pb.PortData
	once          sync.Once
}

func (s *wire) Send(p *pb.PortData) error {
	select {
	case s.output <- p:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func (s *wire) Receive() (*pb.PortData, error) {
	select {
	case p, ok := <-s.input:
		if !ok {
			return nil, io.EOF
		}
		return p, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
func (s *wire) CloseSend() error { s.once.Do(func() { close(s.output) }); return nil }
func (s *wire) Close() error     { s.cancel(); return nil }
func wires(ctx context.Context) (*wire, *wire) {
	ctx, cancel := context.WithCancel(ctx)
	a, b := make(chan *pb.PortData), make(chan *pb.PortData)
	return &wire{ctx: ctx, cancel: cancel, input: a, output: b}, &wire{ctx: ctx, cancel: cancel, input: b, output: a}
}
func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	return client, server
}
func TestHalfClosePreservesLargeDelayedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app, local := tcpPair(t)
	remote, service := tcpPair(t)
	clientStream, serverStream := wires(ctx)
	body := bytes.Repeat([]byte("input"), 70000)
	response := bytes.Repeat([]byte("response"), 60000)
	serviceResult := make(chan error, 1)
	go func() {
		b, err := io.ReadAll(service)
		if err == nil && !bytes.Equal(b, body) {
			err = errors.New("wrong request bytes")
		}
		if err == nil {
			time.Sleep(25 * time.Millisecond)
			_, err = service.Write(response)
		}
		service.CloseWrite()
		serviceResult <- err
	}()
	serverResult := make(chan error, 1)
	clientResult := make(chan error, 1)
	go func() {
		err := Bridge(ctx, remote, serverStream, Options{ID: "connection", Idle: time.Second, Server: true})
		serverStream.CloseSend()
		serverResult <- err
	}()
	go func() {
		clientResult <- Bridge(ctx, local, clientStream, Options{ID: "connection", Idle: time.Second, CloseSend: clientStream.CloseSend, CloseStream: clientStream.Close})
	}()
	if _, err := app.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(app)
	if err != nil || !bytes.Equal(got, response) {
		t.Fatalf("response %d/%d err=%v", len(got), len(response), err)
	}
	for _, result := range []<-chan error{serviceResult, serverResult, clientResult} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestMalformedFramesAndIdleCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames []*pb.PortData
		code   connect.Code
	}{{"changed-id", []*pb.PortData{{Type: pb.PortData_DATA, ConnectionId: "other"}}, connect.CodeInvalidArgument}, {"changed-target", []*pb.PortData{{Type: pb.PortData_DATA, ConnectionId: "id", Port: 80}}, connect.CodeInvalidArgument}, {"oversize", []*pb.PortData{{Type: pb.PortData_DATA, ConnectionId: "id", Data: make([]byte, 256<<10+1)}}, connect.CodeResourceExhausted}, {"control-data", []*pb.PortData{{Type: pb.PortData_HALF_CLOSE, ConnectionId: "id", Data: []byte("bad")}}, connect.CodeInvalidArgument}, {"after-half", []*pb.PortData{{Type: pb.PortData_HALF_CLOSE, ConnectionId: "id"}, {Type: pb.PortData_DATA, ConnectionId: "id", Data: []byte("bad")}}, connect.CodeInvalidArgument}, {"idle", nil, connect.CodeDeadlineExceeded}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, _ := tcpPair(t)
			in, out := wires(ctx)
			done := make(chan error, 1)
			go func() {
				done <- Bridge(ctx, conn, in, Options{ID: "id", Idle: 40 * time.Millisecond, Server: true, CloseStream: in.Close})
			}()
			go func() {
				for _, p := range tc.frames {
					if err := out.Send(p); err != nil {
						return
					}
				}
			}()
			select {
			case err := <-done:
				if connect.CodeOf(err) != tc.code {
					t.Fatalf("got %v want %v", err, tc.code)
				}
			case <-ctx.Done():
				t.Fatal("bridge leaked")
			}
		})
	}
}
func TestHandshakeFramesAreStrict(t *testing.T) {
	if err := ConnectFrame(&pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "id", Port: 80, RemoteAddress: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*pb.PortData{nil, {Type: pb.PortData_DATA}, {Type: pb.PortData_CONNECT, ConnectionId: "id", Port: 80, Data: []byte("bad")}} {
		if ConnectFrame(p) == nil {
			t.Fatal("accepted invalid connect")
		}
	}
	if ConnectedFrame(&pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: "another"}, "id") == nil {
		t.Fatal("accepted wrong connection")
	}
}

type sequenceStream struct {
	frames   []*pb.PortData
	terminal error
}

func (s *sequenceStream) Send(*pb.PortData) error { return nil }
func (s *sequenceStream) Receive() (*pb.PortData, error) {
	if len(s.frames) == 0 {
		return nil, s.terminal
	}
	p := s.frames[0]
	s.frames = s.frames[1:]
	return p, nil
}
func TestClientConsumesFinalRPCStatusAfterClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		frames   []*pb.PortData
		terminal error
		code     connect.Code
	}{{"missing-close", []*pb.PortData{{Type: pb.PortData_HALF_CLOSE, ConnectionId: "id"}}, io.EOF, connect.CodeInvalidArgument}, {"late-error", []*pb.PortData{{Type: pb.PortData_HALF_CLOSE, ConnectionId: "id"}, {Type: pb.PortData_CLOSE, ConnectionId: "id"}}, connect.NewError(connect.CodePermissionDenied, "revoked"), connect.CodePermissionDenied}, {"trailing-frame", []*pb.PortData{{Type: pb.PortData_HALF_CLOSE, ConnectionId: "id"}, {Type: pb.PortData_CLOSE, ConnectionId: "id"}, {Type: pb.PortData_CLOSE, ConnectionId: "id"}}, io.EOF, connect.CodeInvalidArgument}} {
		t.Run(tc.name, func(t *testing.T) {
			conn, _ := tcpPair(t)
			err := receive(conn, &sequenceStream{frames: tc.frames, terminal: tc.terminal}, Options{ID: "id"}, func() {}, func(int) {})
			if connect.CodeOf(err) != tc.code {
				t.Fatal(err)
			}
		})
	}
}
