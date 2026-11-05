package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
	"tailscale.com/types/key"
)

func newLoginReuseNode(t *testing.T) *Node {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	lease, revoke := context.WithCancel(ctx)
	n := &Node{ctx: ctx, state: store, server: &tsnet.Server{}, local: &local.Client{},
		generation: 7, selfKey: key.NewNode().Public(), authorization: lease, authorizationCancel: revoke,
		status: &apb.NodeStatus{State: "offline", ReasonCode: "NODE_CONTROL_UNAVAILABLE"}, requests: map[string]*loginAttempt{}}
	t.Cleanup(func() {
		cancel()
		n.wg.Wait()
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return n
}

func TestLoginSDKReuseRequiresAuthorizedInteractiveAttempt(t *testing.T) {
	for _, mode := range []string{"interactive", "auth-key", "revoked", "no-sdk", "failed-interactive"} {
		t.Run(mode, func(t *testing.T) {
			n := newLoginReuseNode(t)
			request := &apb.BeginLoginRequest{RequestId: mode, Method: &apb.BeginLoginRequest_Interactive{Interactive: true}}
			switch mode {
			case "auth-key":
				request.Method = &apb.BeginLoginRequest_AuthKey{AuthKey: []byte("fixture-key")}
			case "revoked":
				n.authorizationCancel()
			case "no-sdk":
				n.server = nil
			case "failed-interactive":
				// A LocalAPI failure can finish an attempt before the SDK
				// publishes its pending URL and retires the old authorization.
				n.attempt = &loginAttempt{finished: true, interactive: true, result: &apb.NodeStatus{State: "offline", ReasonCode: "LOGIN_START_FAILED"}}
			}
			// Hold the worker at its ownership boundary, so stale observations
			// and request deduplication are checked before any SDK side effect.
			n.opMu.Lock()
			defer func() {
				if n.attempt != nil {
					_, _, _ = n.commitCancellation(n.attempt, "LOGIN_CANCELED")
				}
				n.opMu.Unlock()
			}()
			generation := n.generation
			pending, err := n.beginLogin(request)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := n.beginLogin(request)
			if err != nil || repeated.LoginId != pending.LoginId {
				t.Fatal("login request was not deduplicated")
			}
			if retained := n.generation == generation; retained != (mode == "interactive") {
				t.Fatal("SDK generation did not follow the existing authorization lifetime")
			}
			if mode != "interactive" {
				n.applyStatus(generation, &ipnstate.Status{BackendState: "NeedsMachineAuth"})
				if n.snapshot().State != "authenticating" {
					t.Fatal("retired SDK observation affected the new attempt")
				}
			}
		})
	}
}

func TestInteractiveLoginReusesSDKWithoutAcceptingOldRunning(t *testing.T) {
	n := newLoginReuseNode(t)
	oldSDK, generation, oldLease := n.server, n.generation, n.authorization
	observed := &ipnstate.Status{BackendState: "Running", HaveNodeKey: true,
		Self: &ipnstate.PeerStatus{PublicKey: n.selfKey, Online: true}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	posted := make(chan struct{}, 1)
	var posts atomic.Int32
	n.local = &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts.Add(1)
			if request.URL.Path != "/localapi/v0/login-interactive" {
				t.Error("interactive recovery replaced the control client")
			}
			posted <- struct{}{}
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encoded)), Request: request}, nil
	})}
	n.opMu.Lock()
	_, err = n.beginLogin(&apb.BeginLoginRequest{RequestId: "recover", Method: &apb.BeginLoginRequest_Interactive{Interactive: true}})
	if err != nil || n.generation != generation {
		// Keep a failing implementation from starting the placeholder SDK.
		if n.attempt != nil {
			_, _, _ = n.commitCancellation(n.attempt, "LOGIN_CANCELED")
		}
		n.opMu.Unlock()
		t.Fatal("interactive recovery retired the established SDK", err)
	}
	n.applyStatus(generation, observed)
	if n.attempt.finished {
		n.opMu.Unlock()
		t.Fatal("old Running completed login before the registration call")
	}
	n.opMu.Unlock()
	select {
	case <-posted:
	case <-time.After(5 * time.Second):
		t.Fatal("interactive recovery did not reach the existing SDK")
	}
	// Wait for requestLogin to publish its result, without a scheduler delay.
	n.opMu.Lock()
	n.opMu.Unlock()
	if n.server != oldSDK || n.generation != generation || posts.Load() != 1 {
		t.Fatal("interactive recovery replaced or restarted the SDK")
	}
	n.applyStatus(generation, observed)
	if n.attempt.finished || n.snapshot().State != "authenticating" {
		t.Fatal("same-key Running completed a newly requested registration")
	}
	observed.AuthURL = "https://control.invalid/login/recovery"
	n.applyStatus(generation, observed)
	if n.snapshot().LoginUrl != observed.AuthURL || oldLease.Err() == nil || n.DataPlaneAuthorized() {
		t.Fatal("pending registration failed to publish its URL or retire old authorization")
	}
	observed.AuthURL = ""
	observed.Self.PublicKey = key.NewNode().Public()
	n.applyStatus(generation, observed)
	if !n.attempt.finished || n.snapshot().State != "online" || !n.DataPlaneAuthorized() || n.authorization == oldLease {
		t.Fatal("completed registration did not establish a new authorization lifetime")
	}
}

func TestFailedInteractiveRecoveryRetiresAuthorizedSDK(t *testing.T) {
	for _, persistenceFailure := range []bool{false, true} {
		name := "LocalAPI"
		if persistenceFailure {
			name = "LocalAPI-and-persistence"
		}
		t.Run(name, func(t *testing.T) {
			control := localControl(t, true, "")
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			n := newTestNode(t, ctx, control, "failed-recovery")
			loginInteractive(t, n, control)
			n.opMu.Lock()
			release, err := n.acquireStatusPermit(ctx)
			if err != nil {
				n.opMu.Unlock()
				t.Fatal(err)
			}
			n.mu.Lock()
			oldLease, generation := n.authorization, n.generation
			oldKey := n.selfKey
			n.status = &apb.NodeStatus{State: "offline", ReasonCode: "NODE_CONTROL_UNAVAILABLE"}
			// The existing SDK is real and already running, so failure cleanup must
			// release it. Only the worker's LocalAPI request is fault-injected.
			apiFailed := make(chan struct{}, 1)
			n.local = &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(*http.Request) (*http.Response, error) {
				if persistenceFailure {
					if err := n.state.Close(); err != nil {
						t.Error(err)
					}
				}
				apiFailed <- struct{}{}
				return nil, errors.New("fixture LocalAPI unavailable")
			})}
			n.mu.Unlock()
			_, err = n.beginLogin(&apb.BeginLoginRequest{RequestId: "failed-recovery", Method: &apb.BeginLoginRequest_Interactive{Interactive: true}})
			n.mu.Lock()
			attempt := n.attempt
			n.mu.Unlock()
			release()
			n.opMu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if !persistenceFailure {
				select {
				case <-attempt.done:
				case <-ctx.Done():
					t.Fatal("LocalAPI failure did not finish the login attempt")
				}
				if oldLease.Err() == nil || n.DataPlaneAuthorized() {
					t.Fatal("failed terminal state retained the previous authorization")
				}
			} else {
				// Persistence failure intentionally cannot close attempt.done. Wait
				// until the worker owns opMu, then join it through that mutex below.
				select {
				case <-apiFailed:
				case <-ctx.Done():
					t.Fatal("LocalAPI failure was not injected")
				}
			}
			// The failed worker holds opMu through SDK shutdown; observing its release
			// proves a future login cannot be closed by delayed failure cleanup.
			n.opMu.Lock()
			n.mu.Lock()
			stopped := n.server == nil && n.local == nil && len(n.selfAddresses) == 0 && n.generation != generation
			n.mu.Unlock()
			n.opMu.Unlock()
			if !stopped || oldLease.Err() == nil || n.DataPlaneAuthorized() {
				t.Fatal("failed recovery retained its SDK or data-plane identity")
			}
			wantReason := "LOGIN_START_FAILED"
			if persistenceFailure {
				wantReason = "NODE_STATE_WRITE_FAILED"
			}
			for _, observed := range []*ipnstate.Status{
				{BackendState: "Running", AuthURL: "https://control.invalid/login/late", Self: &ipnstate.PeerStatus{PublicKey: oldKey}},
				{BackendState: "NeedsLogin", Self: &ipnstate.PeerStatus{PublicKey: oldKey, Expired: true}},
				{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: oldKey, Online: true}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}},
			} {
				n.applyStatus(generation, observed)
				if status := n.snapshot(); n.DataPlaneAuthorized() || status.State != "offline" || status.ReasonCode != wantReason || status.LoginUrl != "" {
					t.Fatal("late SDK notification revived the failed login")
				}
			}
		})
	}
}
