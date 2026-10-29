package hostrotation_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var responseTail = []byte("target-tail-after-client-EOF")

type echoTarget struct {
	listener    net.Listener
	mu          sync.Mutex
	connections []net.Conn
	received    [][]byte
	workers     sync.WaitGroup
	acceptDone  chan struct{}
}

func startEcho(t *testing.T) *echoTarget {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e := &echoTarget{listener: listener, acceptDone: make(chan struct{})}
	go func() {
		defer close(e.acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			e.mu.Lock()
			ordinal := len(e.connections)
			e.connections = append(e.connections, conn)
			e.received = append(e.received, nil)
			e.mu.Unlock()
			e.workers.Add(1)
			go func() {
				defer e.workers.Done()
				defer conn.Close()
				buffer := make([]byte, 32<<10)
				for {
					n, err := conn.Read(buffer)
					if n > 0 {
						e.mu.Lock()
						e.received[ordinal] = append(e.received[ordinal], buffer[:n]...)
						e.mu.Unlock()
						if _, writeErr := conn.Write(buffer[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						if errors.Is(err, io.EOF) {
							_, _ = conn.Write(responseTail)
						}
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-e.acceptDone
		e.mu.Lock()
		for _, conn := range e.connections {
			conn.Close()
		}
		e.mu.Unlock()
		e.workers.Wait()
	})
	return e
}

func (e *echoTarget) port() uint32 { return uint32(e.listener.Addr().(*net.TCPAddr).Port) }
func (e *echoTarget) transcripts() [][]byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([][]byte, len(e.received))
	for i, data := range e.received {
		out[i] = append([]byte(nil), data...)
	}
	return out
}

type portWork struct {
	target          *echoTarget
	active, stopped *ppb.ForwardSpec
	old             *net.TCPConn
	oldPayload      []byte
	stopRequest     string
}

func (h *harness) configurePorts(target *echoTarget) {
	values, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(target.port())}, "idle_timeout_seconds": 120})
	if err != nil {
		h.t.Fatal(err)
	}
	h.manage("server", "port", pb.ManagePluginRequest_SET_CONFIG, values)
	values, err = structpb.NewStruct(map[string]any{"idle_timeout_seconds": 120})
	if err != nil {
		h.t.Fatal(err)
	}
	h.manage("client", "port", pb.ManagePluginRequest_SET_CONFIG, values)
}

func (h *harness) dial(port uint32) *net.TCPConn {
	h.t.Helper()
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(50 * time.Second)); err != nil {
		h.t.Fatal(err)
	}
	return conn.(*net.TCPConn)
}

func (h *harness) preparePort(target *echoTarget) *portWork {
	w := &portWork{target: target, oldPayload: []byte("old-acknowledged-payload-must-not-replay")}
	create := func() *ppb.ForwardState {
		h.t.Helper()
		forward, err := h.local.CreateForward(h.ctx, &apb.CreateForwardRequest{RequestId: h.id(), Spec: &ppb.ForwardSpec{ServerId: h.serverID, RemotePort: target.port(), Enabled: true}})
		if err != nil || forward.GetState() != "listening" || forward.GetBoundPort() == 0 {
			h.t.Fatal("forward not listening", forward, err)
		}
		return forward
	}
	// Completed manual stop removes its spec and retains the durable request
	// receipt. A new forward to the same endpoint gets a distinct host-owned ID.
	stopped := create()
	w.stopped = proto.Clone(stopped.Spec).(*ppb.ForwardSpec)
	w.stopRequest = h.id()
	if _, err := h.local.StopForward(h.ctx, &apb.StopForwardRequest{ForwardId: stopped.Spec.Id, ExpectedRevision: stopped.Spec.Revision, RequestId: w.stopRequest}); err != nil {
		h.t.Fatal(err)
	}
	active := create()
	w.active = proto.Clone(active.Spec).(*ppb.ForwardSpec)
	w.old = h.dial(active.BoundPort)
	if _, err := w.old.Write(w.oldPayload); err != nil {
		h.t.Fatal(err)
	}
	got := make([]byte, len(w.oldPayload))
	if _, err := io.ReadFull(w.old, got); err != nil || !bytes.Equal(got, w.oldPayload) {
		h.t.Fatal("initial TCP echo", err)
	}
	h.await("active forward accounting", func(ctx context.Context) error {
		list, err := h.local.ListForwards(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if len(list.Forwards) != 1 || list.Forwards[0].Connections != 1 {
			return errors.New("active connection not accounted")
		}
		return nil
	})
	return w
}

func (h *harness) verifyPort(w *portWork) {
	h.t.Helper()
	w.old.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := w.old.Read(make([]byte, 1)); err == nil {
		h.t.Fatal("old TCP survived rotation")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		h.t.Fatal("old TCP did not terminate", err)
	}
	w.old.Close()
	var restored *ppb.ForwardState
	h.await("persistent listener recovery", func(ctx context.Context) error {
		list, err := h.local.ListForwards(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if len(list.Forwards) != 1 {
			return errors.New("enabled intent missing or stopped intent resurrected")
		}
		state := list.Forwards[0]
		if !proto.Equal(state.Spec, w.active) {
			return errors.New("desired forward changed")
		}
		if state.State != "listening" || state.BoundPort == 0 || state.Connections != 0 {
			return fmt.Errorf("forward %s/%s", state.State, state.ReasonCode)
		}
		restored = state
		return nil
	})
	if got := w.target.transcripts(); len(got) != 1 || !bytes.Equal(got[0], w.oldPayload) {
		h.t.Fatalf("old request replayed before new TCP: connections=%d", len(got))
	}
	fresh := h.dial(restored.BoundPort)
	fresh.SetDeadline(time.Now().Add(8 * time.Second))
	payload := bytes.Repeat([]byte("fresh-payload\x00"), 32<<10)
	sent := make(chan error, 1)
	go func() {
		_, err := fresh.Write(payload)
		if err == nil {
			err = fresh.CloseWrite()
		}
		sent <- err
	}()
	received, err := io.ReadAll(fresh)
	if err != nil {
		h.t.Fatal("fresh TCP receive", err)
	}
	if err := <-sent; err != nil {
		h.t.Fatal("fresh TCP send", err)
	}
	fresh.Close()
	if !bytes.Equal(received, append(append([]byte(nil), payload...), responseTail...)) {
		h.t.Fatal("new TCP half-close lost echo or post-EOF tail")
	}
	transcripts := w.target.transcripts()
	if len(transcripts) != 2 || !bytes.Equal(transcripts[0], w.oldPayload) || !bytes.Equal(transcripts[1], payload) {
		h.t.Fatal("old data replay or unexpected target connection")
	}
	h.t.Logf("%s rotation: old_tcp_closed=true target_connections=2 replay=false new_bytes=%d half_close_tail_verified=true stopped_intent_preserved=true", h.side, len(payload))
}
