package node

import (
	"context"
	"errors"
	"sync"

	"connectrpc.com/connect/v2"
)

// ErrAuthorizationRequired identifies admission blocked by this process's own
// node. The cause is not serialized: remote error text cannot impersonate it.
var ErrAuthorizationRequired = errors.New("local node authorization required")

func authorizationError(code connect.Code) error {
	return connect.NewError(code, "NODE_AUTH_REQUIRED").WithCause(ErrAuthorizationRequired)
}

// authorizedLocked separates retained data-plane authorization from control
// connectivity diagnostics. The caller must hold n.mu.
func (n *Node) authorizedLocked() bool {
	return n.authorization != nil && n.authorization.Err() == nil && n.ctx.Err() == nil
}

// DataPlaneAuthorized reports an established, unrevoked SDK authorization
// lifetime. It remains true during a control-plane outage; it is not a claim
// that control is online or that any particular peer is currently reachable.
func (n *Node) DataPlaneAuthorized() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.authorizedLocked()
}

// authorizeRPC runs even when HTTP/2 reuses a live TCP connection. Each RPC
// belongs to the current online authorization lifetime, which is canceled on
// an explicit SDK identity/state loss. A later online transition creates a new
// lifetime; it never revives streams from the previous one.
func (n *Node) authorizeRPC(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		n.mu.Lock()
		lease := n.authorization
		online := n.authorizedLocked()
		n.mu.Unlock()
		if !online || lease.Err() != nil {
			return nil, authorizationError(connect.CodeFailedPrecondition)
		}
		callCtx, cancel := context.WithCancel(ctx)
		stopLease := context.AfterFunc(lease, cancel)
		// The authorization epoch outlives individual calls. A caller may
		// cancel and abandon a stream without another Receive or Close; remove
		// its lease hook even in that case, including during construction.
		stopCall := context.AfterFunc(callCtx, func() { stopLease() })
		stop := func() { stopCall(); stopLease() }
		stream, err := next(callCtx, spec)
		if err != nil {
			stop()
			cancel()
			return nil, err
		}
		return &authorizedStream{ClientStream: stream, cancel: cancel, stop: stop, lease: lease, singleResponse: spec.StreamType == connect.StreamTypeClient}, nil
	}
}

type authorizedStream struct {
	connect.ClientStream
	cancel context.CancelFunc
	lease  context.Context
	stop   func()
	// A successful client-stream Receive includes the final response/status.
	// Server/bidi messages and request half-close do not end the RPC lifetime.
	singleResponse bool
	once           sync.Once
	err            error
}

func (s *authorizedStream) Close() error {
	s.once.Do(func() { s.stop(); s.cancel(); s.err = s.ClientStream.Close() })
	return s.err
}
func (s *authorizedStream) Receive(message any) error {
	err := s.ClientStream.Receive(message)
	if s.lease.Err() != nil {
		err = authorizationError(connect.CodeCanceled)
	}
	if err != nil || s.singleResponse {
		_ = s.Close()
	}
	return err
}

func (s *authorizedStream) Send(message any) error {
	if s.lease.Err() != nil {
		return authorizationError(connect.CodeCanceled)
	}
	return s.ClientStream.Send(message)
}
