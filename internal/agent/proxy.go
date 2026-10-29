package agent

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var proxyMethods = map[string]bool{
	rpc.ShikiDaemonListProjectDirectoriesProcedure: true, rpc.ShikiDaemonGetSessionLogsProcedure: true,
	rpc.ShikiDaemonListProjectsProcedure: true, rpc.ShikiDaemonCreateProjectProcedure: true,
	rpc.ShikiDaemonGetProjectProcedure: true, rpc.ShikiDaemonDeleteProjectProcedure: true,
	rpc.ShikiDaemonListProjectSessionsProcedure: true, rpc.ShikiDaemonListSessionsProcedure: true,
	rpc.ShikiDaemonCreateSessionProcedure: true, rpc.ShikiDaemonGetSessionProcedure: true, rpc.ShikiDaemonGetSessionScreenProcedure: true,
	rpc.ShikiDaemonUpdateSessionProcedure: true, rpc.ShikiDaemonDeleteSessionProcedure: true,
	rpc.ShikiDaemonTakeControlProcedure: true, rpc.ShikiDaemonStopClaudeProcedure: true,
	rpc.ShikiDaemonRestartClaudeProcedure: true, rpc.ShikiDaemonTerminalStreamProcedure: true,
	rpc.ShikiDaemonCreateSnapshotProcedure: true, rpc.ShikiDaemonListSnapshotsProcedure: true,
	rpc.ShikiDaemonRestoreSnapshotProcedure: true, rpc.ShikiDaemonDeleteSnapshotProcedure: true,
	rpc.ShikiDaemonListPortsProcedure: true, rpc.ShikiDaemonWatchPortsProcedure: true,
	rpc.ShikiDaemonGetSystemInfoProcedure: true, rpc.ShikiDaemonGetClaudeVersionProcedure: true,
	rpc.ShikiDaemonHealthCheckProcedure: true, rpc.ShikiDaemonGetStatsProcedure: true,
	rpc.ShikiDaemonWatchStatsProcedure: true,
}

func ProxyAllowed(procedure string) bool { return proxyMethods[procedure] }

func (s *Service) registerProxy(server *connect.Server) {
	// Use generated method metadata, retaining its exact schema and stream shape.
	// The explicit allowlist above determines access, including for future methods.
	metadata := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(metadata, rpc.UnimplementedShikiDaemonHandler{})
	for spec := range metadata.Specs() {
		server.Register(connect.Method{Spec: spec, Handler: s.proxy})
	}
}
func newMessage(descriptor protoreflect.MessageDescriptor) (proto.Message, error) {
	messageType, err := protoregistry.GlobalTypes.FindMessageByName(descriptor.FullName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "MESSAGE_TYPE_UNAVAILABLE")
	}
	return messageType.New().Interface(), nil
}
func (s *Service) proxy(ctx context.Context, spec connect.Spec, upstream connect.ServerStream) error {
	client, ok := ctx.Value(remoteClientKey{}).(*connect.Client)
	if !ok {
		return connect.NewError(connect.CodeFailedPrecondition, "SERVER_CLIENT_REQUIRED")
	}
	if spec.Procedure == rpc.ShikiDaemonTerminalStreamProcedure {
		return s.terminalProxy(ctx, spec, upstream, client)
	}
	descriptor, ok := spec.Schema.(protoreflect.MethodDescriptor)
	if !ok {
		return connect.NewError(connect.CodeInternal, "METHOD_SCHEMA_REQUIRED")
	}
	request, err := newMessage(descriptor.Input())
	if err != nil {
		return err
	}
	if err = upstream.Receive(request); err != nil {
		return err
	}
	if request, ok := request.(*pb.TakeControlRequest); ok && !s.Registry.OwnsAttachment(ctx, request.SessionId, request.AttachmentId) {
		return connect.NewError(connect.CodePermissionDenied, "ATTACHMENT_NOT_OWNED")
	}
	remoteCtx := FreshRemoteContext(ctx)
	switch spec.StreamType {
	case connect.StreamTypeUnary:
		response, err := newMessage(descriptor.Output())
		if err != nil {
			return err
		}
		if err = client.CallUnary(remoteCtx, spec, request, response); err != nil {
			return connection.RebuildError(err)
		}
		return upstream.Send(response)
	case connect.StreamTypeServer:
		downstream, err := client.CallServerStream(remoteCtx, spec, request)
		if err != nil {
			return connection.RebuildError(err)
		}
		defer downstream.Close()
		for {
			response, err := newMessage(descriptor.Output())
			if err != nil {
				return err
			}
			if err = downstream.Receive(response); errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return connection.RebuildError(err)
			}
			if err = upstream.Send(response); err != nil {
				return err
			}
		}
	default:
		return connect.NewError(connect.CodePermissionDenied, "PROXY_METHOD_FORBIDDEN")
	}
}

func (s *Service) terminalProxy(ctx context.Context, spec connect.Spec, upstream connect.ServerStream, client *connect.Client) (result error) {
	open := new(pb.TerminalInput)
	if err := upstream.Receive(open); err != nil {
		return err
	}
	if open.GetOpen() == nil || open.GetOpen().SessionId == "" {
		return connect.NewError(connect.CodeInvalidArgument, "TERMINAL_OPEN_REQUIRED")
	}
	sessionID := open.GetOpen().SessionId
	remoteCtx, cancel := context.WithCancel(FreshRemoteContext(ctx))
	defer cancel()
	downstream, err := client.CallClientStream(remoteCtx, spec)
	if err != nil {
		return connection.RebuildError(err)
	}
	defer downstream.Close()
	if err = downstream.Send(open); err != nil {
		return connection.FinalSendError(remoteCtx, cancel, err, func() error {
			return downstream.Receive(new(pb.TerminalOutput))
		})
	}
	ready := new(pb.TerminalOutput)
	if err = downstream.Receive(ready); err != nil {
		return connection.RebuildError(err)
	}
	if ready.SessionId != sessionID || ready.AttachmentId == "" || ready.GetEvent() == nil || ready.GetEvent().Type != pb.SessionEvent_READY {
		return connect.NewError(connect.CodeDataLoss, "TERMINAL_READY_INVALID")
	}
	remove, err := s.Registry.AddAttachment(ctx, sessionID, ready.AttachmentId)
	if err != nil {
		return err
	}
	defer remove()
	if err = upstream.Send(ready); err != nil {
		return err
	}
	inputDone := make(chan error, 1)
	go func() {
		for {
			input := new(pb.TerminalInput)
			err := upstream.Receive(input)
			if errors.Is(err, io.EOF) {
				inputDone <- downstream.CloseSend()
				return
			}
			if err == nil && input.GetOpen() != nil {
				err = connect.NewError(connect.CodeInvalidArgument, "TERMINAL_OPEN_REPEATED")
			}
			if err == nil {
				err = downstream.Send(input)
			}
			if err != nil {
				inputDone <- err
				cancel()
				return
			}
		}
	}()
	// Closing the inbound body wakes Receive, cancellation wakes downstream Send.
	// Both pumps finish before the handler returns; no request goroutine is orphaned.
	defer func() {
		cancel()
		cancelRead(ctx)
		_ = downstream.Close()
		inputErr := <-inputDone
		if connect.CodeOf(inputErr) == connect.CodeInvalidArgument {
			result = connection.RebuildError(inputErr)
		}
	}()
	for {
		output := new(pb.TerminalOutput)
		err = downstream.Receive(output)
		if err != nil && remoteCtx.Err() != nil {
			return connection.RebuildError(remoteCtx.Err())
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return connection.RebuildError(err)
		}
		if output.SessionId != sessionID || output.AttachmentId != ready.AttachmentId {
			return connect.NewError(connect.CodeDataLoss, "TERMINAL_ATTACHMENT_CHANGED")
		}
		if err = upstream.Send(output); err != nil {
			return err
		}
	}
}
