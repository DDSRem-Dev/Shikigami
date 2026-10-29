package agent

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	InstanceHeader = "Shiki-Agent-Instance-Id"
	FrontendHeader = "Shiki-Frontend-Id"
	ServerHeader   = "Shiki-Server-Id"
)

type remoteClientKey struct{}
type readCloserKey struct{}

// ResolveClient resolves an explicit stored server ID to its already trusted,
// authenticated client. It must not accept URLs or credentials from the frontend.
type ResolveClient func(context.Context, string) (*connect.Client, error)

type Service struct {
	agentrpc.UnimplementedAgentHandler
	Registry        *Registry
	Events          *Events
	uid             uint32
	build           string
	resolve         ResolveClient
	resources       atomic.Pointer[Resources]
	plugins         atomic.Pointer[host.Supervisor]
	handlerMu       sync.Mutex
	handlers        sync.WaitGroup
	admissionClosed bool
	stateMu         sync.Mutex
	state           string
	reason          string
	shutdown        func()
	startup         *startupObserver
}

func NewService(ctx context.Context, uid uint32, build string, resolve ResolveClient) *Service {
	registry := NewRegistry(ctx)
	return &Service{Registry: registry, Events: NewEvents(registry.InstanceID()), uid: uid, build: build, resolve: resolve}
}
func (s *Service) Handshake(ctx context.Context, in *agentpb.HandshakeRequest) (*agentpb.HandshakeResponse, error) {
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.control.v1", "agent.events.v1", "agent.startup.v1"}}, in.GetProtocol(), s.build)
	if err != nil {
		return nil, err
	}
	id, err := s.Registry.registerCapabilities(agreement.Capabilities)
	if err != nil {
		return nil, err
	}
	status, _ := s.GetStatus(ctx, &pb.Empty{})
	return &agentpb.HandshakeResponse{Protocol: agreement, InstanceId: s.Registry.InstanceID(), FrontendId: id, Status: status}, nil
}
func (s *Service) GetStatus(ctx context.Context, _ *pb.Empty) (*agentpb.AgentStatus, error) {
	s.stateMu.Lock()
	state := s.state
	reason := s.reason
	s.stateMu.Unlock()
	if state == "" {
		state = "ready"
	}
	return &agentpb.AgentStatus{InstanceId: s.Registry.InstanceID(), State: state, ReasonCode: reason, ActiveForwards: s.activeForwardTasks(ctx), ActiveTerminals: uint32(s.Registry.TerminalCount()), ActiveSyncTasks: s.activeSyncTasks()}, nil
}
func (s *Service) ReleaseFrontend(ctx context.Context, _ *pb.Empty) (*pb.Empty, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "HANDSHAKE_REQUIRED")
	}
	s.Registry.Release(identity.FrontendID)
	return &pb.Empty{}, nil
}
func (s *Service) WatchEvents(ctx context.Context, _ *pb.Empty, out agentrpc.AgentWatchEventsServerStream) error {
	subscription, stop := s.Events.subscribe()
	defer stop()
	for {
		event, err := subscription.receive(ctx)
		if err != nil {
			return err
		}
		if err = out.Send(event); err != nil {
			return err
		}
	}
}

// RPCServer can also register NodeControl before mounting; that implementation
// must perform its independent node instance negotiation/checks.
func (s *Service) RPCServer() *connect.Server {
	server := connect.NewServer(s.Interceptor())
	agentrpc.RegisterAgentHandler(server, s)
	s.registerProxy(server)
	server.SetUnknownHandler(func(context.Context, connect.Spec, connect.ServerStream) error {
		return connect.NewError(connect.CodePermissionDenied, "IPC_METHOD_FORBIDDEN")
	})
	return server
}

// Handler preserves a way to interrupt request reads before joining proxy
// goroutines. Cancellation alone does not unblock an HTTP request-body read.
func (s *Service) Handler(server *connect.Server) http.Handler {
	handler := connection.NewRPCHandler(server, host.WithStreamCancellation)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handlerMu.Lock()
		if s.admissionClosed {
			s.handlerMu.Unlock()
			http.Error(w, "AGENT_STOPPING", http.StatusServiceUnavailable)
			return
		}
		s.handlers.Add(1)
		s.handlerMu.Unlock()
		defer s.handlers.Done()
		var once sync.Once
		closeRead := func() { once.Do(func() { _ = r.Body.Close() }) }
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), readCloserKey{}, closeRead)))
		if r.URL.Path == agentrpc.AgentShutdownProcedure {
			s.stateMu.Lock()
			stopping := s.state == "stopping"
			shutdown := s.shutdown
			s.stateMu.Unlock()
			if stopping && shutdown != nil {
				shutdown()
			}
		}
	})
}
func cancelRead(ctx context.Context) {
	if closeRead, ok := ctx.Value(readCloserKey{}).(func()); ok {
		closeRead()
	}
}

func singleHeader(header interface{ Values(string) []string }, key string) (string, error) {
	values := header.Values(key)
	if len(values) > 1 || (len(values) == 1 && (strings.TrimSpace(values[0]) != values[0] || strings.ContainsAny(values[0], ",\r\n"))) {
		return "", connect.NewError(connect.CodeInvalidArgument, "AMBIGUOUS_ROUTING_HEADER")
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}
func (s *Service) Interceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if uid, ok := connection.PeerUID(ctx); !ok || uid != s.uid {
				return connect.NewError(connect.CodePermissionDenied, "IPC_UID_REQUIRED")
			}
			info, ok := connect.CallInfoForServerContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeInternal, "CALL_INFO_REQUIRED")
			}
			header := info.RequestHeader()
			if len(header.Values("Authorization")) != 0 {
				return connect.NewError(connect.CodePermissionDenied, "FRONTEND_CREDENTIAL_FORBIDDEN")
			}
			// NodeControl is separately negotiated, including when the node is offline.
			if strings.HasPrefix(spec.Procedure, "/"+agentrpc.NodeControlName+"/") {
				return next(ctx, spec, stream)
			}
			if spec.Procedure == agentrpc.AgentHandshakeProcedure {
				return next(ctx, spec, stream)
			}
			instance, err := singleHeader(header, InstanceHeader)
			if err != nil {
				return err
			}
			frontend, err := singleHeader(header, FrontendHeader)
			if err != nil {
				return err
			}
			serverID, err := singleHeader(header, ServerHeader)
			if err != nil {
				return err
			}
			ctx, done, err := s.Registry.bind(ctx, instance, frontend, spec.Procedure != agentrpc.AgentReleaseFrontendProcedure)
			if err != nil {
				return err
			}
			defer done()
			boundContext := ctx
			stop := context.AfterFunc(boundContext, func() { cancelRead(boundContext) })
			defer stop()
			ctx = context.WithValue(ctx, identityKey{}, Identity{FrontendID: frontend, ServerID: serverID})
			if strings.HasPrefix(spec.Procedure, "/"+rpc.ShikiDaemonName+"/") {
				if !ProxyAllowed(spec.Procedure) {
					return connect.NewError(connect.CodePermissionDenied, "PROXY_METHOD_FORBIDDEN")
				}
				if serverID == "" {
					return connect.NewError(connect.CodeInvalidArgument, "SERVER_ID_REQUIRED")
				}
				s.stateMu.Lock()
				resolve := s.resolve
				s.stateMu.Unlock()
				if resolve == nil {
					return connect.NewError(connect.CodeNotFound, "SERVER_NOT_FOUND")
				}
				client, err := resolve(ctx, serverID)
				if err != nil {
					return connection.RebuildError(err)
				}
				if client == nil {
					return connect.NewError(connect.CodeUnavailable, "SERVER_UNAVAILABLE")
				}
				ctx = context.WithValue(ctx, remoteClientKey{}, client)
			} else if !strings.HasPrefix(spec.Procedure, "/"+agentrpc.AgentName+"/") {
				return connect.NewError(connect.CodePermissionDenied, "IPC_METHOD_FORBIDDEN")
			} else if serverID != "" {
				stream = &routeCheckingStream{ServerStream: stream, serverID: serverID}
			}
			return next(ctx, spec, stream)
		}
	}
}

type routeCheckingStream struct {
	connect.ServerStream
	serverID string
}

func (s *routeCheckingStream) Receive(message any) error {
	if err := s.ServerStream.Receive(message); err != nil {
		return err
	}
	m, ok := message.(interface{ ProtoReflect() protoreflect.Message })
	if !ok {
		return connect.NewError(connect.CodeInternal, "MESSAGE_SCHEMA_REQUIRED")
	}
	ids := requestServerIDs(m.ProtoReflect())
	if len(ids) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, "UNEXPECTED_SERVER_HEADER")
	}
	for _, id := range ids {
		if id != s.serverID {
			return connect.NewError(connect.CodeInvalidArgument, "SERVER_ROUTE_MISMATCH")
		}
	}
	return nil
}
func requestServerIDs(message protoreflect.Message) []string {
	fields := message.Descriptor().Fields()
	if field := fields.ByName("server_id"); field != nil {
		return []string{message.Get(field).String()}
	}
	if string(message.Descriptor().FullName()) == "shiki.agent.v1.ServerConfig" {
		return []string{message.Get(fields.ByName("id")).String()}
	}
	for _, name := range []protoreflect.Name{"scope", "mapping", "spec", "target", "config"} {
		if field := fields.ByName(name); field != nil && field.Kind() == protoreflect.MessageKind && message.Has(field) {
			if ids := requestServerIDs(message.Get(field).Message()); len(ids) > 0 {
				return ids
			}
		}
	}
	return nil
}

// FreshRemoteContext never copies frontend headers or CallInfo. The trusted
// remote client's own interceptor supplies the stored server credential.
func FreshRemoteContext(ctx context.Context) context.Context {
	child, _ := connect.NewClientContext(ctx)
	return child
}
