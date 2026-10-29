package plugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type cancelReadKey struct{}
type outputControlKey struct{}
type outputControl struct {
	mu         sync.Mutex
	closed     bool
	controller *http.ResponseController
}

func (c *outputControl) abort() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		_ = c.controller.SetWriteDeadline(time.Now())
	}
}
func abortOutput(ctx context.Context) {
	if control, ok := ctx.Value(outputControlKey{}).(*outputControl); ok {
		control.abort()
	}
}

// WithStreamCancellation binds the HTTP request reader to proxy cleanup. A
// downstream failure must wake a pump blocked reading an otherwise idle client.
func WithStreamCancellation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		control := &outputControl{controller: http.NewResponseController(w)}
		defer func() { control.mu.Lock(); control.closed = true; control.mu.Unlock() }()
		ctx := context.WithValue(r.Context(), outputControlKey{}, control)
		ctx = context.WithValue(ctx, cancelReadKey{}, func() { _ = r.Body.Close() })
		stop := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
		defer stop()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func closeInput(ctx context.Context) {
	if close, ok := ctx.Value(cancelReadKey{}).(func()); ok {
		close()
	}
}

type portDownstream interface {
	Send(*pb.PortData) error
	Receive() (*pb.PortData, error)
	CloseSend() error
	Close() error
}

// bridgePort keeps each direction single-writer, preserves normal half-close,
// waits for the downstream RPC terminal status, and joins its request pump.
func bridgePort(ctx context.Context, cancel context.CancelFunc, receive func() (*pb.PortData, error), send func(*pb.PortData) error, downstream portDownstream) error {
	if _, ok := ctx.Value(cancelReadKey{}).(func()); !ok {
		return connect.NewError(connect.CodeFailedPrecondition, "stream cancellation middleware is required")
	}
	input := make(chan error, 1)
	go func() {
		for {
			message, err := receive()
			if errors.Is(err, io.EOF) {
				input <- downstream.CloseSend()
				return
			}
			if err != nil {
				input <- err
				cancel()
				_ = downstream.Close()
				return
			}
			if err = downstream.Send(message); err != nil {
				input <- err
				// gRPC Send may yield EOF before Receive exposes a business error.
				if !errors.Is(err, io.EOF) {
					cancel()
					_ = downstream.Close()
				}
				return
			}
		}
	}()
	var outputErr error
	for {
		message, err := downstream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			outputErr = err
			break
		}
		if err = send(message); err != nil {
			outputErr = err
			break
		}
	}
	cancel()
	_ = downstream.Close()
	closeInput(ctx)
	inputErr := <-input
	if connect.CodeOf(outputErr) == connect.CodeCanceled && inputErr != nil && !errors.Is(inputErr, io.EOF) && !errors.Is(inputErr, context.Canceled) {
		return connection.RebuildError(inputErr)
	}
	return connection.RebuildError(outputErr)
}
