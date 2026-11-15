package plugin

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ExtensionCapability is an explicit typed remote-method allowlist. New methods
// cannot inherit access merely by appearing in generated service metadata.
func ExtensionCapability(procedure string) string {
	switch procedure {
	case erpc.RegionServiceInspectRegionProcedure, erpc.RegionServicePlanRegionProcedure,
		erpc.RegionServiceApplyRegionProcedure, erpc.RegionServiceConfirmRegionProcedure,
		erpc.RegionServiceRollbackRegionProcedure, erpc.RegionServiceGetRegionOperationProcedure,
		erpc.RegionServiceDiagnoseRegionProcedure:
		return "region.host.v1"
	case erpc.BrowserServiceListBrowserProfilesProcedure, erpc.BrowserServicePutBrowserProfileProcedure,
		erpc.BrowserServiceDeleteBrowserProfileProcedure, erpc.BrowserServiceStartBrowserProcedure,
		erpc.BrowserServiceStopBrowserProcedure, erpc.BrowserServiceGetBrowserStatusProcedure,
		erpc.BrowserServiceTakeBrowserControlProcedure, erpc.BrowserServiceBrowserStreamProcedure,
		erpc.BrowserServiceListBrowserFilesProcedure, erpc.BrowserServiceReadBrowserFileProcedure,
		erpc.BrowserServiceWriteBrowserFileProcedure, erpc.BrowserServiceDiagnoseBrowserProcedure:
		return "browser.remote.v1"
	default:
		return ""
	}
}

// RegisterExtensionProxy retains the generated schemas and stream shapes; the
// implementation is a transport proxy, not an arbitrary plugin action tunnel.
func RegisterExtensionProxy(server *connect.Server, handler connect.ServerFunc) {
	metadata := connect.NewServer()
	erpc.RegisterRegionServiceHandler(metadata, erpc.UnimplementedRegionServiceHandler{})
	erpc.RegisterBrowserServiceHandler(metadata, erpc.UnimplementedBrowserServiceHandler{})
	for spec := range metadata.Specs() {
		if ExtensionCapability(spec.Procedure) != "" {
			server.Register(connect.Method{Spec: spec, Handler: handler})
		}
	}
}

func extensionMessage(descriptor protoreflect.MessageDescriptor) (proto.Message, error) {
	message, err := protoregistry.GlobalTypes.FindMessageByName(descriptor.FullName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "EXTENSION_MESSAGE_UNAVAILABLE")
	}
	return message.New().Interface(), nil
}

// ProxyExtension forwards one generated typed extension method. Both pumps are
// joined on cancellation, including an idle upstream body and blocked h2 output.
// closeRead must interrupt Receive on the inbound transport.
func ProxyExtension(ctx context.Context, spec connect.Spec, upstream connect.ServerStream, client *connect.Client, closeRead func()) (result error) {
	if ExtensionCapability(spec.Procedure) == "" || client == nil || closeRead == nil {
		return connect.NewError(connect.CodePermissionDenied, "EXTENSION_ROUTE_FORBIDDEN")
	}
	descriptor, ok := spec.Schema.(protoreflect.MethodDescriptor)
	if !ok {
		return connect.NewError(connect.CodeInternal, "EXTENSION_SCHEMA_REQUIRED")
	}
	request, err := extensionMessage(descriptor.Input())
	if err != nil {
		return err
	}
	if err = upstream.Receive(request); err != nil {
		return connection.RebuildError(err)
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	call, _ = connect.NewClientContext(call)
	if spec.StreamType == connect.StreamTypeUnary {
		response, err := extensionMessage(descriptor.Output())
		if err != nil {
			return err
		}
		if err = client.CallUnary(call, spec, request, response); err != nil {
			return connection.RebuildError(err)
		}
		return upstream.Send(response)
	}
	var downstream connect.ClientStream
	if spec.StreamType == connect.StreamTypeServer {
		downstream, err = client.CallServerStream(call, spec, request)
	} else {
		downstream, err = client.CallClientStream(call, spec)
	}
	if err != nil {
		return connection.RebuildError(err)
	}
	defer downstream.Close()
	var input chan error
	if spec.StreamType != connect.StreamTypeServer {
		if err = downstream.Send(request); err != nil {
			return connection.FinalSendError(call, cancel, err, func() error {
				response, makeErr := extensionMessage(descriptor.Output())
				if makeErr != nil {
					return makeErr
				}
				return downstream.Receive(response)
			})
		}
		input = make(chan error, 1)
		go func() {
			for {
				message, err := extensionMessage(descriptor.Input())
				if err == nil {
					err = upstream.Receive(message)
				}
				if errors.Is(err, io.EOF) {
					input <- downstream.CloseSend()
					return
				}
				if err == nil {
					err = downstream.Send(message)
				}
				if err != nil {
					input <- err
					if !errors.Is(err, io.EOF) {
						cancel()
						_ = downstream.Close()
					}
					return
				}
			}
		}()
		defer func() {
			cancel()
			closeRead()
			_ = downstream.Close()
			inputErr := <-input
			if inputErr != nil && !errors.Is(inputErr, io.EOF) && !errors.Is(inputErr, context.Canceled) && (result == nil || connect.CodeOf(result) == connect.CodeCanceled) {
				result = connection.RebuildError(inputErr)
			}
		}()
	}
	for {
		response, err := extensionMessage(descriptor.Output())
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
		if spec.StreamType == connect.StreamTypeClient {
			// Connect client-stream Receive consumes the single response and
			// terminal status together. Pool-bound streams release their call
			// here; another Receive would read an already-canceled transport.
			return nil
		}
	}
}

// RemoteExtensions is installed only on a client plugin's private callback
// socket. Resolve is host-owned and selects an existing stored server route.
// The per-instance interceptor checks every request frame before this proxy.
type RemoteExtensions struct {
	Resolve func(context.Context, string) (*connect.Client, error)
}

func (r *RemoteExtensions) Register(server *connect.Server) {
	RegisterExtensionProxy(server, func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		return r.proxy(ctx, spec, stream)
	})
}

func (r *RemoteExtensions) proxy(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
	if r.Resolve == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "EXTENSION_SERVER_UNAVAILABLE")
	}
	descriptor, ok := spec.Schema.(protoreflect.MethodDescriptor)
	if !ok {
		return connect.NewError(connect.CodeInternal, "EXTENSION_SCHEMA_REQUIRED")
	}
	first, err := extensionMessage(descriptor.Input())
	if err != nil {
		return err
	}
	if err = stream.Receive(first); err != nil {
		return err
	}
	field := first.ProtoReflect().Descriptor().Fields().ByName("server_id")
	if field == nil || first.ProtoReflect().Get(field).String() == "" {
		return connect.NewError(connect.CodeInvalidArgument, "EXTENSION_SERVER_REQUIRED")
	}
	client, err := r.Resolve(ctx, first.ProtoReflect().Get(field).String())
	if err != nil {
		return connection.RebuildError(err)
	}
	return ProxyExtension(ctx, spec, &extensionReplay{ServerStream: stream, first: first}, client, func() { closeInput(ctx) })
}

type extensionReplay struct {
	connect.ServerStream
	first proto.Message
}

func (s *extensionReplay) Receive(message any) error {
	if s.first == nil {
		return s.ServerStream.Receive(message)
	}
	proto.Merge(message.(proto.Message), s.first)
	s.first = nil
	return nil
}
