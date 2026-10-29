package agent

import (
	"connectrpc.com/connect/v2"
	"context"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func (s *Service) Shutdown(ctx context.Context, in *agentpb.ShutdownRequest) (*agentpb.AgentStatus, error) {
	if in.GetExpectedInstanceId() != s.Registry.InstanceID() {
		return nil, rpcError(connect.CodeFailedPrecondition, "AGENT_RESTARTED")
	}
	if in.GetRequestId() == "" {
		return nil, rpcError(connect.CodeInvalidArgument, "REQUEST_ID_REQUIRED")
	}
	if r := s.resources.Load(); r != nil && !r.faulted.Load() {
		r.mu.Lock()
		_, err := r.mutate("Shutdown", in.RequestId, in, &pb.Empty{}, func(*storage.Snapshot) error { return nil })
		r.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	s.stateMu.Lock()
	s.state = "stopping"
	s.stateMu.Unlock()
	return s.GetStatus(ctx, &pb.Empty{})
}
func (s *Service) setState(state, reason string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.state = state
	s.reason = reason
}
