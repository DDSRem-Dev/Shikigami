//go:build darwin || linux

package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// Hold only the real response's terminal envelope. This exposes the interval
// in which HTTP/2 has closed the request writer but Receive has not yet returned
// its authoritative final status. All framing, transport and PTY I/O remain real.
func TestTerminalSendClosurePreservesReceiveStatus(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "clean-eof"
		if denied {
			name = "authentication-error"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			saved, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			terminal, err := openTerminalFD(master)
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			payload := append(bytes.Repeat([]byte("output"), 32<<10), []byte("FINAL-OUTPUT")...)
			probe := &frontendProbe{terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
				if _, err := stream.Receive(); err != nil {
					return err
				}
				if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "controller"}}}}); err != nil {
					return err
				}
				if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: payload}}); err != nil {
					return err
				}
				if denied {
					return connect.NewError(connect.CodeUnauthenticated, "revoked after output")
				}
				return nil
			}}
			client := frontendFixture(t, probe)()
			gate := &terminalFinalGate{next: client.endpoint.HTTPClient.Transport, seen: make(chan struct{}), allow: make(chan struct{}), requestClosed: make(chan struct{})}
			client.endpoint.HTTPClient.Transport = gate
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
			}()
			defer func() {
				cancel()
				gate.release()
				<-finished // Join before deferred PTY descriptors close, including assertion failures.
			}()
			select {
			case <-gate.seen:
			case <-ctx.Done():
				t.Fatal("real Connect terminal envelope did not arrive")
			}
			select {
			case <-gate.requestClosed:
			case <-ctx.Done():
				t.Fatal("HTTP transport did not close completed request writer")
			}
			// The received payload is larger than the undrained PTY's capacity,
			// so output cannot complete while this input reaches the closed sender.
			if err := terminal.Write(ctx, []byte("input-after-peer-completion")); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				t.Fatalf("Send closure preempted the unread final response: %v", err)
			case <-time.After(150 * time.Millisecond):
			}
			gate.release()
			if !denied {
				actual := make([]byte, 0, len(payload))
				buffer := make([]byte, 8192)
				for len(actual) < len(payload) {
					n, err := terminal.Read(ctx, buffer)
					actual = append(actual, buffer[:n]...)
					if err != nil {
						t.Fatal("drain clean EOF output", err)
					}
				}
				if !bytes.Equal(actual[:len(payload)], payload) {
					t.Fatal("clean EOF lost or changed terminal bytes")
				}
			}
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("terminal pumps did not join")
			}
			if denied && connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("final authentication status was lost: %v", err)
			}
			if !denied && err != nil {
				t.Fatalf("clean EOF became a sender failure: %v", err)
			}
			restored, err := term.GetState(int(slave.Fd()))
			if err != nil || !reflect.DeepEqual(saved, restored) {
				t.Fatal("terminal state was not restored", err)
			}
		})
	}
}

type terminalFinalGate struct {
	next          http.RoundTripper
	seen          chan struct{}
	allow         chan struct{}
	requestClosed chan struct{}
	releaseOnce   sync.Once
	closeOnce     sync.Once
}

func (g *terminalFinalGate) release() { g.releaseOnce.Do(func() { close(g.allow) }) }
func (g *terminalFinalGate) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != rpc.ShikiDaemonTerminalStreamProcedure {
		return g.next.RoundTrip(req)
	}
	req.Body = &terminalObservedRequest{ReadCloser: req.Body, closed: func() { g.closeOnce.Do(func() { close(g.requestClosed) }) }}
	response, err := g.next.RoundTrip(req)
	if err == nil {
		response.Body = &terminalGatedResponse{ReadCloser: response.Body, ctx: req.Context(), gate: g}
	}
	return response, err
}

type terminalObservedRequest struct {
	io.ReadCloser
	closed func()
}

func (r *terminalObservedRequest) Close() error {
	err := r.ReadCloser.Close()
	r.closed()
	return err
}

type terminalGatedResponse struct {
	io.ReadCloser
	ctx     context.Context
	gate    *terminalFinalGate
	pending []byte
}

func (r *terminalGatedResponse) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		header := make([]byte, 5)
		if _, err := io.ReadFull(r.ReadCloser, header); err != nil {
			return 0, err
		}
		size := binary.BigEndian.Uint32(header[1:])
		if size > 1<<20 {
			return 0, errors.New("test response exceeded Connect message bound")
		}
		r.pending = append(header, make([]byte, size)...)
		if _, err := io.ReadFull(r.ReadCloser, r.pending[5:]); err != nil {
			return 0, err
		}
		if header[0]&2 != 0 {
			close(r.gate.seen)
			select {
			case <-r.gate.allow:
			case <-r.ctx.Done():
				return 0, r.ctx.Err()
			}
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
