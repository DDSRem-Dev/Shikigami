package node

import (
	"context"
	"errors"
	"testing"
	"time"

	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/health"
	"tailscale.com/ipn"
	"tailscale.com/tsconst"
)

// The real SDK sends non-200 registration failures through login-state health,
// whereas its test control's RegisterResponse.Error uses Notify.ErrMessage.
// Exercise the health tracker and LocalAPI bus rather than assuming those
// channels are equivalent.
func TestAuthKeyHTTPRejectionCompletesLogin(t *testing.T) {
	for _, initial := range []bool{false, true} {
		name := "health-event"
		if initial {
			name = "health-before-watch"
		}
		t.Run(name, func(t *testing.T) { assertHTTPKeyRejection(t, initial) })
	}
}

func assertHTTPKeyRejection(t *testing.T, initial bool) {
	t.Helper()
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	node := newTestNode(t, ctx, control, "key-http-rejection")
	request := &agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("test-key-only-in-memory")}, RequestId: "http-key-rejection"}
	first, err := node.beginLogin(request)
	if err != nil {
		t.Fatal(err)
	}
	// RequireAuth leaves the test node waiting for interactive completion. The
	// noninteractive attempt must never expose that URL through NodeControl.
	awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.State == "needs_login" })
	node.opMu.Lock()
	node.mu.Lock()
	server := node.server
	if initial {
		// Deliberately miss the error event. A newly subscribed watcher must
		// recover the terminal error from the SDK's initial health snapshot.
		node.watchCancel()
		node.generation++
	}
	generation := node.generation
	lc := node.local
	node.mu.Unlock()
	server.Sys().HealthTracker.Get().SetAuthRoutineInError(errors.New("register request: http 400: invalid key: test-key-only-in-memory"))
	if initial {
		watchCtx, stop := context.WithCancel(ctx)
		defer stop()
		node.wg.Add(1)
		go node.watch(watchCtx, lc, generation)
	}
	node.opMu.Unlock()
	denied := awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.State == "denied" })
	if denied.ReasonCode != "INVALID_AUTH_KEY" || denied.LoginId != first.LoginId || denied.LoginUrl != "" || denied.LoginExpiresAt != 0 {
		t.Fatal("HTTP key rejection did not produce a sanitized terminal result")
	}
	replayed, err := node.beginLogin(request)
	if err != nil || replayed.State != "denied" || replayed.ReasonCode != "INVALID_AUTH_KEY" || replayed.LoginId != first.LoginId {
		t.Fatal("HTTP rejection was not retained for request deduplication")
	}
}

func TestAuthRejectionIgnoresNetworkAndUnrelatedHealth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		denied  bool
	}{
		{"legacy-control", "invalid authkey", true},
		{"control-detail", "invalid auth key: revoked", true},
		{"http-key", "register request: http 400: invalid key: already used", true},
		{"http-auth", "register request: http 401: invalid authkey", true},
		{"http-forbidden", "register request: http 403: invalid auth key", true},
		{"case-whitespace", "  INVALID KEY: revoked  ", true},
		{"empty", "", false},
		{"timeout", "register request: context deadline exceeded", false},
		{"network", "getNoiseClient: dial tcp: connection refused", false},
		{"proxy", "register request: http 407: invalid key: proxy credentials", false},
		{"rate-limit", "register request: http 429: invalid authkey", false},
		{"server", "register request: http 500: invalid authkey", false},
		{"unrelated-forbidden", "register request: http 403: access forbidden", false},
		{"ambiguous-key", "invalid key exchange", false},
		{"quoted-text", "dial https://invalid authkey: connection refused", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, notification := range []ipn.Notify{
				{ErrMessage: &tc.message},
				{Health: &health.State{Warnings: map[health.WarnableCode]health.UnhealthyState{
					tsconst.HealthWarnableLoginState: {Args: health.Args{health.ArgError: tc.message}},
				}}},
			} {
				if got := authKeyRejected(notification); got != tc.denied {
					t.Errorf("key rejection=%v, want %v", got, tc.denied)
				}
			}
		})
	}
	if authKeyRejected(ipn.Notify{Health: &health.State{Warnings: map[health.WarnableCode]health.UnhealthyState{
		"unrelated-warning": {Args: health.Args{health.ArgError: "invalid authkey"}},
	}}}) {
		t.Fatal("unrelated health warning was treated as login rejection")
	}
}

func TestKeyErrorDoesNotCompleteInteractiveLogin(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	node := newTestNode(t, ctx, control, "interactive-key-error")
	if _, err := node.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "interactive"}); err != nil {
		t.Fatal(err)
	}
	awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.LoginUrl != "" })
	node.mu.Lock()
	generation := node.generation
	node.mu.Unlock()
	if node.rejectAuthKey(generation) {
		t.Fatal("key error completed an interactive login")
	}
	if status := node.snapshot(); status.State == "denied" || status.LoginUrl == "" {
		t.Fatal("interactive attempt lost its pending login URL")
	}
}
