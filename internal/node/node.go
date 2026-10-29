// Package node owns a single embedded tsnet identity and its local management
// API. It never operates the machine's system tailscaled.
package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/logtail"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

const LoginLifetime = 10 * time.Minute
const NodeInstanceHeader = "Shiki-Node-Control-Instance-Id"

type Options struct {
	StateDir   string
	Hostname   string
	ControlURL string
	// Tests use an isolated in-process control server. Production never accepts
	// HTTP control URLs or exposes this escape hatch through configuration.
	allowHTTPForTest bool
}

type Node struct {
	mu                  sync.Mutex
	opMu                sync.Mutex
	ctx                 context.Context
	cancel              context.CancelFunc
	options             Options
	instance            string
	state               *storage.Store
	server              *tsnet.Server
	local               *local.Client
	generation          uint64
	watchCancel         context.CancelFunc
	authorization       context.Context
	authorizationCancel context.CancelFunc
	status              *agentpb.NodeStatus
	attempt             *loginAttempt
	requests            map[string]*loginAttempt
	started             chan struct{}
	wg                  sync.WaitGroup
	closeOnce           sync.Once
	closeErr            error
	closing             bool
	closed              chan struct{}
}

type loginAttempt struct {
	id          string
	fingerprint [32]byte
	requestID   string
	interactive bool
	expires     time.Time
	finished    bool
	result      *agentpb.NodeStatus
	done        chan struct{}
}

func identifier(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(bytes[:])
}

// Open acquires the node state lock and returns before network authentication.
// Start/Listen/Dial never block the caller's NodeControl listener from starting.
// SDK log uploads are disabled process-wide before any node starts, because the
// SDK's own logger otherwise records authentication URLs outside UserLogf.
func Open(parent context.Context, options Options) (*Node, error) {
	if !filepath.IsAbs(options.StateDir) || strings.TrimSpace(options.Hostname) == "" || strings.ContainsAny(options.Hostname, " /\\\t\r\n") {
		return nil, errors.New("node requires an absolute state directory and hostname")
	}
	if options.ControlURL == "" {
		options.ControlURL = "https://controlplane.tailscale.com"
	}
	control, err := url.Parse(options.ControlURL)
	if err != nil || control.Hostname() == "" || control.User != nil || control.RawQuery != "" || control.Fragment != "" || (control.Scheme != "https" && !(options.allowHTTPForTest && control.Scheme == "http" && (control.Hostname() == "127.0.0.1" || control.Hostname() == "localhost"))) {
		return nil, errors.New("node control URL must use HTTPS")
	}
	// The SDK implicitly reads these globals. Require explicit NodeControl input
	// so an inherited shell environment cannot silently enroll or replace a node.
	for _, key := range []string{"TS_AUTHKEY", "TS_AUTH_KEY", "TS_CLIENT_SECRET", "TS_CLIENT_ID", "TS_ID_TOKEN", "TS_AUDIENCE", "TSNET_FORCE_LOGIN"} {
		if os.Getenv(key) != "" {
			return nil, errors.New("implicit Tailscale authentication environment is not supported; use NodeControl")
		}
	}
	store, err := storage.Open(options.StateDir, storage.Options{DisableRecovery: true})
	if err != nil {
		return nil, err
	}
	logtail.Disable()
	ctx, cancel := context.WithCancel(parent)
	n := &Node{ctx: ctx, cancel: cancel, options: options, instance: identifier("node_"), state: store, status: &agentpb.NodeStatus{State: "uninitialized"}, requests: map[string]*loginAttempt{}, started: make(chan struct{}), closed: make(chan struct{})}
	if err := n.recoverLogins(); err != nil {
		cancel()
		_ = store.Close()
		return nil, err
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		defer close(n.started)
		n.opMu.Lock()
		defer n.opMu.Unlock()
		_ = n.startLocked(nil)
	}()
	context.AfterFunc(parent, func() { _ = n.Close() })
	return n, nil
}
func (n *Node) InstanceID() string { return n.instance }

// startLocked requires opMu. All SDK fields are immutable after Start.
func (n *Node) startLocked(attempt *loginAttempt) error {
	if n.ctx.Err() != nil {
		return n.ctx.Err()
	}
	n.mu.Lock()
	if n.attempt != attempt || attempt != nil && attempt.finished {
		n.mu.Unlock()
		return context.Canceled
	}
	if n.server != nil {
		n.mu.Unlock()
		return nil
	}
	generation := n.generation
	n.mu.Unlock()
	server := &tsnet.Server{Dir: n.options.StateDir, Hostname: n.options.Hostname, ControlURL: n.options.ControlURL, Store: &stateStore{n.state}, Logf: logger.Discard, UserLogf: logger.Discard}
	if err := server.Start(); err != nil {
		n.setGenerationFailure(generation, "NODE_START_FAILED")
		return connect.NewError(connect.CodeFailedPrecondition, "NODE_START_FAILED")
	}
	lc, err := server.LocalClient()
	if err != nil {
		_ = server.Close()
		n.setGenerationFailure(generation, "NODE_START_FAILED")
		return connect.NewError(connect.CodeFailedPrecondition, "NODE_START_FAILED")
	}
	n.mu.Lock()
	// Initial startup can overlap BeginLogin, and an obsolete queued login
	// worker can lose ownership before its SDK finishes starting. Neither may
	// publish a watcher as though it belonged to the current login attempt.
	if generation != n.generation || n.attempt != attempt || attempt != nil && attempt.finished {
		n.mu.Unlock()
		_ = server.Close()
		return context.Canceled
	}
	watchCtx, watchCancel := context.WithCancel(n.ctx)
	n.server = server
	n.local = lc
	n.generation++
	generation = n.generation
	n.watchCancel = watchCancel
	n.mu.Unlock()
	n.wg.Add(1)
	go n.watch(watchCtx, lc, generation)
	return nil
}
func (n *Node) setGenerationFailure(generation uint64, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if generation == n.generation {
		n.status = &agentpb.NodeStatus{State: "offline", ReasonCode: reason}
	}
}
func (n *Node) snapshot() *agentpb.NodeStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return proto.Clone(n.status).(*agentpb.NodeStatus)
}

// Status exposes a diagnostic copy without authentication URLs. Login URLs
// remain available only through the owner-authorized NodeControl RPC.
func (n *Node) Status() *agentpb.NodeStatus {
	status := n.snapshot()
	status.LoginUrl = ""
	return status
}
func (n *Node) watch(ctx context.Context, lc *local.Client, generation uint64) {
	defer n.wg.Done()
	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		if ctx.Err() == nil {
			n.setGenerationFailure(generation, "NODE_STATUS_UNAVAILABLE")
		}
		return
	}
	defer watcher.Close()
	for {
		notification, err := watcher.Next()
		if err != nil {
			return
		}
		if notification.ErrMessage != nil {
			// Only classify the SDK's explicit control rejection, never dial errors.
			text := strings.ToLower(*notification.ErrMessage)
			if strings.Contains(text, "invalid authkey") || strings.Contains(text, "invalid auth key") {
				if n.rejectAuthKey(generation) {
					go n.stopGeneration(generation)
				}
				continue
			}
		}
		statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, err := lc.StatusWithoutPeers(statusCtx)
		cancel()
		if err != nil {
			continue
		}
		n.applyStatus(generation, status)
	}
}
func (n *Node) rejectAuthKey(generation uint64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	active := n.attempt
	if generation != n.generation || active == nil || active.finished {
		return false
	}
	n.status = &agentpb.NodeStatus{State: "denied", ReasonCode: "INVALID_AUTH_KEY", LoginId: active.id}
	n.finishAttemptLocked(active, n.status)
	return true
}
func (n *Node) applyStatus(generation uint64, observed *ipnstate.Status) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if generation != n.generation {
		return
	}
	if n.attempt != nil && n.attempt.finished && n.attempt.result.GetState() != "online" {
		return
	}
	result := &agentpb.NodeStatus{State: "offline", ReasonCode: "NODE_OFFLINE"}
	switch observed.BackendState {
	case "Running":
		if observed.Self != nil && observed.Self.Expired {
			result.State = "expired"
			result.ReasonCode = "NODE_KEY_EXPIRED"
		} else if len(observed.TailscaleIPs) > 0 {
			result.State = "online"
			result.ReasonCode = ""
			for _, ip := range observed.TailscaleIPs {
				result.Addresses = append(result.Addresses, ip.String())
			}
		}
	case "NeedsLogin":
		result.State = "needs_login"
		result.ReasonCode = "NODE_AUTH_REQUIRED"
	case "NeedsMachineAuth":
		result.State = "denied"
		result.ReasonCode = "NODE_APPROVAL_REQUIRED"
	case "Starting":
		result.State = "authenticating"
		result.ReasonCode = ""
	case "NoState":
		result.State = "uninitialized"
		result.ReasonCode = ""
	}
	if attempt := n.attempt; attempt != nil {
		result.LoginId = attempt.id
		if !attempt.finished && result.State != "online" {
			result.LoginExpiresAt = attempt.expires.Unix()
			if attempt.interactive && observed.AuthURL != "" && time.Now().Before(attempt.expires) {
				result.LoginUrl = observed.AuthURL
			}
		}
		if result.State == "online" && !attempt.finished {
			if err := n.finishAttemptLocked(attempt, result); err != nil {
				go n.stopGeneration(generation)
				return
			}
		}
	}
	if result.State == "online" {
		if n.authorization == nil {
			n.authorization, n.authorizationCancel = context.WithCancel(n.ctx)
		}
	} else if n.authorizationCancel != nil {
		n.authorizationCancel()
		n.authorization = nil
		n.authorizationCancel = nil
	}
	n.status = result
}

// stopGeneration invalidates only a pending SDK instance; persisted identity
// remains intact. A fresh explicit BeginLogin creates a new SDK login flow.
func (n *Node) stopGeneration(generation uint64) {
	n.opMu.Lock()
	defer n.opMu.Unlock()
	n.mu.Lock()
	same := generation == n.generation
	n.mu.Unlock()
	if same {
		n.stopLocked()
	}
}
func (n *Node) stopLocked() {
	n.mu.Lock()
	server := n.server
	cancel := n.watchCancel
	if n.authorizationCancel != nil {
		n.authorizationCancel()
		n.authorization = nil
		n.authorizationCancel = nil
	}
	n.server = nil
	n.local = nil
	n.generation++
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil {
		_ = server.Close()
	}
}
func (n *Node) Close() error {
	n.closeOnce.Do(func() {
		defer close(n.closed)
		n.mu.Lock()
		n.closing = true
		n.cancel()
		n.mu.Unlock()
		n.opMu.Lock()
		n.stopLocked()
		n.opMu.Unlock()
		n.wg.Wait()
		n.closeErr = n.state.Close()
	})
	return n.closeErr
}

func (n *Node) finishAttemptLocked(attempt *loginAttempt, result *agentpb.NodeStatus) error {
	if err := n.saveLoginLocked(attempt, result, true); err != nil {
		n.status = &agentpb.NodeStatus{State: "offline", ReasonCode: "NODE_STATE_WRITE_FAILED", LoginId: attempt.id}
		return err
	}
	if !attempt.finished {
		attempt.finished = true
		close(attempt.done)
	}
	attempt.result = proto.Clone(result).(*agentpb.NodeStatus)
	return nil
}

// Done closes after SDK resources and the identity lock are released.
func (n *Node) Done() <-chan struct{} { return n.closed }
