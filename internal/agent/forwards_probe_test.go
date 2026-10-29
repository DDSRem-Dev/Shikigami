package agent

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

// This is a host runtime-state regression, not a real plugin/TCP acceptance.
// Two observed listener states make a stale monitor failure distinguishable
// from a failure of the listener's actual connection generation.
func TestProbeFailureSuspendsOnlyRetiredForwardGenerations(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx := context.Background()
	if _, err := r.Resolve(ctx, server.Config.Id); err != nil {
		t.Fatal(err)
	}
	var states []*ppb.ForwardState
	for index, requestID := range []string{"old-forward", "replacement-forward"} {
		request := createRequest(server.Config.Id)
		request.RequestId = requestID
		request.Spec.RemotePort += uint32(index)
		state, err := s.CreateForward(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		state = proto.Clone(state).(*ppb.ForwardState)
		state.State, state.ReasonCode = "listening", ""
		state.BoundPort, state.Connections = uint32(21001+index), 1
		states = append(states, state)
	}
	r.forwardMu.Lock()
	r.mu.Lock()
	original := r.pools[server.Config.Id]
	original.nextCheck = time.Now().Add(time.Hour)
	r.mu.Unlock()
	for _, state := range states {
		r.forwardRoutes[state.Spec.Id] = original
		r.forwardStates.Store(state.Spec.Id, state)
	}
	r.forwardMu.Unlock()
	assertStates := func(firstRetired bool) {
		t.Helper()
		for _, state := range states {
			cached, ok := r.forwardStates.Load(state.Spec.Id)
			if !ok {
				t.Fatal("missing runtime observation", state.Spec.Id)
			}
			got := cached.(*ppb.ForwardState)
			want := states[1]
			if got.Spec.Id == states[0].Spec.Id {
				want = states[0]
				if firstRetired {
					want = proto.Clone(want).(*ppb.ForwardState)
					want.State, want.ReasonCode = "offline", "REMOTE_UNAVAILABLE"
					want.BoundPort, want.Connections = 0, 0
				}
			}
			if !proto.Equal(got, want) {
				t.Fatal("monitor suspended the wrong forward generation", got, want)
			}
		}
	}
	cause := rpcError(connect.CodeUnavailable, "REMOTE_RPC_FAILED")
	suspend := func() {
		t.Helper()
		r.forwardMu.Lock()
		defer r.forwardMu.Unlock()
		var observed []*ppb.ForwardState
		for _, state := range states {
			cached, _ := r.forwardStates.Load(state.Spec.Id)
			observed = append(observed, proto.Clone(cached.(*ppb.ForwardState)).(*ppb.ForwardState))
		}
		r.suspendForwardStates(ctx, server.Config.Id, cause, observed)
	}
	suspend()
	assertStates(false)

	r.mu.Lock()
	r.retireLocked(server.Config.Id)
	r.mu.Unlock()
	if _, err := r.Resolve(ctx, server.Config.Id); err != nil {
		t.Fatal(err)
	}
	r.forwardMu.Lock()
	r.mu.Lock()
	replacement := r.pools[server.Config.Id]
	replacement.nextCheck = time.Now().Add(time.Hour)
	r.mu.Unlock()
	r.forwardRoutes[states[1].Spec.Id] = replacement
	r.forwardMu.Unlock()
	// A delayed failure for the old pool must stop the old listener, while
	// preserving a listener already restored on the new healthy generation.
	suspend()
	assertStates(true)
}
