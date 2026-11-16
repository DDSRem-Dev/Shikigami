package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

type probeResolveResult struct {
	client *connect.Client
	record *apb.ServerRecord
	err    error
}

// Concurrent short calls should pay for one authenticated HTTP/2 handshake,
// rather than queue a separate round trip for every caller. A later independent
// call must still probe, including when it immediately follows the shared one.
func TestRemoteProbeCoalescesOnlyOverlappingSuccessfulCalls(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold identity pin"
		if warm {
			name = "existing identity"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var calls atomic.Int64
			var block atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			config := routeEndpoint(t, func(call context.Context) (*pb.ProtocolAgreement, error) {
				calls.Add(1)
				if block.CompareAndSwap(true, false) {
					close(entered)
					select {
					case <-release:
					case <-call.Done():
						return nil, call.Err()
					}
				}
				return routeAgreement(call)
			})
			service, resources := openResourceService(t, ctx, resourceConfig(t))
			defer resources.Close()
			record := storeRouteServer(t, service, config)
			if warm {
				if _, err := resources.Resolve(ctx, record.Config.Id); err != nil {
					t.Fatal(err)
				}
				resources.mu.Lock()
				resources.pools[record.Config.Id].nextCheck = time.Now().Add(time.Hour)
				resources.mu.Unlock()
			}
			before := calls.Load()
			block.Store(true)
			resolve := func(call context.Context, out chan<- probeResolveResult) {
				client, snapshot, err := resources.ResolveSnapshot(call, record.Config.Id)
				out <- probeResolveResult{client, snapshot, err}
			}
			const waiters = 8
			results := make(chan probeResolveResult, waiters+1)
			go resolve(ctx, results)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("owner did not start its HTTP/2 probe")
			}
			for range waiters {
				waiter := &observedProbeWaitContext{Context: ctx, observed: make(chan struct{})}
				go resolve(waiter, results)
				select {
				case <-waiter.observed:
				case <-ctx.Done():
					t.Fatal("caller did not join the pending probe")
				}
			}
			releaseOnce.Do(func() { close(release) })
			var shared *connect.Client
			for range waiters + 1 {
				select {
				case result := <-results:
					if result.err != nil || result.client == nil || result.record.GetConfig().GetDaemonId() != "daemon_route" {
						t.Fatalf("overlapping caller did not receive the verified identity: %v", result.err)
					}
					if shared != nil && shared != result.client {
						t.Fatal("overlapping calls did not share their authenticated pool")
					}
					shared = result.client
				case <-ctx.Done():
					t.Fatal("overlapping callers did not complete")
				}
			}
			if got := calls.Load() - before; got != 1 {
				t.Fatalf("overlapping callers made %d probes, want one", got)
			}
			if _, err := resources.Resolve(ctx, record.Config.Id); err != nil {
				t.Fatal(err)
			}
			if got := calls.Load() - before; got != 2 {
				t.Fatalf("independent later call reused a completed probe: %d", got)
			}
		})
	}
}

// Pause a waiter after it has observed an active flight but before it can
// consume completion. This tests superseding probes without scheduler sleeps.
type pausedProbeWaitContext struct {
	context.Context
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (c *pausedProbeWaitContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.entered)
		select {
		case <-c.resume:
		case <-c.Context.Done():
		}
	})
	return c.Context.Done()
}

func TestRemoteProbeWaiterRejectsSupersededSuccess(t *testing.T) {
	for _, change := range []string{"later failed probe", "credential replacement"} {
		t.Run(change, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var calls atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			config := routeEndpoint(t, func(call context.Context) (*pb.ProtocolAgreement, error) {
				switch calls.Add(1) {
				case 2:
					close(entered)
					select {
					case <-release:
					case <-call.Done():
						return nil, call.Err()
					}
				case 3:
					return nil, rpcError(connect.CodeDeadlineExceeded, "REQUEST_DEADLINE")
				case 4:
					return nil, rpcError(connect.CodeUnauthenticated, "CREDENTIAL_REVOKED")
				}
				return routeAgreement(call)
			})
			service, resources := openResourceService(t, ctx, resourceConfig(t))
			defer resources.Close()
			record := storeRouteServer(t, service, config)
			if _, err := resources.Resolve(ctx, record.Config.Id); err != nil {
				t.Fatal(err)
			}
			resources.mu.Lock()
			resources.pools[record.Config.Id].nextCheck = time.Now().Add(time.Hour)
			resources.mu.Unlock()
			ownerDone := make(chan error, 1)
			go func() { _, err := resources.Resolve(ctx, record.Config.Id); ownerDone <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("owner did not start")
			}
			waiter := &pausedProbeWaitContext{Context: ctx, entered: make(chan struct{}), resume: make(chan struct{})}
			waiterDone := make(chan probeResolveResult, 1)
			go func() {
				client, record, err := resources.ResolveSnapshot(waiter, record.Config.Id)
				waiterDone <- probeResolveResult{client, record, err}
			}()
			select {
			case <-waiter.entered:
			case <-ctx.Done():
				t.Fatal("waiter did not join owner")
			}
			releaseOnce.Do(func() { close(release) })
			if err := <-ownerDone; err != nil {
				t.Fatal(err)
			}
			if change == "later failed probe" {
				if _, err := resources.Resolve(ctx, record.Config.Id); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
					t.Fatalf("expected intervening request-scoped failure: %v", err)
				}
			} else {
				if _, err := service.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, ExpectedCredentialRevision: record.CredentialRevision, Token: []byte("shiki_changed_credential"), RequestId: newID("request_")}); err != nil {
					t.Fatal(err)
				}
			}
			close(waiter.resume)
			select {
			case result := <-waiterDone:
				if result.client != nil || connect.CodeOf(result.err) != connect.CodeUnauthenticated {
					t.Fatalf("waiter reused a stale successful flight: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("waiter did not complete")
			}
		})
	}
}
