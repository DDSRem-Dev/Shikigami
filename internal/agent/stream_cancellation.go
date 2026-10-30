package agent

import (
	"context"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
)

type frontendOutputKey struct{}

// Allow an idle canceled call to flush its Connect terminal status, but bound
// that transport write too: its peer may have exhausted the receive window.
const frontendFinalStatusTimeout = time.Second

func bindFrontendCancellation(ctx context.Context, output *frontendOutput) func() {
	stop := context.AfterFunc(ctx, func() {
		cancelRead(ctx)
		if output != nil {
			output.cancel()
		}
	})
	return func() {
		stop()
		// Done may wake the handler before AfterFunc starts. Stopping that
		// callback must not leave the final status write unbounded. The
		// caller defers Registry cleanup until after this cancellation check.
		if ctx.Err() != nil && output != nil {
			output.cancel()
		}
	}
}

// frontendOutput owns only this HTTP response. Canceling a derived frontend
// context does not cancel net/http's request context, so a flow-controlled Send
// needs an explicit stream write deadline. Idle calls can still write their
// normal Canceled terminal status after the handler returns.
type frontendOutput struct {
	mu         sync.Mutex
	controller *http.ResponseController
	canceled   bool
	closed     bool
}

func (o *frontendOutput) cancel() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.canceled || o.closed {
		return
	}
	o.canceled = true
	// Being inside Send does not imply a blocked peer. Give a healthy
	// reader time to receive the current message and Canceled status,
	// while bounding both a blocked Send and the final status flush.
	_ = o.controller.SetWriteDeadline(time.Now().Add(frontendFinalStatusTimeout))
}

func (o *frontendOutput) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.controller = nil
}

func (o *frontendOutput) send(ctx context.Context, send func() error) error {
	o.mu.Lock()
	if err := ctx.Err(); err != nil {
		o.mu.Unlock()
		return err
	}
	if o.canceled || o.closed {
		o.mu.Unlock()
		return context.Canceled
	}
	o.mu.Unlock()
	return send()
}

type frontendOutputStream struct {
	connect.ServerStream
	ctx    context.Context
	output *frontendOutput
}

func (s *frontendOutputStream) Send(message any) error {
	return s.output.send(s.ctx, func() error { return s.ServerStream.Send(message) })
}

func (s *frontendOutputStream) SendHeaders() error {
	return s.output.send(s.ctx, s.ServerStream.SendHeaders)
}
