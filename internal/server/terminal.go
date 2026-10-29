package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type terminalControlKey struct{}
type terminalIO struct {
	closeRead    func()
	abort        func()
	readDeadline func(time.Time) error
}

func terminalTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var once sync.Once
		closeRead := func() { once.Do(func() { _ = r.Body.Close() }) }
		controller := http.NewResponseController(w)
		controls := terminalIO{closeRead: closeRead, readDeadline: controller.SetReadDeadline, abort: func() {
			_ = controller.SetReadDeadline(time.Now())
			_ = controller.SetWriteDeadline(time.Now())
			closeRead()
		}}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), terminalControlKey{}, controls)))
	})
}

func (s *Service) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, "verified identity required")
	}
	transport, ok := ctx.Value(terminalControlKey{}).(terminalIO)
	if !ok {
		return connect.NewError(connect.CodeInternal, "terminal transport controls required")
	}
	if err := transport.readDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	first, err := stream.Receive()
	if clearErr := transport.readDeadline(time.Time{}); err == nil {
		err = clearErr
	}
	if err != nil {
		return err
	}
	if first.GetOpen() == nil {
		return connect.NewError(connect.CodeInvalidArgument, "first terminal frame must be open")
	}
	bridge, err := s.Sessions.Terminal(first.GetOpen().SessionId)
	if err != nil {
		return err
	}
	subscription, err := bridge.Open(ctx, first.GetOpen(), claims.HasPermission("session.write"))
	if err != nil {
		return err
	}
	return serveTerminal(ctx, stream, bridge, subscription, transport)
}

func serveTerminal(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream, bridge *session.Bridge, subscription *session.Subscription, transport terminalIO) error {
	defer subscription.Close()
	pumpCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	inputDone := make(chan error, 1)
	go func() {
		for {
			input, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				inputDone <- nil
				return
			}
			if err == nil {
				err = bridge.Input(subscription.ID, input)
			}
			if err != nil {
				inputDone <- err
				cancel(err)
				return
			}
		}
	}()
	// Either the input pump or the bridge can terminate an attachment while
	// its sender is blocked by HTTP/2 flow control. Give a responsive sender
	// time to return a proper EndStream error; abort only if it cannot finish.
	watchDone := make(chan struct{})
	watchStop := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-subscription.Done():
		case <-pumpCtx.Done():
		case <-watchStop:
			return
		}
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-watchStop:
		case <-timer.C:
			transport.abort()
		}
	}()
	defer func() { close(watchStop); <-watchDone; cancel(nil); transport.closeRead(); <-inputDone }()
	for {
		output, err := subscription.Next(pumpCtx)
		if err != nil {
			if cause := context.Cause(pumpCtx); cause != nil {
				return cause
			}
			return err
		}
		if err := stream.Send(output); err != nil {
			if cause := context.Cause(pumpCtx); cause != nil {
				return cause
			}
			if cause := subscription.Err(); cause != nil {
				return cause
			}
			return err
		}
	}
}

func (s *Service) TakeControl(ctx context.Context, request *pb.TakeControlRequest) (*pb.Empty, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "takeover request required")
	}
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "verified identity required")
	}
	bridge, err := s.Sessions.Terminal(request.SessionId)
	if err != nil {
		return nil, err
	}
	if err := bridge.TakeControl(request.AttachmentId, request.ExpectedControlRevision, request.RequestId, claims.HasPermission("session.control")); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}
