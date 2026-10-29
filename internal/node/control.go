package node

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/proto"
	"tailscale.com/ipn"
)

// Control is registered only on an owner-verified local Unix listener. Its
// handshake is independent from Agent, so it remains usable before login.
type Control struct {
	agentrpc.UnimplementedNodeControlHandler
	node  *Node
	uid   uint32
	build string
}

func NewControl(node *Node, uid uint32, build string) *Control {
	return &Control{node: node, uid: uid, build: build}
}
func (c *Control) authorize(ctx context.Context, negotiate bool) error {
	if uid, ok := connection.PeerUID(ctx); !ok || uid != c.uid {
		return connect.NewError(connect.CodePermissionDenied, "IPC_UID_REQUIRED")
	}
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, "LOCAL_CONTROL_REQUIRED")
	}
	if len(info.RequestHeader().Values("Authorization")) > 0 {
		return connect.NewError(connect.CodePermissionDenied, "LOCAL_CONTROL_REQUIRED")
	}
	if !negotiate {
		values := info.RequestHeader().Values(NodeInstanceHeader)
		if len(values) != 1 || values[0] != c.node.instance {
			return connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
		}
	}
	if c.node.ctx.Err() != nil {
		return connect.NewError(connect.CodeUnavailable, "NODE_STOPPING")
	}
	return nil
}
func (c *Control) Negotiate(ctx context.Context, offer *pb.ProtocolOffer) (*agentpb.NodeNegotiation, error) {
	if err := c.authorize(ctx, true); err != nil {
		return nil, err
	}
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}}, offer, c.build)
	if err != nil {
		return nil, err
	}
	return &agentpb.NodeNegotiation{Protocol: agreement, InstanceId: c.node.instance}, nil
}
func (c *Control) GetStatus(ctx context.Context, _ *pb.Empty) (*agentpb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return c.node.snapshot(), nil
}
func (c *Control) BeginLogin(ctx context.Context, request *agentpb.BeginLoginRequest) (*agentpb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return c.node.beginLogin(request)
}
func (c *Control) CancelLogin(ctx context.Context, request *agentpb.CancelLoginRequest) (*agentpb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return c.node.cancelAttempt(request.GetLoginId(), "LOGIN_CANCELED")
}

func (n *Node) beginLogin(request *agentpb.BeginLoginRequest) (*agentpb.NodeStatus, error) {
	if request.GetRequestId() == "" || len(request.GetRequestId()) > 128 || strings.ContainsAny(request.GetRequestId(), "\r\n") {
		return nil, connect.NewError(connect.CodeInvalidArgument, "REQUEST_ID_REQUIRED")
	}
	interactive := request.GetInteractive()
	key := request.GetAuthKey()
	if !interactive && len(key) == 0 || len(key) > 4096 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "LOGIN_METHOD_REQUIRED")
	}
	material := append([]byte{0}, key...)
	if interactive {
		material[0] = 1
	}
	fingerprint := sha256.Sum256(material)
	clear(material)
	n.mu.Lock()
	if n.closing || n.ctx.Err() != nil {
		n.mu.Unlock()
		return nil, connect.NewError(connect.CodeUnavailable, "NODE_STOPPING")
	}
	if previous := n.requests[request.RequestId]; previous != nil {
		if previous.fingerprint != fingerprint {
			n.mu.Unlock()
			return nil, connect.NewError(connect.CodeInvalidArgument, "REQUEST_ID_REUSED")
		}
		var result *agentpb.NodeStatus
		if previous.finished {
			result = proto.Clone(previous.result).(*agentpb.NodeStatus)
		} else {
			result = proto.Clone(n.status).(*agentpb.NodeStatus)
		}
		n.mu.Unlock()
		return result, nil
	}
	if n.attempt != nil && !n.attempt.finished {
		n.mu.Unlock()
		return nil, connect.NewError(connect.CodeAlreadyExists, "LOGIN_IN_PROGRESS")
	}
	if len(n.requests) >= 128 {
		n.mu.Unlock()
		return nil, connect.NewError(connect.CodeResourceExhausted, "LOGIN_REQUEST_LIMIT")
	}
	attempt := &loginAttempt{id: identifier("login_"), requestID: request.RequestId, fingerprint: fingerprint, interactive: interactive, expires: time.Now().Add(LoginLifetime), done: make(chan struct{})}
	if n.status.State == "online" {
		// Even a successful no-op must reserve its request ID across restarts.
		result := proto.Clone(n.status).(*agentpb.NodeStatus)
		result.LoginId = attempt.id
		if err := n.saveLoginLocked(attempt, result, true); err != nil {
			n.mu.Unlock()
			return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_STATE_WRITE_FAILED")
		}
		attempt.finished = true
		attempt.result = result
		close(attempt.done)
		n.requests[request.RequestId] = attempt
		n.mu.Unlock()
		return proto.Clone(result).(*agentpb.NodeStatus), nil
	}
	pending := &agentpb.NodeStatus{State: "authenticating", LoginId: attempt.id, LoginExpiresAt: attempt.expires.Unix()}
	if err := n.saveLoginLocked(attempt, pending, false); err != nil {
		n.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_STATE_WRITE_FAILED")
	}
	// Publishing a new attempt immediately retires the previous SDK's events.
	// runLogin replaces the SDK asynchronously under opMu; until then a queued
	// rejection/status from the old login must not complete this new attempt.
	n.generation++
	n.attempt = attempt
	n.requests[request.RequestId] = attempt
	n.status = pending
	result := proto.Clone(n.status).(*agentpb.NodeStatus)
	n.wg.Add(1)
	n.mu.Unlock()
	keyCopy := append([]byte(nil), key...)
	go func() { defer n.wg.Done(); defer clear(keyCopy); n.runLogin(attempt, keyCopy) }()
	return result, nil
}
func (n *Node) runLogin(attempt *loginAttempt, key []byte) {
	n.opMu.Lock()
	n.mu.Lock()
	active := n.attempt == attempt && !attempt.finished
	n.mu.Unlock()
	if !active {
		n.opMu.Unlock()
		clear(key)
		return
	}
	// Each explicit attempt gets a fresh control client; old rejected keys and
	// canceled followups cannot be resumed by a subsequent request.
	n.stopLocked()
	err := n.startLocked(attempt)
	n.mu.Lock()
	active = n.attempt == attempt && !attempt.finished
	lc := n.local
	n.mu.Unlock()
	if err == nil && active {
		callCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		if attempt.interactive {
			err = lc.StartLoginInteractive(callCtx)
		} else {
			err = lc.Start(callCtx, ipn.Options{AuthKey: string(key)})
			if err == nil {
				err = lc.StartLoginInteractive(callCtx)
			}
		}
		cancel()
	}
	n.opMu.Unlock()
	clear(key)
	if err != nil {
		n.mu.Lock()
		if !attempt.finished {
			n.status = &agentpb.NodeStatus{State: "offline", ReasonCode: "LOGIN_START_FAILED", LoginId: attempt.id}
			n.finishAttemptLocked(attempt, n.status)
		}
		n.mu.Unlock()
		return
	}
	timer := time.NewTimer(time.Until(attempt.expires))
	defer timer.Stop()
	select {
	case <-n.ctx.Done():
		return
	case <-attempt.done:
		return
	case <-timer.C:
		_, _ = n.cancelAttempt(attempt.id, "LOGIN_EXPIRED")
	}
}
func (n *Node) cancelAttempt(id, reason string) (*agentpb.NodeStatus, error) {
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "LOGIN_ID_REQUIRED")
	}
	n.opMu.Lock()
	defer n.opMu.Unlock()
	n.mu.Lock()
	attempt := n.attempt
	if attempt == nil || attempt.id != id {
		attempt = nil
		for _, previous := range n.requests {
			if previous.id == id && previous.finished {
				attempt = previous
				break
			}
		}
		if attempt == nil {
			n.mu.Unlock()
			return nil, connect.NewError(connect.CodeNotFound, "LOGIN_NOT_FOUND")
		}
	}
	if attempt.finished {
		result := proto.Clone(n.status).(*agentpb.NodeStatus)
		n.mu.Unlock()
		return result, nil
	}
	lc := n.local
	generation := n.generation
	n.mu.Unlock()
	// Do not stop a node that completed authentication while the request raced.
	if lc != nil {
		queryCtx, cancel := context.WithTimeout(n.ctx, 3*time.Second)
		observed, err := lc.StatusWithoutPeers(queryCtx)
		cancel()
		if err == nil && observed.BackendState == "Running" {
			n.applyStatus(generation, observed)
			return n.snapshot(), nil
		}
	}
	n.mu.Lock()
	n.status = &agentpb.NodeStatus{State: "needs_login", ReasonCode: reason, LoginId: id}
	err := n.finishAttemptLocked(attempt, n.status)
	result := proto.Clone(n.status).(*agentpb.NodeStatus)
	n.mu.Unlock()
	// SDK has no cancellation API for a login URL. Closing the pending SDK
	// cancels its registration/followup and destroys its in-memory auth key. It
	// does not revoke an online node or remove persisted identity data.
	n.stopLocked()
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_STATE_WRITE_FAILED")
	}
	return result, nil
}
