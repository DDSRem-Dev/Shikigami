package client

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

// This controlled public transport drives the generated client, not a real
// HTTP/2 peer. It makes otherwise scheduling-dependent terminal statuses exact.
type handshakeTransport struct {
	plans chan *handshakeStream
	calls atomic.Int32
}

func (p *handshakeTransport) NewClientStream(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
	p.calls.Add(1)
	if spec.Procedure != "/shiki.plugin.v1.CoreAPI/RemoteForwardPort" {
		return nil, errors.New("unexpected RPC")
	}
	select {
	case s := <-p.plans:
		s.ctx = ctx
		s.started = time.Now()
		close(s.opened)
		if s.mode == "open-resource" {
			return nil, handshakeResourceError()
		}
		return s, nil
	default:
		return nil, errors.New("unexpected retry or replay")
	}
}
func handshakeResourceError() error {
	// Deliberately unrelated to PORT_CONNECTION_LIMIT: classification is by code.
	return connect.NewError(connect.CodeResourceExhausted, "controlled metadata capacity exhausted")
}

type handshakeReply struct {
	frame *pb.PortData
	err   error
}
type handshakeStream struct {
	mode                       string
	ctx                        context.Context
	started                    time.Time
	opened, received, closed   chan struct{}
	release                    chan struct{}
	replies                    chan handshakeReply
	closeOnce, receiveOnce     sync.Once
	mu                         sync.Mutex
	id                         string
	opens, receives, dataSends int
	sentData                   []byte
	finalError                 error
	sendError                  error
}

func newHandshakeStream(mode string) *handshakeStream {
	return &handshakeStream{mode: mode, opened: make(chan struct{}), received: make(chan struct{}), closed: make(chan struct{}), release: make(chan struct{}), replies: make(chan handshakeReply, 16)}
}
func (s *handshakeStream) SendHeaders() error { return nil }
func (s *handshakeStream) CloseSend() error   { return nil }
func (s *handshakeStream) Close() error       { s.closeOnce.Do(func() { close(s.closed) }); return nil }
func (s *handshakeStream) sendFailure(err error) error {
	s.mu.Lock()
	s.sendError = err
	s.mu.Unlock()
	return err
}
func (s *handshakeStream) enqueue(p *pb.PortData) error {
	select {
	case s.replies <- handshakeReply{frame: p}:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.closed:
		return io.EOF
	}
}
func (s *handshakeStream) Send(msg any) error {
	frame, ok := msg.(*ppb.RemotePortFrame)
	if !ok {
		return errors.New("wrong generated request type")
	}
	if open := frame.GetOpen(); open != nil {
		s.mu.Lock()
		s.opens++
		s.id = open.GetConnect().GetConnectionId()
		id := s.id
		s.mu.Unlock()
		if id == "" || open.GetConnect().GetType() != pb.PortData_CONNECT {
			return errors.New("invalid initial CONNECT")
		}
		switch s.mode {
		case "send-resource":
			return s.sendFailure(handshakeResourceError())
		case "eof-resource", "eof-permission", "eof-connected", "eof-block":
			return s.sendFailure(io.EOF)
		case "receive-resource":
			return nil
		default:
			return s.enqueue(&pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: id})
		}
	}
	p := frame.GetData()
	if p == nil {
		return errors.New("missing data frame")
	}
	s.mu.Lock()
	id := s.id
	if p.Type == pb.PortData_DATA {
		s.dataSends++
		s.sentData = append(s.sentData, p.Data...)
	}
	s.mu.Unlock()
	if p.ConnectionId != id {
		return errors.New("connection ID changed")
	}
	switch p.Type {
	case pb.PortData_DATA:
		return s.enqueue(proto.Clone(p).(*pb.PortData))
	case pb.PortData_HALF_CLOSE:
		if err := s.enqueue(&pb.PortData{Type: pb.PortData_HALF_CLOSE, ConnectionId: id}); err != nil {
			return err
		}
		if err := s.enqueue(&pb.PortData{Type: pb.PortData_CLOSE, ConnectionId: id}); err != nil {
			return err
		}
		select {
		case s.replies <- handshakeReply{err: io.EOF}:
			return nil
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	default:
		return errors.New("unexpected control frame")
	}
}
func (s *handshakeStream) Receive(msg any) error {
	s.mu.Lock()
	s.receives++
	first := s.receives == 1
	id := s.id
	s.mu.Unlock()
	s.receiveOnce.Do(func() { close(s.received) })
	var terminal error
	if first {
		switch s.mode {
		case "receive-resource", "eof-resource":
			terminal = handshakeResourceError()
		case "eof-permission":
			terminal = connect.NewError(connect.CodePermissionDenied, "controlled denial")
		case "eof-connected":
			proto.Merge(msg.(*pb.PortData), &pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: id})
			return nil
		case "eof-block":
			<-s.ctx.Done()
			terminal = s.ctx.Err()
		}
	}
	if terminal != nil {
		s.mu.Lock()
		s.finalError = terminal
		s.mu.Unlock()
		return terminal
	}
	select {
	case <-s.release:
		terminal = handshakeResourceError()
	case r := <-s.replies:
		if r.err != nil {
			return r.err
		}
		proto.Merge(msg.(*pb.PortData), r.frame)
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.closed:
		return io.EOF
	}
	s.mu.Lock()
	s.finalError = terminal
	s.mu.Unlock()
	return terminal
}
func handshakeSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(12 * time.Second):
		t.Fatal("controlled stream did not reach its barrier")
	}
}
func handshakeEcho(t *testing.T, c *net.TCPConn, payload string) {
	t.Helper()
	if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(c, payload); err != nil {
		t.Fatal("existing TCP stream lost its write path", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != payload {
		t.Fatalf("echo got %q: %v", got, err)
	}
	if err := c.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}
func handshakeClosedTCP(t *testing.T, c *net.TCPConn) {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := c.Read(make([]byte, 1))
	if n != 0 || err == nil {
		t.Fatal("rejected/failed TCP connection remained usable", n, err)
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatal("connection only ended at test deadline", err)
	}
}
func handshakeState(t *testing.T, w *Worker, condition func(*ppb.ForwardState) bool) *ppb.ForwardState {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		list, err := w.ListForwards(t.Context())
		if err != nil || len(list.GetForwards()) != 1 {
			t.Fatal("forward snapshot", list, err)
		}
		state := list.Forwards[0]
		if condition(state) {
			return state
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("forward did not settle: %v", state)
		}
	}
}
