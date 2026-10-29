package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

// A real blocked HTTP/2 authentication probe must not block unrelated local
// reads or credential/configuration replacement. A result authenticated using
// the replaced generation must never be returned as the new generation.
func TestRemoteProbeDoesNotLockLocalResources(t *testing.T) {
	for _, change := range []string{"read", "credential", "address", "remove"} {
		t.Run(change, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var block atomic.Bool
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
			probe.probe = func(call context.Context) (*pb.ProtocolAgreement, error) {
				if block.CompareAndSwap(true, false) {
					entered <- struct{}{}
					select {
					case <-release:
					case <-call.Done():
						return nil, call.Err()
					}
				}
				return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", Capabilities: []string{"terminal.v1"}}, nil
			}
			config := terminalProbeEndpoint(t, probe)
			s, r := openResourceService(t, ctx, resourceConfig(t))
			defer r.Close()
			// Release before Close even when the old implementation fails this test.
			defer close(release)
			record := storeRouteServer(t, s, config)
			original, err := r.Resolve(ctx, record.Config.Id)
			if err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			r.pools[record.Config.Id].nextCheck = time.Now().Add(time.Hour)
			snapshot, err := r.store.View()
			if err == nil {
				record, err = storedServer(snapshot, record.Config.Id)
			}
			r.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			block.Store(true)
			type resolved struct {
				client *connect.Client
				err    error
			}
			result := make(chan resolved, 1)
			go func() { client, err := r.Resolve(ctx, record.Config.Id); result <- resolved{client, err} }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("probe never entered", ctx.Err())
			}
			changed := make(chan error, 1)
			go func() {
				var err error
				switch change {
				case "read":
					_, err = s.GetPreferences(ctx, &pb.Empty{})
				case "credential":
					_, err = s.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, ExpectedCredentialRevision: record.CredentialRevision, Token: []byte("shiki_replacement_fixture"), RequestId: newID("request_")})
				case "address":
					next := proto.Clone(record.Config).(*apb.ServerConfig)
					next.Name = "updated-during-probe"
					next.DirectTlsAddress = "127.0.0.1:1"
					_, err = s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: next, ExpectedRevision: record.Revision, RequestId: newID("request_")})
				case "remove":
					_, err = s.RemoveServer(ctx, &apb.RemoveServerRequest{ServerId: record.Config.Id, ExpectedRevision: record.Revision, RequestId: newID("request_")})
				}
				changed <- err
			}()
			select {
			case err := <-changed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked remote authentication holds the local resource lock")
			}
			if change == "read" {
				// The independent local read succeeds while the network handler remains blocked.
				select {
				case got := <-result:
					t.Fatalf("probe completed before release: %v", got.err)
				default:
				}
				cancel()
			}
			select {
			case got := <-result:
				if got.err == nil || got.client != nil {
					t.Fatal("obsolete/canceled probe exposed a client", got.client == original, got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("replacing/canceling a probe did not terminate it")
			}
		})
	}
}

func TestColdRemoteHandshakeAllowsCredentialReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	entered := make(chan struct{}, 8)
	probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
	probe.probe = func(call context.Context) (*pb.ProtocolAgreement, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-call.Done()
		return nil, call.Err()
	}
	config := terminalProbeEndpoint(t, probe)
	s, r := openResourceService(t, ctx, resourceConfig(t))
	defer r.Close()
	defer cancel()
	record := storeRouteServer(t, s, config)
	result := make(chan error, 1)
	go func() {
		client, err := r.Resolve(ctx, record.Config.Id)
		if client != nil {
			result <- fmt.Errorf("cold handshake exposed an unauthenticated client")
			return
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("cold handshake never entered")
	}
	updated := make(chan *apb.ServerRecord, 1)
	failure := make(chan error, 1)
	go func() {
		if _, err := s.GetPreferences(ctx, &pb.Empty{}); err != nil {
			failure <- err
			return
		}
		record, err := s.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, ExpectedCredentialRevision: record.CredentialRevision, Token: []byte("shiki_cold_replacement"), RequestId: newID("request_")})
		if err != nil {
			failure <- err
			return
		}
		updated <- record
	}()
	select {
	case replacement := <-updated:
		if replacement.CredentialRevision == record.CredentialRevision || replacement.Config.DaemonId != "" {
			t.Fatal("cold handshake published an obsolete identity or credential")
		}
	case err := <-failure:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("cold handshake blocked credential replacement")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cold handshake did not stop")
	}
	snapshot, err := r.store.View()
	if err != nil {
		t.Fatal(err)
	}
	current, err := storedServer(snapshot, record.Config.Id)
	if err != nil || current.Config.DaemonId != "" {
		t.Fatal("failed handshake persisted an identity pin", current, err)
	}
}

// Observe a waiter reaching the cancellable gate without timing sleeps or an
// implementation hook. No transport is created for the waiting request.
type observedProbeWaitContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedProbeWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestRemoteProbeWaiterCancellationPreservesOwner(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint("shutdown=", shutdown), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var block atomic.Bool
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
			probe.probe = func(call context.Context) (*pb.ProtocolAgreement, error) {
				if block.CompareAndSwap(true, false) {
					entered <- struct{}{}
					select {
					case <-release:
					case <-call.Done():
						return nil, call.Err()
					}
				}
				return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", Capabilities: []string{"terminal.v1"}}, nil
			}
			s, r := openResourceService(t, ctx, resourceConfig(t))
			defer r.Close()
			defer close(release)
			record := storeRouteServer(t, s, terminalProbeEndpoint(t, probe))
			original, err := r.Resolve(ctx, record.Config.Id)
			if err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			pool := r.pools[record.Config.Id]
			pool.nextCheck = time.Now().Add(time.Hour)
			r.mu.Unlock()
			block.Store(true)
			ownerDone := make(chan error, 1)
			go func() { _, err := r.Resolve(ctx, record.Config.Id); ownerDone <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("owner never entered")
			}
			waitCtx, stopWait := context.WithCancel(ctx)
			defer stopWait()
			waiter := &observedProbeWaitContext{Context: waitCtx, observed: make(chan struct{})}
			waiterDone := make(chan error, 1)
			go func() { _, err := r.Resolve(waiter, record.Config.Id); waiterDone <- err }()
			select {
			case <-waiter.observed:
			case <-ctx.Done():
				t.Fatal("waiter never reached gate")
			}
			if shutdown {
				cancel()
			} else {
				stopWait()
			}
			select {
			case err := <-waiterDone:
				if err == nil {
					t.Fatal("canceled waiter succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("waiter cancellation did not release it")
			}
			if shutdown {
				select {
				case err := <-ownerDone:
					if err == nil {
						t.Fatal("shutdown probe succeeded")
					}
				case <-time.After(time.Second):
					t.Fatal("shutdown did not cancel active authentication")
				}
				r.mu.Lock()
				waiting := pool.resolving != nil
				r.mu.Unlock()
				if waiting {
					t.Fatal("shutdown left authentication gate occupied")
				}
			} else {
				r.mu.Lock()
				preserved := r.pools[record.Config.Id] == pool && !pool.bad.Load() && pool.client == original && pool.resolving != nil
				r.mu.Unlock()
				if !preserved {
					t.Fatal("waiter cancellation changed owner or shared route")
				}
				select {
				case err := <-ownerDone:
					t.Fatal("waiter canceled active owner", err)
				default:
				}
				// Cancel the owner's separate lifetime only after proving it survived.
				cancel()
				select {
				case <-ownerDone:
				case <-time.After(time.Second):
					t.Fatal("owner cleanup did not finish")
				}
			}
		})
	}
}
