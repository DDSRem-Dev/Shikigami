package agent

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Counting the public Context.AfterFunc registrations exposes retained request
// lifetimes deterministically, without GC timing or inspecting runtime internals.
// A private Done channel and Background parent prevent the context package from
// bypassing this hook through an embedded cancelCtx.
type countedPoolContext struct {
	context.Context
	mu      sync.Mutex
	done    chan struct{}
	changed chan struct{}
	err     error
	hooks   map[*int]func()
}

func newCountedPool(t *testing.T) (*remotePool, *countedPoolContext) {
	t.Helper()
	c := &countedPoolContext{Context: context.Background(), done: make(chan struct{}), changed: make(chan struct{}), hooks: map[*int]func(){}}
	p := &remotePool{ctx: c, cancel: c.cancel}
	p.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.FileCapability, "terminal.v1"}})
	t.Cleanup(c.cancel)
	return p, c
}
func (c *countedPoolContext) Done() <-chan struct{} { return c.done }
func (c *countedPoolContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
func (c *countedPoolContext) notify() { close(c.changed); c.changed = make(chan struct{}) }
func (c *countedPoolContext) AfterFunc(fn func()) func() bool {
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
func (c *countedPoolContext) cancel() {
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
func (c *countedPoolContext) awaitCount(t *testing.T, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		c.mu.Lock()
		got, changed := len(c.hooks), c.changed
		c.mu.Unlock()
		if got == want {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatalf("pool lifetime retains %d callbacks, want %d", got, want)
		}
	}
}

type lifetimeStream struct {
	closes atomic.Int32
	result error
}

func (*lifetimeStream) SendHeaders() error  { return nil }
func (*lifetimeStream) Send(any) error      { return nil }
func (*lifetimeStream) CloseSend() error    { return nil }
func (s *lifetimeStream) Receive(any) error { return s.result }
func (s *lifetimeStream) Close() error      { s.closes.Add(1); return nil }

func TestRemoteStreamReleasesPoolLifetime(t *testing.T) {
	for _, mode := range []string{"client-success", "caller-canceled", "EOF", "application-error", "explicit-close", "construction-error"} {
		t.Run(mode, func(t *testing.T) {
			pool, lifetime := newCountedPool(t)
			caller, cancel := context.WithCancel(t.Context())
			defer cancel()
			probe := &lifetimeStream{}
			stream, err := pool.bind(func(context.Context, connect.Spec) (connect.ClientStream, error) {
				if mode == "construction-error" {
					return nil, connect.NewError(connect.CodePermissionDenied, "DENIED")
				}
				return probe, nil
			})(caller, connect.Spec{StreamType: connect.StreamTypeClient})
			if mode == "construction-error" {
				if connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = stream.Close() })
				lifetime.awaitCount(t, 1)
				switch mode {
				case "caller-canceled":
					cancel()
				case "explicit-close":
					if err := stream.Close(); err != nil {
						t.Fatal(err)
					}
				default:
					if mode == "EOF" {
						probe.result = io.EOF
					}
					if mode == "application-error" {
						probe.result = connect.NewError(connect.CodeDataLoss, "REMOTE_FINAL_FAILURE")
					}
					if err := stream.CloseSend(); err != nil {
						t.Fatal(err)
					}
					if err := stream.Receive(nil); err != probe.result {
						t.Fatal("terminal result changed", err)
					}
				}
			}
			lifetime.awaitCount(t, 0)
			if pool.bad.Load() || lifetime.Err() != nil {
				t.Fatal("one request revoked the shared pool")
			}
			if mode != "caller-canceled" && mode != "construction-error" && probe.closes.Load() != 1 {
				t.Fatal("terminal stream not closed exactly once", probe.closes.Load())
			}
		})
	}
}

func TestRemoteStreamingMessageDoesNotEndLifetime(t *testing.T) {
	for _, kind := range []connect.StreamType{connect.StreamTypeServer, connect.StreamTypeBidi} {
		t.Run(kind.String(), func(t *testing.T) {
			pool, lifetime := newCountedPool(t)
			probe := &lifetimeStream{}
			var child context.Context
			stream, err := pool.bind(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				child = ctx
				return probe, nil
			})(t.Context(), connect.Spec{StreamType: kind})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if err := stream.CloseSend(); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err := stream.Receive(nil); err != nil {
					t.Fatal(err)
				}
				lifetime.awaitCount(t, 1)
				if probe.closes.Load() != 0 || pool.bad.Load() || lifetime.Err() != nil || child.Err() != nil {
					t.Fatal("ordinary response closed stream or pool")
				}
			}
			probe.result = io.EOF
			if err := stream.Receive(nil); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			lifetime.awaitCount(t, 0)
			// A separate active stream must still follow the pool lifetime,
			// including after request half-close and a successful response.
			probe = &lifetimeStream{}
			active, err := pool.bind(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				child = ctx
				return probe, nil
			})(t.Context(), connect.Spec{StreamType: kind})
			if err != nil {
				t.Fatal(err)
			}
			defer active.Close()
			if err := active.CloseSend(); err != nil {
				t.Fatal(err)
			}
			if err := active.Receive(nil); err != nil {
				t.Fatal(err)
			}
			pool.invalidate()
			select {
			case <-child.Done():
			case <-time.After(time.Second):
				t.Fatal("active stream detached from pool lifetime")
			}
			lifetime.awaitCount(t, 0)
		})
	}
}

type blockedLifetimeStream struct {
	lifetimeStream
	ctx                         context.Context
	sendEntered, receiveEntered chan struct{}
}

func (s *blockedLifetimeStream) Send(any) error {
	close(s.sendEntered)
	<-s.ctx.Done()
	return s.ctx.Err()
}
func (s *blockedLifetimeStream) Receive(any) error {
	close(s.receiveEntered)
	<-s.ctx.Done()
	return s.ctx.Err()
}

func TestRemoteStreamConcurrentCancellationAndClose(t *testing.T) {
	for range 32 {
		pool, lifetime := newCountedPool(t)
		caller, cancel := context.WithCancel(t.Context())
		defer cancel()
		probe := &blockedLifetimeStream{sendEntered: make(chan struct{}), receiveEntered: make(chan struct{})}
		stream, err := pool.bind(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
			probe.ctx = ctx
			return probe, nil
		})(caller, connect.Spec{StreamType: connect.StreamTypeBidi})
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 2)
		go func() { finished <- stream.Send(nil) }()
		go func() { finished <- stream.Receive(nil) }()
		<-probe.sendEntered
		<-probe.receiveEntered
		var closing sync.WaitGroup
		for range 8 {
			closing.Go(func() { _ = stream.Close() })
			closing.Go(cancel)
		}
		closing.Wait()
		for range 2 {
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation result changed", err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked stream survived cancellation")
			}
		}
		lifetime.awaitCount(t, 0)
		if probe.closes.Load() != 1 || pool.bad.Load() || lifetime.Err() != nil {
			t.Fatal("concurrent close damaged stream/pool lifetime")
		}
	}
}

func TestRemoteStreamCancellationDuringConstruction(t *testing.T) {
	for _, succeed := range []bool{false, true} {
		pool, lifetime := newCountedPool(t)
		caller, cancel := context.WithCancel(t.Context())
		stream, err := pool.bind(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
			cancel()
			<-ctx.Done()
			if !succeed {
				return nil, ctx.Err()
			}
			return &lifetimeStream{}, nil
		})(caller, connect.Spec{StreamType: connect.StreamTypeClient})
		if succeed {
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
		} else if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatal("construction cancellation changed", err)
		}
		lifetime.awaitCount(t, 0)
		if pool.bad.Load() || lifetime.Err() != nil {
			t.Fatal("construction cancellation revoked shared pool")
		}
	}
}

type lifetimeUploadHandler struct {
	rpc.UnimplementedShikiDaemonHandler
	final error
}

func (h *lifetimeUploadHandler) WriteFile(_ context.Context, stream rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	for {
		_, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if h.final != nil {
		return nil, h.final
	}
	return &pb.FileInfo{Path: "completed.txt"}, nil
}

func (*lifetimeUploadHandler) TerminalStream(_ context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	for {
		input, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: input.GetData()}}); err != nil {
			return err
		}
	}
}

func TestGeneratedUploadReleasesPoolLifetime(t *testing.T) {
	for _, mode := range []string{"success", "remote-error", "caller-canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			pool, lifetime := newCountedPool(t)
			handler := &lifetimeUploadHandler{}
			if mode == "remote-error" {
				handler.final = connect.NewError(connect.CodeDataLoss, "REMOTE_FINAL_FAILURE")
			}
			server := connect.NewServer()
			rpc.RegisterShikiDaemonHandler(server, handler)
			httpServer := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
			httpServer.EnableHTTP2 = true
			httpServer.StartTLS()
			defer httpServer.Close()
			roots := x509.NewCertPool()
			roots.AddCert(httpServer.Certificate())
			endpoint, err := connection.NewTLSClient(httpServer.URL, connection.TLSOptions{RootCAs: roots})
			if err != nil {
				t.Fatal(err)
			}
			defer endpoint.Close()
			base := connection.NewRPCClient(endpoint)
			client := rpc.NewShikiDaemonClient(connect.NewClient(clientTransport{client: base}, pool.bind))
			terminal, err := client.TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			exchange := func() {
				t.Helper()
				if err := terminal.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("same-live-terminal")}}); err != nil {
					t.Fatal(err)
				}
				output, err := terminal.Receive()
				if err != nil || string(output.GetData()) != "same-live-terminal" {
					t.Fatal("upload affected sibling terminal", output, err)
				}
			}
			exchange()
			lifetime.awaitCount(t, 1)
			for range 8 {
				call, stopCall := context.WithCancel(ctx)
				stream, err := client.WriteFile(call)
				if err != nil {
					stopCall()
					t.Fatal(err)
				}
				if err := stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("payload")}}); err != nil {
					t.Fatal(err)
				}
				if mode == "caller-canceled" {
					// Abandon the generated client-stream without an explicit Close
					// or final Receive. Its context must release the pool callback.
					stopCall()
				} else {
					result, err := stream.CloseAndReceive()
					if mode == "success" {
						if err != nil || result.Path != "completed.txt" {
							t.Fatal("successful upload lost terminal response", result, err)
						}
					} else if connect.CodeOf(err) != connect.CodeDataLoss {
						t.Fatal("terminal application error changed", err)
					}
				}
				// Generated client-streams expose no Close. A completed call must
				// release its pool registration even while the caller stays alive.
				lifetime.awaitCount(t, 1) // Only the unrelated terminal remains.
				stopCall()
				if pool.bad.Load() || lifetime.Err() != nil {
					t.Fatal("completed upload revoked shared pool")
				}
				exchange()
			}
			if err := terminal.Close(); err != nil {
				t.Fatal(err)
			}
			lifetime.awaitCount(t, 0)
		})
	}
}
