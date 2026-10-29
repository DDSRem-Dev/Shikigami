package connection

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type cancellationProbe struct {
	ctx         context.Context
	streaming   bool
	entered     chan struct{}
	closed      chan struct{}
	once        sync.Once
	closes      atomic.Int32
	headers     atomic.Int32
	remoteError error
}

func (p *cancellationProbe) SendHeaders() error { p.headers.Add(1); return nil }
func (p *cancellationProbe) Send(any) error {
	close(p.entered)
	if p.streaming {
		<-p.closed
	} else {
		<-p.ctx.Done()
	}
	return p.ctx.Err()
}
func (p *cancellationProbe) CloseSend() error { return nil }
func (p *cancellationProbe) Close() error {
	p.once.Do(func() { p.closes.Add(1); close(p.closed) })
	return nil
}
func (p *cancellationProbe) Receive(any) error {
	if p.remoteError != nil {
		return p.remoteError
	}
	<-p.closed
	return errors.New("transport stopped")
}

func TestCancellationUnblocksBothDirectionsAndClosesExactlyOnce(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "unary", true: "duplex"}[streaming], func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			probe := &cancellationProbe{streaming: streaming, entered: make(chan struct{}), closed: make(chan struct{})}
			next := func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				probe.ctx = ctx
				return probe, nil
			}
			kind := connect.StreamTypeUnary
			if streaming {
				kind = connect.StreamTypeBidi
			}
			stream, err := closeOnCancellation(next)(parent, connect.Spec{StreamType: kind})
			if err != nil {
				t.Fatal(err)
			}
			sendDone, receiveDone := make(chan error, 1), make(chan error, 1)
			go func() { sendDone <- stream.Send(nil) }()
			go func() { receiveDone <- stream.Receive(nil) }()
			<-probe.entered
			cancel()
			select {
			case <-sendDone:
			case <-time.After(time.Second):
				t.Fatal("blocked Send survived cancellation")
			}
			select {
			case err := <-receiveDone:
				if connect.CodeOf(err) != connect.CodeCanceled {
					t.Fatal("cancellation cause lost", err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked Receive survived cancellation")
			}
			var closeWG sync.WaitGroup
			for range 4 {
				closeWG.Go(func() { _ = stream.Close() })
			}
			closeWG.Wait()
			if probe.closes.Load() != 1 {
				t.Fatal("stream closed repeatedly")
			}
			if streaming && probe.headers.Load() != 1 {
				t.Fatal("duplex headers not initialized before concurrent close")
			}
		})
	}
}
func TestRemoteTerminalFailureSurvivesCleanupCancellation(t *testing.T) {
	remote := connect.NewError(connect.CodeDataLoss, "remote end-stream failure")
	probe := &cancellationProbe{entered: make(chan struct{}), closed: make(chan struct{}), remoteError: remote}
	stream, err := closeOnCancellation(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
		probe.ctx = ctx
		return probe, nil
	})(t.Context(), connect.Spec{StreamType: connect.StreamTypeBidi})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Receive(nil); connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatal("transport cleanup hid remote failure", err)
	}
	if probe.closes.Load() != 1 {
		t.Fatal("terminal response leaked stream")
	}
}
