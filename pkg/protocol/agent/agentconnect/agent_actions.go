// Non-generated file. Provides the AgentActions Connect service on
// /shiki.agent.v1.AgentActions for plugin action invocations from the CLI.
// When protoc is available, fold InvokePluginAction into agent.proto and
// regenerate. The interceptor in internal/agent/ipc.go explicitly allows
// this service prefix alongside the main Agent service.
package agentconnect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

const (
	// AgentActionsName is the fully-qualified service name.
	AgentActionsName = "shiki.agent.v1.AgentActions"

	// AgentActionsInvokePluginActionProcedure is the full procedure path.
	AgentActionsInvokePluginActionProcedure = "/shiki.agent.v1.AgentActions/InvokePluginAction"
)

// AgentActionsClient is the client for plugin action invocation.
// InvokePluginAction carries a structpb.Struct whose JSON shape is:
//
//	{
//	  "plugin_id":  "sync",
//	  "side":       "local",          // "local" | "remote"
//	  "server_id":  "srv_xxx",        // required when side="remote"
//	  "action_id":  "list-conflicts",
//	  "invocation_id": "...",
//	  "flags":      { "json": true },
//	  "input_json": ""
//	}
//
// The response Struct carries:
//
//	{
//	  "kind":   "text",        // text | form | json | notification
//	  "text":   "...",
//	  "form":   {...},
//	  "json":   {...},
//	  "notify": {...},
//	  "done":   true
//	}
type AgentActionsClient interface {
	InvokePluginAction(context.Context, *structpb.Struct) (*structpb.Struct, error)
}

// AgentActionsHandler is the server-side interface.
type AgentActionsHandler interface {
	InvokePluginAction(context.Context, *structpb.Struct) (*structpb.Struct, error)
}

// UnimplementedAgentActionsHandler returns CodeUnimplemented from all methods.
type UnimplementedAgentActionsHandler struct{}

func (UnimplementedAgentActionsHandler) InvokePluginAction(context.Context, *structpb.Struct) (*structpb.Struct, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "shiki.agent.v1.AgentActions.InvokePluginAction is not implemented")
}

// NewAgentActionsClient constructs a Connect client for AgentActions.
func NewAgentActionsClient(client *connect.Client) AgentActionsClient {
	return &agentActionsClient{client: client}
}

type agentActionsClient struct {
	client *connect.Client
}

func (c *agentActionsClient) InvokePluginAction(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	spec := agentActionsInvokePluginActionSpec()
	var res structpb.Struct
	if err := c.client.CallUnary(ctx, spec, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// RegisterAgentActionsHandler registers svc on a connect.Server.
func RegisterAgentActionsHandler(server *connect.Server, svc AgentActionsHandler) {
	adapter := agentActionsHandler{svc: svc}
	server.Register(
		connect.Method{Spec: agentActionsInvokePluginActionSpec(), Handler: adapter.invokePluginAction},
	)
}

type agentActionsHandler struct{ svc AgentActionsHandler }

func (h agentActionsHandler) invokePluginAction(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
	var req structpb.Struct
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.InvokePluginAction(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}

func agentActionsInvokePluginActionSpec() connect.Spec {
	return connect.Spec{
		StreamType: connect.StreamTypeUnary,
		Procedure:  AgentActionsInvokePluginActionProcedure,
		// Schema is intentionally nil; set when proto is regenerated.
	}
}
