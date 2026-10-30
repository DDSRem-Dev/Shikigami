package agent

import (
	"context"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestNetworkRetirementDuringProbeDoesNotBecomeTrustRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var block atomic.Bool
	entered := make(chan struct{})
	endpoint := routeEndpoint(t, func(call context.Context) (*pb.ProtocolAgreement, error) {
		if block.CompareAndSwap(true, false) {
			close(entered)
			<-call.Done()
			return nil, call.Err()
		}
		return routeAgreement(call)
	})
	service, resources := openResourceService(t, ctx, resourceConfig(t))
	defer resources.Close()
	record := storeRouteServer(t, service, endpoint)
	if _, err := resources.Resolve(ctx, record.Config.Id); err != nil {
		t.Fatal(err)
	}
	resources.mu.Lock()
	pool := resources.pools[record.Config.Id]
	pool.nextCheck = time.Now().Add(time.Hour)
	snapshot, err := resources.store.View()
	if err != nil {
		resources.mu.Unlock()
		t.Fatal(err)
	}
	block.Store(true)
	retired := make(chan struct{})
	go func() {
		defer close(retired)
		select {
		case <-entered:
			// A concurrent stream observes an actual TCP reset. Retiring that
			// shared pool cancels its still-pending authentication probe.
			pool.failed(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET})
		case <-ctx.Done():
		}
	}()
	_, err = resources.resolveLocked(ctx, record.Config.Id, snapshot)
	current := resources.pools[record.Config.Id]
	stopped, state, reason := current.episode.stopped, current.state, current.reason
	initialCode := connect.CodeOf(err)
	// Advance only the local recovery clock; no retry budget is reset and no
	// server/credential update or explicit connect operation is performed.
	current.episode.retryAt = time.Time{}
	_, recoveryErr := resources.resolveLocked(ctx, record.Config.Id, snapshot)
	recovered := resources.pools[record.Config.Id]
	ready := recovered != pool && recovered.state == "ready" && recovered.daemonID == "daemon_route"
	resources.mu.Unlock()
	<-retired
	if ctx.Err() != nil {
		t.Fatal("caller was unexpectedly canceled")
	}
	if stopped || state == "incompatible" {
		t.Fatalf("network-retired probe became terminal: state=%s reason=%s stopped=%t code=%s", state, reason, stopped, initialCode)
	}
	if recoveryErr != nil || !ready {
		t.Fatalf("fresh recovery did not authenticate a new pool: code=%s", connect.CodeOf(recoveryErr))
	}
	t.Logf("initial_code=%s state=%s stopped=%t fresh_pool_ready=%t", initialCode, state, stopped, ready)
}
