package plugin

import (
	"context"
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/proto"
)

// Policy is an explicit per-instance callback capability and exact scope list.
// Empty fields are not wildcards. A global scope must be granted explicitly.
type Policy struct {
	Side         string
	Capabilities []string
	Scopes       []*ppb.Scope
	// AuthorizeScope validates live host-owned resources. It supplements exact
	// static scopes; it cannot bypass the method capability or side checks.
	AuthorizeScope func(context.Context, *ppb.Scope) error
}

var callbackPermissions = map[string]string{
	prpc.CoreAPIGetProjectProcedure: "core.projects.read", prpc.CoreAPIListProjectsProcedure: "core.projects.read",
	prpc.CoreAPIGetSessionProcedure: "core.sessions.read", prpc.CoreAPIListSessionsProcedure: "core.sessions.read",
	prpc.CoreAPIGetMappingProcedure: "core.mappings.read", prpc.CoreAPIListMappingsProcedure: "core.mappings.read",
	prpc.CoreAPISubscribeEventsProcedure: "core.events.read", prpc.CoreAPIPublishEventProcedure: "core.events.write",
	prpc.CoreAPILogProcedure: "core.log", prpc.CoreAPIGetConfigProcedure: "core.config.read", prpc.CoreAPISetConfigProcedure: "core.config.write",
	prpc.CoreAPIRemoteListFilesProcedure: "file.read", prpc.CoreAPIRemoteStatFileProcedure: "file.read", prpc.CoreAPIRemoteReadFileProcedure: "file.read",
	prpc.CoreAPIRemoteWriteFileProcedure: "file.write", prpc.CoreAPIRemoteDeleteFileProcedure: "file.write", prpc.CoreAPIRemoteWatchFilesProcedure: "file.watch",
	prpc.CoreAPIRemoteCheckPortProcedure: "port.forward", prpc.CoreAPIRemoteListPortsProcedure: "port.list", prpc.CoreAPIRemoteWatchPortsProcedure: "port.list", prpc.CoreAPIRemoteForwardPortProcedure: "port.forward",
}

type grant struct {
	mu                    sync.Mutex
	token, instance, host string
	deadline              time.Time
	valid                 bool
	policy                Policy
	lifetime              context.Context
	cancel                context.CancelFunc
}

func (g *grant) revoke() { g.mu.Lock(); g.valid = false; g.mu.Unlock(); g.cancel() }
func (g *grant) interceptor(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		info, ok := connect.CallInfoForServerContext(ctx)
		if !ok {
			return connect.NewError(connect.CodeUnauthenticated, "plugin authentication required")
		}
		headers := info.RequestHeader()
		single := func(key string) string {
			v := headers.Values(key)
			if len(v) != 1 {
				return ""
			}
			return v[0]
		}
		g.mu.Lock()
		allowed := g.valid && time.Now().Before(g.deadline) && single("shiki-plugin-instance-id") == g.instance && single("shiki-host-instance-id") == g.host && subtle.ConstantTimeCompare([]byte(single("Authorization")), []byte("Bearer "+g.token)) == 1
		g.mu.Unlock()
		if !allowed {
			return connect.NewError(connect.CodeUnauthenticated, "plugin instance expired or revoked")
		}
		capability, known := callbackPermissions[spec.Procedure]
		if !known {
			return connect.NewError(connect.CodePermissionDenied, "callback method is not permitted")
		}
		if g.policy.Side != "client" && strings.Contains(spec.Procedure, "/Remote") {
			return connect.NewError(connect.CodePermissionDenied, "remote callbacks require a client plugin")
		}
		permitted := false
		for _, c := range g.policy.Capabilities {
			if c == capability {
				permitted = true
				break
			}
		}
		if !permitted {
			return connect.NewError(connect.CodePermissionDenied, "callback capability is not granted")
		}
		scoped, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(g.lifetime, func() { cancel(); closeInput(ctx); abortOutput(ctx) })
		defer stop()
		return next(scoped, spec, &scopeStream{ServerStream: stream, grant: g, procedure: spec.Procedure, ctx: scoped})
	}
}

type scopeStream struct {
	connect.ServerStream
	ctx       context.Context
	grant     *grant
	procedure string
	opened    bool
}

func (s *scopeStream) Receive(msg any) error {
	if err := s.ServerStream.Receive(msg); err != nil {
		return err
	}
	var scope *ppb.Scope
	switch request := msg.(type) {
	case *ppb.Scope:
		scope = request
	case *ppb.ConfigRequest:
		scope = request.Scope
	case *ppb.SetConfigRequest:
		scope = request.Scope
	case *ppb.SubscribeRequest:
		scope = request.Scope
	case *ppb.PublishEventRequest:
		if request.Event == nil || request.ExpectedHostInstanceId != s.grant.host {
			return connect.NewError(connect.CodeFailedPrecondition, "plugin event belongs to an old host")
		}
		scope = request.Event.Scope
		request.Event.SourceInstanceId = s.grant.instance
	case *pb.LogRecord:
		// Logging is instance-only, not a scoped data access. The backend must
		// validate allowed fields and redact content before persisting it.
		return nil
	case *ppb.RemoteListFilesRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.ProjectId != scope.GetProjectId() {
			return scopeDenied()
		}
	case *ppb.RemoteStatFileRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.ProjectId != scope.GetProjectId() {
			return scopeDenied()
		}
	case *ppb.RemoteReadFileRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.ProjectId != scope.GetProjectId() {
			return scopeDenied()
		}
	case *ppb.RemoteDeleteFileRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.ProjectId != scope.GetProjectId() {
			return scopeDenied()
		}
	case *ppb.RemoteWatchFilesRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.ProjectId != scope.GetProjectId() {
			return scopeDenied()
		}
	case *ppb.RemoteCheckPortRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.SessionId != scope.GetSessionId() {
			return scopeDenied()
		}
	case *ppb.RemoteListPortsRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.SessionId != scope.GetSessionId() {
			return scopeDenied()
		}
	case *ppb.RemoteWatchPortsRequest:
		scope = request.Scope
		if request.Request == nil || request.Request.SessionId != scope.GetSessionId() {
			return scopeDenied()
		}
	case *ppb.RemoteWriteFrame:
		if !s.opened {
			open := request.GetOpen()
			if open == nil || open.Header == nil || open.Header.ProjectId != open.Scope.GetProjectId() {
				return scopeDenied()
			}
			scope = open.Scope
			s.opened = true
		} else {
			if _, ok := request.Payload.(*ppb.RemoteWriteFrame_Chunk); !ok {
				return connect.NewError(connect.CodeInvalidArgument, "upload chunk required")
			}
			if len(request.GetChunk()) > connection.MaxDataBytes {
				return connect.NewError(connect.CodeResourceExhausted, "file chunk too large")
			}
			return nil
		}
	case *ppb.RemotePortFrame:
		if !s.opened {
			open := request.GetOpen()
			if open == nil || open.Connect == nil || open.Connect.Type != pb.PortData_CONNECT {
				return scopeDenied()
			}
			scope = open.Scope
			s.opened = true
		} else {
			if request.GetData() == nil {
				return connect.NewError(connect.CodeInvalidArgument, "invalid port data frame")
			}
			if len(request.GetData().Data) > connection.MaxDataBytes {
				return connect.NewError(connect.CodeResourceExhausted, "port chunk too large")
			}
			return nil
		}
	default:
		return connect.NewError(connect.CodePermissionDenied, "unsupported callback message")
	}
	if scope == nil {
		return scopeDenied()
	}
	for _, allowed := range s.grant.policy.Scopes {
		if proto.Equal(scope, allowed) {
			return nil
		}
	}
	if s.grant.policy.AuthorizeScope != nil {
		return s.grant.policy.AuthorizeScope(s.ctx, proto.Clone(scope).(*ppb.Scope))
	}
	return scopeDenied()
}
func scopeDenied() error {
	return connect.NewError(connect.CodePermissionDenied, "plugin scope is not granted")
}
