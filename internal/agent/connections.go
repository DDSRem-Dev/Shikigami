package agent

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/proto"
)

type remotePool struct {
	client        *connect.Client
	base          *connect.Client
	daemonID      string
	ctx           context.Context
	cancel        context.CancelFunc
	close         func()
	bad           atomic.Bool
	state, reason string
	protocol      *pb.ProtocolAgreement
	capabilities  atomic.Pointer[remoteCapabilities]
	route, path   string
	attempts      []string // Latest completed route selection, never persisted.
	address       string
	episode       reconnectEpisode
	nextCheck     time.Time
	failure       atomic.Pointer[remoteFailure]
	needsSuspend  bool
	// Protected by Resources.mu; only consecutive full-budget probe deadlines
	// count, never a short caller budget or an abandoned request.
	probeTimeouts int
	// Only one authentication operation per server generation; waiters do not
	// hold Resources.mu and may independently cancel.
	resolving chan struct{}
}

// One slow probe is not evidence that unrelated live streams have failed.
// Two consecutive failures are the minimum evidence for route replacement.
const consecutiveProbeTimeoutLimit = 2

func (p *remotePool) invalidate() {
	if p.bad.CompareAndSwap(false, true) {
		p.cancel()
	}
}
func (r *Resources) retireLocked(id string) {
	if p := r.pools[id]; p != nil {
		p.invalidate()
		if p.close != nil {
			p.close()
		}
		delete(r.pools, id)
	}
}
func (r *Resources) decorateLocked(record *agentpb.ServerRecord) {
	record.ConnectionState = "offline"
	record.ReasonCode = "NOT_CONNECTED"
	record.RemoteProtocol = nil
	record.ConnectionPath = "unknown"
	record.ConnectionAttempts = nil
	if !record.CredentialPresent {
		record.ConnectionState = "auth_required"
		record.ReasonCode = "CREDENTIAL_REQUIRED"
	}
	if p := r.pools[record.Config.Id]; p != nil {
		record.ConnectionAttempts = slices.Clone(p.attempts)
		if !p.bad.Load() || p.client == nil {
			record.ConnectionState = p.state
			record.ReasonCode = p.reason
			if p.path != "" {
				record.ConnectionPath = p.path
			}
			if p.protocol != nil {
				record.RemoteProtocol = proto.Clone(p.protocol).(*pb.ProtocolAgreement)
			}
		}
	}
}
func safeRemoteError(err error) error {
	if err == nil {
		return nil
	}
	if version.SafeDetail(err) != nil {
		return connection.RebuildError(err)
	}
	code := connect.CodeOf(err)
	if connection.IsRetryableNetworkError(err) {
		code = connect.CodeUnavailable
	}
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	return rpcError(code, "REMOTE_RPC_FAILED")
}
func (r *Resources) Resolve(ctx context.Context, id string) (*connect.Client, error) {
	client, _, err := r.ResolveSnapshot(ctx, id)
	return client, err
}

// ResolveSnapshot returns the authenticated, persisted server generation.
// Callers that perform remote validation before a local mutation must compare
// this record (including credential revision) again in their commit transaction.
func (r *Resources) ResolveSnapshot(ctx context.Context, id string) (*connect.Client, *agentpb.ServerRecord, error) {
	return r.resolveSnapshot(ctx, id, false)
}

func (r *Resources) resolveSnapshot(ctx context.Context, id string, maintenance bool) (*connect.Client, *agentpb.ServerRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.faulted.Load() {
		return nil, nil, rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
	}
	if maintenance {
		if p := r.pools[id]; p != nil {
			if p.resolving != nil || !p.maintenanceDue(time.Now()) {
				return nil, nil, nil
			}
			p.nextCheck = time.Now().Add(5 * time.Second)
		}
	}
	snapshot, err := r.store.View()
	if err != nil {
		return nil, nil, stateError(err)
	}
	client, err := r.resolveLocked(ctx, id, snapshot)
	if err != nil {
		return nil, nil, err
	}
	if err = r.persistPinnedIdentity(snapshot, id); err != nil {
		r.faulted.Store(true)
		for id := range r.pools {
			r.retireLocked(id)
		}
		r.service.setState("degraded", "AGENT_STATE_UNAVAILABLE")
		return nil, nil, err
	}
	record, err := storedServer(snapshot, id)
	return client, record, err
}
func (r *Resources) persistPinnedIdentity(candidate *storage.Snapshot, id string) error {
	verified, err := storedServer(candidate, id)
	if err != nil {
		return err
	}
	current, err := r.store.View()
	if err != nil {
		return stateError(err)
	}
	existing, err := storedServer(current, id)
	if err != nil {
		return err
	}
	if existing.Config.DaemonId != "" {
		return nil
	}
	return stateError(r.store.Update(func(current *storage.Snapshot) error {
		record, err := storedServer(current, id)
		if err != nil {
			return err
		}
		if record.Config.DaemonId == "" && verified.Config.DaemonId != "" {
			record.Config.DaemonId = verified.Config.DaemonId
			record.Revision = verified.Revision
			return current.Put("servers", id, record)
		}
		return nil
	}))
}

// resolveLocked enters and returns with Resources.mu held, but releases it for
// every network operation and while awaiting another probe. It validates both
// the pool generation and immutable server/credential snapshot before publishing
// any result. Callers must persist a new identity pin before exposing the client.
func (r *Resources) resolveLocked(ctx context.Context, id string, snapshot *storage.Snapshot) (*connect.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.ctx.Err() != nil {
		return nil, rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	record, err := storedServer(snapshot, id)
	if err != nil {
		return nil, err
	}
	// Serialize only this server's authentication work, never local state access.
	for {
		existing := r.pools[id]
		if existing == nil || existing.resolving == nil {
			break
		}
		done := existing.resolving
		r.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		case <-r.ctx.Done():
		}
		r.mu.Lock()
		if err := ctx.Err(); err != nil {
			return nil, safeRemoteError(err)
		}
		if r.ctx.Err() != nil {
			return nil, rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
		}
		current, err := r.store.View()
		if err != nil {
			return nil, stateError(err)
		}
		latest, err := storedServer(current, id)
		if err != nil {
			return nil, err
		}
		// No network work has started for this waiter. Authenticate the current
		// generation, including a pin established by the preceding caller.
		record = latest
		if err := snapshot.Put("servers", id, record); err != nil {
			return nil, stateError(err)
		}
	}
	gate := make(chan struct{})
	owner := r.pools[id]
	if owner != nil {
		owner.resolving = gate
	}
	defer func() {
		if owner != nil && owner.resolving == gate {
			owner.resolving = nil
		}
		close(gate)
	}()
	validate := func(pool *remotePool) error {
		if r.faulted.Load() {
			return rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
		}
		if r.ctx.Err() != nil {
			return rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
		}
		if r.pools[id] != pool {
			return rpcError(connect.CodeAborted, "SERVER_CONNECTION_REPLACED")
		}
		current, err := r.store.View()
		if err != nil {
			return stateError(err)
		}
		latest, err := storedServer(current, id)
		if err != nil {
			return err
		}
		if !proto.Equal(latest, record) {
			return rpcError(connect.CodeAborted, "SERVER_CONNECTION_REPLACED")
		}
		return nil
	}
	var episode reconnectEpisode
	if existing := r.pools[id]; existing != nil {
		episode = existing.episode
		if err := episode.allowed(time.Now()); err != nil {
			return nil, safeRemoteError(err)
		}
		if existing.client != nil && !existing.bad.Load() {
			// Reconfirm application identity after HTTP/2 internal reconnect, without
			// replaying the caller's operation. Leave budget for route replacement.
			r.mu.Unlock()
			agreement, path, probeErr := r.verifyRemote(ctx, existing)
			r.mu.Lock()
			if err = validate(existing); err != nil {
				return nil, err
			}
			err = probeErr
			if err == nil && existing.bad.Load() {
				err = rpcError(connect.CodeUnavailable, "SERVER_CONNECTION_REPLACED")
			}
			if err == nil {
				existing.protocol = agreement
				existing.setCapabilities(agreement)
				existing.path = path
				existing.probeTimeouts = 0
				if record.Config.DaemonId == "" {
					record.Config.DaemonId = existing.daemonID
					record.Revision = newID("rev_")
					if err := snapshot.Put("servers", id, record); err != nil {
						return nil, stateError(err)
					}
				}
				return existing.client, nil
			}
			// Canceling one caller's identity probe says nothing about the
			// shared route. In particular, pausing/stopping a sync task must not
			// cancel an unrelated terminal or put plugin management in cooldown.
			// A subsequent caller still performs its own authenticated probe.
			if ctx.Err() != nil {
				return nil, safeRemoteError(ctx.Err())
			}
			var remote *connect.Error
			if errors.As(err, &remote) && remote.IsRemote() && (remote.Code() == connect.CodeCanceled || remote.Code() == connect.CodeDeadlineExceeded) {
				// A request-scoped remote cancellation/deadline is not a trust
				// rejection or proof that other streams on the route failed.
				return nil, safeRemoteError(err)
			}
			var timeout remoteProbeTimeout
			if errors.As(err, &timeout) {
				if timeout.callerLimited {
					return nil, safeRemoteError(err)
				}
				existing.probeTimeouts++
				if existing.probeTimeouts < consecutiveProbeTimeoutLimit {
					// Fail this request without retiring an authenticated shared
					// route. A successful later probe resets the failure sequence;
					// repeated timeouts enter the normal bounded recovery below.
					return nil, safeRemoteError(err)
				}
			}
			existing.invalidate()
		} else if failure := existing.failure.Load(); failure != nil {
			err = failure.err
		} else {
			err = nil
		}
		if err != nil && !connection.IsRetryableNetworkError(err) {
			r.failRemote(existing, err)
			r.publish("server", id, id, record.Revision)
			return nil, safeRemoteError(err)
		}
	}
	r.retireLocked(id)
	lifetime, cancel := context.WithCancel(r.ctx)
	p := &remotePool{ctx: lifetime, cancel: cancel, state: "connecting", path: "unknown", episode: episode, resolving: gate}
	if owner != nil {
		owner.resolving = nil
	}
	owner = p
	r.pools[id] = p
	fail := func(err error) (*connect.Client, error) {
		r.failRemote(p, err)
		r.publish("server", id, id, record.Revision)
		return nil, safeRemoteError(err)
	}
	handshakeCtx, stop := context.WithCancel(ctx)
	defer stop()
	detach := context.AfterFunc(p.ctx, stop)
	defer detach()
	r.mu.Unlock()
	token, credentialErr := r.readCredential(record)
	var candidate *remoteCandidate
	var attempts []string
	var path string
	if credentialErr != nil {
		err = rpcError(connect.CodeUnauthenticated, "CREDENTIAL_REQUIRED")
	} else {
		candidate, attempts, err = establishRemote(handshakeCtx, record.Config, func(route string) (*connect.Client, func(), error) {
			return r.newTrustedRoute(record.Config, token, route)
		})
		if err == nil {
			path = r.connectionPath(handshakeCtx, candidate.route, record.Config.TailscaleAddress)
		}
	}
	r.mu.Lock()
	if invalid := validate(p); invalid != nil {
		if candidate != nil {
			candidate.close()
		}
		return nil, invalid
	}
	p.attempts = slices.Clone(attempts)
	if err != nil {
		return fail(err)
	}
	if err = handshakeCtx.Err(); err != nil {
		candidate.close()
		return fail(err)
	}
	p.close = candidate.close
	if record.Config.DaemonId == "" {
		record.Config.DaemonId = candidate.daemonID
		record.Revision = newID("rev_")
		if err = snapshot.Put("servers", id, record); err != nil {
			return fail(err)
		}
	}
	p.state = "ready"
	p.reason = ""
	p.protocol = candidate.agreement
	p.setCapabilities(candidate.agreement)
	p.base = candidate.base
	p.daemonID = candidate.daemonID
	p.route = candidate.route
	p.address = record.Config.TailscaleAddress
	p.path = path
	p.nextCheck = time.Now().Add(5 * time.Second)
	p.episode = reconnectEpisode{}
	p.client = connect.NewClient(clientTransport{p.base}, p.bind)
	r.publish("server", id, id, record.Revision)
	return p.client, nil
}

func (r *Resources) failRemote(p *remotePool, err error) {
	p.cancel()
	if p.close != nil {
		p.close()
		p.close = nil
	}
	p.client, p.base, p.protocol = nil, nil, nil
	p.path = "unknown"
	p.bad.Store(true)
	p.state, p.reason = "offline", "REMOTE_UNAVAILABLE"
	p.needsSuspend = true
	p.episode.failed(time.Now(), err)
	if p.episode.exhausted {
		p.reason = "RECONNECT_EXHAUSTED"
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		p.state, p.reason = "auth_required", "CREDENTIAL_REJECTED"
	case connect.CodeFailedPrecondition:
		p.state, p.reason = "incompatible", "REMOTE_INCOMPATIBLE"
	case connect.CodePermissionDenied:
		p.state, p.reason = "incompatible", "IDENTITY_VERIFICATION_DENIED"
		var remoteError *connect.Error
		if errors.As(err, &remoteError) && remoteError.Message() == "DAEMON_ID_MISMATCH" {
			p.reason = "DAEMON_ID_MISMATCH"
		}
	default:
		if p.episode.stopped {
			p.state, p.reason = "incompatible", "REMOTE_TRUST_OR_PROTOCOL_REJECTED"
		}
	}
}

func (r *Resources) verifyRemote(ctx context.Context, p *remotePool) (*pb.ProtocolAgreement, string, error) {
	probe, cancel, callerLimited := remoteProbeContext(ctx, 2)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	agreement, _, err := negotiateRemote(probe, p.base, p.daemonID)
	if err = classifyProbeError(ctx, probe, err); err != nil {
		var timeout remoteProbeTimeout
		if errors.As(err, &timeout) {
			return nil, "", remoteProbeTimeout{callerLimited: callerLimited}
		}
		return nil, "", err
	}
	return agreement, r.connectionPath(probe, p.route, p.address), nil
}

// ClientTransport preserves the node/TLS client's bounded messages, credential
// injection and cancellation machinery while adding a server-record lifetime.
type clientTransport struct{ client *connect.Client }

func (t clientTransport) NewClientStream(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
	return t.client.CallClientStream(ctx, spec)
}
func (p *remotePool) bind(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		if p.bad.Load() || p.ctx.Err() != nil {
			return nil, rpcError(connect.CodeUnavailable, "SERVER_CONNECTION_REPLACED")
		}
		if err := p.requireCapability(spec.Procedure); err != nil {
			return nil, err
		}
		child, cancel := context.WithCancel(ctx)
		stopPool := context.AfterFunc(p.ctx, cancel)
		// The pool outlives individual requests. Canceling a request must also
		// remove its pool callback, even if its caller abandons the stream before
		// Receive/Close. Otherwise the callback retains the canceled request's
		// context and transport metadata until the entire pool is replaced.
		stopChild := context.AfterFunc(child, func() { stopPool() })
		stop := func() { stopChild(); stopPool() }
		stream, err := next(child, spec)
		if err != nil {
			stop()
			cancel()
			p.failed(err)
			return nil, safeRemoteError(err)
		}
		return &remoteStream{ClientStream: stream, pool: p, cancel: cancel, stop: stop, singleResponse: spec.StreamType == connect.StreamTypeClient}, nil
	}
}
func (p *remotePool) failed(err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		p.failure.Store(&remoteFailure{err: err})
		p.invalidate()
	}
	if connection.IsRetryableNetworkError(err) {
		p.invalidate()
	}
}

type remoteStream struct {
	connect.ClientStream
	pool   *remotePool
	cancel context.CancelFunc
	stop   func()
	// Connect client-stream Receive drains its single response and terminal
	// status before returning success. Server/bidi streams still require EOF.
	singleResponse bool
	once           sync.Once
	err            error
}

func (s *remoteStream) Close() error {
	s.once.Do(func() { s.stop(); s.cancel(); s.err = s.ClientStream.Close() })
	return s.err
}
func (s *remoteStream) Send(message any) error {
	if s.pool.ctx.Err() != nil {
		return rpcError(connect.CodeCanceled, "SERVER_CONNECTION_REPLACED")
	}
	err := s.ClientStream.Send(message)
	s.pool.failed(err)
	return err
}
func (s *remoteStream) Receive(message any) error {
	err := s.ClientStream.Receive(message)
	s.pool.failed(err)
	if err != nil || s.singleResponse {
		_ = s.Close()
	}
	return err
}
