package connection

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// A public AfterFunc hook makes the caller's retained cancellation registrations
// countable, without private runtime introspection or GC timing. The caller
// remains live until after completion assertions; cleanup cannot create a pass.
type ownedCaller struct {
	context.Context
	mu      sync.Mutex
	done    chan struct{}
	changed chan struct{}
	err     error
	hooks   map[*int]func()
}

func newOwnedCaller(t *testing.T) *ownedCaller {
	t.Helper()
	// Keep CallInfo inside the counted context so Connect does not hide its
	// AfterFunc hook behind a new valueCtx before entering the interceptor.
	base, _ := connect.NewClientContext(context.Background())
	c := &ownedCaller{Context: base, done: make(chan struct{}), changed: make(chan struct{}), hooks: map[*int]func(){}}
	t.Cleanup(c.cancel)
	return c
}
func (c *ownedCaller) Done() <-chan struct{} { return c.done }
func (c *ownedCaller) Err() error            { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *ownedCaller) notify()               { close(c.changed); c.changed = make(chan struct{}) }
func (c *ownedCaller) AfterFunc(fn func()) func() bool {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		go fn()
		return func() bool { return false }
	}
	id := new(int)
	c.hooks[id] = fn
	c.notify()
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.hooks[id]; !ok {
			return false
		}
		delete(c.hooks, id)
		c.notify()
		return true
	}
}
func (c *ownedCaller) cancel() {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = context.Canceled
	close(c.done)
	hooks := c.hooks
	c.hooks = map[*int]func(){}
	c.notify()
	c.mu.Unlock()
	for _, fn := range hooks {
		go fn()
	}
}
func (c *ownedCaller) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.hooks) }
func (c *ownedCaller) await(t *testing.T, want int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		c.mu.Lock()
		got, changed := len(c.hooks), c.changed
		c.mu.Unlock()
		if got == want {
			return
		}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatalf("caller retains %d cancellation registrations; want %d", got, want)
		}
	}
}

type ownedStreamProbe struct {
	ctx         context.Context
	result      error
	headerError error
	closes      atomic.Int32
	done        chan struct{}
}

func (p *ownedStreamProbe) Send(any) error     { return nil }
func (p *ownedStreamProbe) SendHeaders() error { return p.headerError }
func (p *ownedStreamProbe) CloseSend() error   { return nil }
func (p *ownedStreamProbe) Receive(any) error  { return p.result }
func (p *ownedStreamProbe) Close() error {
	if p.closes.Add(1) == 1 {
		close(p.done)
	}
	return nil
}

func TestCompletedClientStreamReleasesCallerOwnership(t *testing.T) {
	for _, mode := range []string{"success", "EOF", "remote-error", "explicit-close", "caller-canceled", "construction-error", "header-error"} {
		t.Run(mode, func(t *testing.T) {
			caller := newOwnedCaller(t)
			p := &ownedStreamProbe{done: make(chan struct{})}
			remote := connect.NewError(connect.CodeDataLoss, "TERMINAL_FAILURE")
			if mode == "EOF" {
				p.result = io.EOF
			}
			if mode == "remote-error" {
				p.result = remote
			}
			if mode == "header-error" {
				p.headerError = remote
			}
			stream, err := closeOnCancellation(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				p.ctx = ctx
				if mode == "construction-error" {
					return nil, remote
				}
				return p, nil
			})(caller, connect.Spec{StreamType: connect.StreamTypeClient})
			if mode == "construction-error" || mode == "header-error" {
				if !errors.Is(err, remote) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "explicit-close":
					err = stream.Close()
				case "caller-canceled":
					caller.cancel()
					select {
					case <-p.done:
					case <-time.After(time.Second):
						t.Fatal("cancellation did not close stream")
					}
				default:
					if err = stream.CloseSend(); err != nil {
						t.Fatal(err)
					}
					if caller.count() != 2 || p.ctx.Err() != nil {
						t.Fatal("request half-close ended response lifetime")
					}
					err = stream.Receive(nil)
					if !errors.Is(err, p.result) {
						t.Fatal("cleanup changed terminal outcome", err, p.result)
					}
				}
			}
			caller.await(t, 0)
			if mode != "caller-canceled" && caller.Err() != nil {
				t.Fatal("stream cleanup canceled its caller")
			}
			if p.ctx.Err() != context.Canceled {
				t.Fatal("owned transport child was not detached")
			}
			want := int32(1)
			if mode == "construction-error" {
				want = 0
			}
			if p.closes.Load() != want {
				t.Fatal("incorrect underlying close count", p.closes.Load(), want)
			}
		})
	}
}

func TestResponseMessagesAndHalfCloseRetainOpenStream(t *testing.T) {
	for _, kind := range []connect.StreamType{connect.StreamTypeServer, connect.StreamTypeBidi} {
		t.Run(kind.String(), func(t *testing.T) {
			caller := newOwnedCaller(t)
			p := &ownedStreamProbe{done: make(chan struct{})}
			stream, err := closeOnCancellation(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) { p.ctx = ctx; return p, nil })(caller, connect.Spec{StreamType: kind})
			if err != nil {
				t.Fatal(err)
			}
			if err = stream.CloseSend(); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err = stream.Receive(nil); err != nil {
					t.Fatal(err)
				}
				if p.closes.Load() != 0 || p.ctx.Err() != nil || caller.count() != 2 {
					t.Fatal("successful message ended live stream")
				}
			}
			p.result = io.EOF
			if err = stream.Receive(nil); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			caller.await(t, 0)
			if caller.Err() != nil || p.closes.Load() != 1 {
				t.Fatal("terminal EOF corrupted caller or failed cleanup")
			}
		})
	}
}

// The real generated client has no Close method on its upload stream. Its
// CloseAndReceive must release wrappers after final success with caller alive.
type ownershipUpload struct {
	rpc.UnimplementedShikiDaemonHandler
	final error
	calls atomic.Int32
}

func (h *ownershipUpload) WriteFile(ctx context.Context, stream rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, "CALL_INFO_REQUIRED")
	}
	info.ResponseHeader().Set("shiki-owner-response", "ack")
	info.ResponseTrailer().Set("shiki-owner-terminal", "done")
	for {
		_, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			h.calls.Add(1)
			return &pb.FileInfo{Path: "completed.txt"}, h.final
		}
		if err != nil {
			return nil, err
		}
	}
}
func TestGeneratedUploadReleasesCallerOwnership(t *testing.T) {
	for _, mode := range []string{"success", "remote-error"} {
		t.Run(mode, func(t *testing.T) {
			caller := newOwnedCaller(t)
			handler := &ownershipUpload{}
			if mode == "remote-error" {
				handler.final = connect.NewError(connect.CodeDataLoss, "REMOTE_FINAL_FAILURE")
			}
			dir, err := os.MkdirTemp("/tmp", "shiki-stream-owner-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "rpc.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(socket, 0600); err != nil {
				listener.Close()
				t.Fatal(err)
			}
			server := connect.NewServer()
			rpc.RegisterShikiDaemonHandler(server, handler)
			mux := http.NewServeMux()
			connecthttp.Mount(mux, server, connecthttp.WithCompressors())
			protocols := new(http.Protocols)
			protocols.SetUnencryptedHTTP2(true)
			var http2 atomic.Bool
			httpServer := &http.Server{Protocols: protocols, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ProtoMajor == 2 {
					http2.Store(true)
				}
				mux.ServeHTTP(w, r)
			})}
			done := make(chan error, 1)
			go func() { done <- httpServer.Serve(listener) }()
			t.Cleanup(func() { httpServer.Close(); <-done })
			endpoint, err := NewUnixClient(socket, uint32(os.Getuid()), "shiki-owner")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(endpoint.Close)
			client := rpc.NewShikiDaemonClient(NewRPCClient(endpoint))
			for range 4 {
				stream, err := client.WriteFile(caller)
				if err != nil {
					t.Fatal(err)
				}
				if err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("exact-payload")}}); err != nil {
					t.Fatal(err)
				}
				response, err := stream.CloseAndReceive()
				if mode == "success" {
					if err != nil || response.GetPath() != "completed.txt" {
						t.Fatal("lost terminal response", response, err)
					}
				} else {
					var remote *connect.Error
					if !errors.As(err, &remote) || !remote.IsRemote() || remote.Code() != connect.CodeDataLoss {
						t.Fatal("lost remote terminal failure", err)
					}
				}
				info, ok := connect.CallInfoForClientContext(caller)
				if !ok || info.ResponseHeader().Get("shiki-owner-response") != "ack" || info.ResponseTrailer().Get("shiki-owner-terminal") != "done" {
					t.Fatal("terminal cleanup lost response metadata")
				}
				caller.await(t, 0)
				if caller.Err() != nil {
					t.Fatal("completed upload canceled caller")
				}
			}
			if handler.calls.Load() != 4 || !http2.Load() {
				t.Fatal("real HTTP2 uploads did not complete")
			}
		})
	}
}
