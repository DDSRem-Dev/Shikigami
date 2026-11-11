// Code generated MANUALLY (not by protoc). This file provides the PluginActions
// gRPC service that extends the generated Plugin service with InvokeAction.
// When protoc is available, this should be folded back into plugin.proto and
// the generated plugin_grpc.pb.go. Until then, this non-generated file keeps
// the generated code untouched while providing the same wire format.
//
// The PluginActions service is registered alongside the Plugin service on
// the same gRPC server, using a distinct service name to avoid
// duplicate-service registration panics.
package plugin

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

const (
	// PluginActions_InvokeAction_FullMethodName is the wire method path for
	// the PluginActions service's InvokeAction method. Distinct from the
	// generated Plugin_*_FullMethodName constants to avoid conflicts.
	PluginActions_InvokeAction_FullMethodName = "/shiki.plugin.v1.PluginActions/InvokeAction"
)

// PluginActionsServer is the server-side interface for InvokeAction. It is
// satisfied by the SDK runtime struct, which checks at call time whether
// the underlying Implementation supports actions.
type PluginActionsServer interface {
	InvokeAction(context.Context, *structpb.Struct) (*structpb.Struct, error)
}

// UnimplementedPluginActionsServer returns CodeUnimplemented from all methods.
// Embed by value for forward compatibility.
type UnimplementedPluginActionsServer struct{}

func (UnimplementedPluginActionsServer) InvokeAction(context.Context, *structpb.Struct) (*structpb.Struct, error) {
	return nil, status.Error(codes.Unimplemented, "method InvokeAction not implemented")
}

// _Plugin_InvokeAction_Handler is the gRPC handler descriptor for InvokeAction.
func _Plugin_InvokeAction_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(structpb.Struct)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PluginActionsServer).InvokeAction(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: PluginActions_InvokeAction_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PluginActionsServer).InvokeAction(ctx, req.(*structpb.Struct))
	}
	return interceptor(ctx, in, info, handler)
}

// PluginActions_ServiceDesc is the gRPC ServiceDesc for the PluginActions
// service. Register it on the same grpc.Server as the Plugin service.
var PluginActions_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "shiki.plugin.v1.PluginActions",
	HandlerType: (*PluginActionsServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "InvokeAction", Handler: _Plugin_InvokeAction_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "plugin.proto",
}

// RegisterPluginActionsServer registers srv as the PluginActions service on s.
// The Plugin service must have already been registered via plugin_grpc.pb.go's
// RegisterPluginServer; this adds InvokeAction to the same process.
func RegisterPluginActionsServer(s grpc.ServiceRegistrar, srv PluginActionsServer) {
	s.RegisterService(&PluginActions_ServiceDesc, srv)
}

// PluginActionsClient is the client API for the InvokeAction RPC.
type PluginActionsClient interface {
	InvokeAction(ctx context.Context, in *structpb.Struct, opts ...grpc.CallOption) (*structpb.Struct, error)
}

type pluginActionsClient struct {
	cc grpc.ClientConnInterface
}

// NewPluginActionsClient constructs a client that calls InvokeAction on the
// shiki.plugin.v1.PluginActions service via the same grpc.ClientConn as the
// Plugin service.
func NewPluginActionsClient(cc grpc.ClientConnInterface) PluginActionsClient {
	return &pluginActionsClient{cc}
}

func (c *pluginActionsClient) InvokeAction(ctx context.Context, in *structpb.Struct, opts ...grpc.CallOption) (*structpb.Struct, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(structpb.Struct)
	err := c.cc.Invoke(ctx, PluginActions_InvokeAction_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}
