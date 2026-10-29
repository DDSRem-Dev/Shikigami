package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
)

func admissionContextError(ctx context.Context, prefix string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, prefix+"_REQUEST_DEADLINE")
	}
	if ctx.Err() != nil {
		return connect.NewError(connect.CodeCanceled, prefix+"_REQUEST_CANCELED")
	}
	return nil
}

// Install after Auth.Interceptor so denied callers never acquire a slot or
// receive a budget diagnostic. The generated handler reads its first request
// inside next, including unary and server-streaming methods.
func admissionInterceptor(budget func(string) (*atomic.Int32, int32), timeout time.Duration, prefix string) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			active, maximum := budget(spec.Procedure)
			if active == nil {
				return next(ctx, spec, stream)
			}
			if err := admissionContextError(ctx, prefix); err != nil {
				return err
			}
			for {
				used := active.Load()
				if used >= maximum {
					return connect.NewError(connect.CodeResourceExhausted, prefix+"_RPC_CONCURRENCY_LIMIT")
				}
				if active.CompareAndSwap(used, used+1) {
					break
				}
			}
			defer active.Add(-1)
			if err := admissionContextError(ctx, prefix); err != nil {
				return err
			}
			control, ok := ctx.Value(requestReadKey{}).(*requestReadControl)
			if !ok {
				return connect.NewError(connect.CodeInternal, prefix+"_TRANSPORT_CONTROL_REQUIRED")
			}
			deadline := time.Now().Add(timeout)
			if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
				deadline = parent
			}
			if err := control.set(deadline); err != nil {
				return connect.NewError(connect.CodeInternal, prefix+"_TRANSPORT_DEADLINE_UNAVAILABLE")
			}
			// Cancellation must interrupt an actual blocked HTTP body Receive.
			// Join the callback before the handler can release its transport;
			// no helper goroutine waits indefinitely for a first message.
			finished := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				defer close(finished)
				control.abort()
			})
			defer func() {
				if !stop() {
					<-finished
				}
			}()
			return next(ctx, spec, &firstRequestReceive{ServerStream: stream, ctx: ctx, control: control, prefix: prefix})
		}
	}
}

type requestReadKey struct{}
type requestReadControl struct {
	mu           sync.Mutex
	finished     bool
	aborted      bool
	readDeadline func(time.Time) error
	closeRead    func() error
}

func firstRequestTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		control := &requestReadControl{readDeadline: controller.SetReadDeadline, closeRead: r.Body.Close}
		defer func() { control.mu.Lock(); control.finished = true; control.mu.Unlock() }()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestReadKey{}, control)))
	})
}

func (c *requestReadControl) set(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished || c.aborted {
		return context.Canceled
	}
	return c.readDeadline(deadline)
}

func (c *requestReadControl) abort() {
	c.mu.Lock()
	if c.finished {
		c.mu.Unlock()
		return
	}
	c.aborted = true
	_ = c.readDeadline(time.Now())
	c.mu.Unlock()
	_ = c.closeRead()
}

type firstRequestReceive struct {
	connect.ServerStream
	ctx      context.Context
	control  *requestReadControl
	received bool
	prefix   string
}

func (s *firstRequestReceive) Receive(message any) error {
	if s.received {
		return s.ServerStream.Receive(message)
	}
	err := s.ServerStream.Receive(message)
	if contextErr := admissionContextError(s.ctx, s.prefix); contextErr != nil {
		return contextErr
	}
	if err != nil {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return connect.NewError(connect.CodeDeadlineExceeded, s.prefix+"_FIRST_FRAME_DEADLINE")
		}
		return err
	}
	if err = s.control.set(time.Time{}); err != nil {
		if contextErr := admissionContextError(s.ctx, s.prefix); contextErr != nil {
			return contextErr
		}
		return connect.NewError(connect.CodeInternal, s.prefix+"_TRANSPORT_DEADLINE_UNAVAILABLE")
	}
	s.received = true
	return nil
}
