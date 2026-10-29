package agent

import (
	"context"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type cancellableRouteProbe struct {
	daemonProbe
	probe func(context.Context) (*pb.ProtocolAgreement, error)
}

// Propagate an actual parent deadline while withholding its scheduling hint,
// so the parent expires during the probe rather than exercising the independent
// fair-share sub-deadline.
type unannouncedDeadline struct{ context.Context }

func (unannouncedDeadline) Deadline() (time.Time, bool) { return time.Time{}, false }

func (p *cancellableRouteProbe) NegotiateProtocol(ctx context.Context, _ *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	return p.probe(ctx)
}

func TestCanceledRouteVerificationPreservesSharedTerminal(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			var block atomic.Bool
			entered := make(chan struct{}, 1)
			probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
			probe.probe = func(call context.Context) (*pb.ProtocolAgreement, error) {
				if block.CompareAndSwap(true, false) {
					entered <- struct{}{}
					<-call.Done()
					return nil, call.Err()
				}
				return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", Capabilities: []string{"terminal.v1"}}, nil
			}
			server := connect.NewServer()
			rpc.RegisterShikiDaemonHandler(server, probe)
			httpServer := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
			httpServer.EnableHTTP2 = true
			httpServer.StartTLS()
			defer httpServer.Close()
			ca := filepath.Join(t.TempDir(), "ca.pem")
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			s, r := openResourceService(t, ctx, resourceConfig(t))
			defer r.Close()
			record := storeRouteServer(t, s, &apb.ServerConfig{DirectTlsAddress: httpServer.Listener.Addr().String(), TlsServerName: "example.com", TlsCaFile: ca})
			client, err := r.Resolve(ctx, record.Config.Id)
			if err != nil {
				t.Fatal(err)
			}
			terminal, err := rpc.NewShikiDaemonClient(client).TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			if err := terminal.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess_shared"}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := terminal.Receive(); err != nil {
				t.Fatal(err)
			}

			// Hold the same lock as Resolve so maintenance cannot consume the
			// deliberately blocked verification before the canceled caller does.
			r.mu.Lock()
			pool := r.pools[record.Config.Id]
			pool.nextCheck = time.Now().Add(time.Hour)
			snapshot, err := r.store.View()
			if err != nil {
				r.mu.Unlock()
				t.Fatal(err)
			}
			call, cancel := context.WithCancel(ctx)
			if mode == "deadline" {
				cancel()
				call, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				call = unannouncedDeadline{call}
			}
			defer cancel()
			block.Store(true)
			go func() {
				select {
				case <-entered:
					if mode == "cancel" {
						cancel()
					}
				case <-ctx.Done():
				}
			}()
			_, err = r.resolveLocked(call, record.Config.Id, snapshot)
			preserved := r.pools[record.Config.Id] == pool && !pool.bad.Load() && pool.ctx.Err() == nil && pool.episode.attempts == 0 && pool.episode.retryAt.IsZero()
			r.mu.Unlock()
			want := connect.CodeCanceled
			if mode == "deadline" {
				want = connect.CodeDeadlineExceeded
			}
			if connect.CodeOf(err) != want {
				t.Fatalf("caller cancellation code = %v, want %v", err, want)
			}
			if !preserved {
				t.Fatal("caller cancellation invalidated the shared route or its retry budget")
			}
			if err := terminal.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("still-alive")}}); err != nil {
				t.Fatal("unrelated terminal canceled", err)
			}
			output, err := terminal.Receive()
			if err != nil || string(output.GetData()) != "still-alive" {
				t.Fatal("unrelated terminal no longer transfers data", output, err)
			}
			next, err := r.Resolve(ctx, record.Config.Id)
			if err != nil || next != client {
				t.Fatal("next independent request entered reconnect cooldown", err)
			}
		})
	}
}

func storeRouteServer(t *testing.T, s *Service, config *apb.ServerConfig) *apb.ServerRecord {
	t.Helper()
	config.Name = "route-test"
	record, err := s.UpsertServer(t.Context(), &apb.UpsertServerRequest{Config: config, RequestId: newID("request_")})
	if err != nil {
		t.Fatal(err)
	}
	record, err = s.SetServerCredential(t.Context(), &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte("shiki_route_fixture"), RequestId: newID("request_")})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestReconnectEpisodeExhaustsAndExplicitUpdateRestarts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var accepted atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			connection.Close() // Recoverable transport EOF during TLS handshake.
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	s, r := openResourceService(t, t.Context(), resourceConfig(t))
	defer r.Close()
	record := storeRouteServer(t, s, &apb.ServerConfig{DirectTlsAddress: listener.Addr().String(), TlsServerName: "localhost"})
	id := record.Config.Id
	if _, err := r.Resolve(t.Context(), id); err == nil {
		t.Fatal("broken endpoint admitted")
	}
	// A foreground RPC and the maintenance timer must share the cooldown.
	before := accepted.Load()
	for range 10 {
		_, _ = r.Resolve(t.Context(), id)
	}
	if accepted.Load() != before {
		t.Fatal("foreground calls bypassed reconnect cooldown")
	}
	for {
		r.mu.Lock()
		p := r.pools[id]
		if p.episode.exhausted {
			attempts := p.episode.attempts
			r.mu.Unlock()
			if attempts != 6 {
				t.Fatalf("attempts = %d", attempts)
			}
			break
		}
		if p.episode.stopped || p.episode.attempts == 0 {
			cause := p.episode.lastError
			r.mu.Unlock()
			t.Fatal("network failure did not enter bounded reconnect episode", cause)
		}
		// Advance only the schedule, leaving actual authenticated network probes
		// and the shared resource path intact; do not wait a minute in this test.
		p.episode.retryAt = time.Now().Add(-time.Second)
		r.mu.Unlock()
		_, _ = r.Resolve(t.Context(), id)
	}
	before = accepted.Load()
	for range 10 {
		_, _ = r.Resolve(t.Context(), id)
	}
	timer := time.NewTimer(600 * time.Millisecond)
	defer timer.Stop()
	<-timer.C // More than two real maintenance ticks.
	if accepted.Load() != before {
		t.Fatal("maintenance or foreground call restarted an exhausted episode")
	}
	state := findServer(t, s, id)
	if state.ReasonCode != "RECONNECT_EXHAUSTED" || state.ConnectionPath != "unknown" {
		t.Fatal("exhaustion/path not reported", state)
	}
	if _, err := s.UpsertServer(t.Context(), &apb.UpsertServerRequest{Config: state.Config, ExpectedRevision: state.Revision, RequestId: newID("request_")}); err != nil {
		t.Fatal(err)
	}
	_, _ = r.Resolve(t.Context(), id)
	if accepted.Load() <= before {
		t.Fatal("explicit server update did not start a new episode")
	}
}

func TestAuthenticationFailureWaitsForCredentialChangeAndPathIsRuntimeOnly(t *testing.T) {
	var calls atomic.Int32
	var denied atomic.Bool
	denied.Store(true)
	endpoint := routeEndpoint(t, func(ctx context.Context) (*pb.ProtocolAgreement, error) {
		calls.Add(1)
		if denied.Load() {
			return nil, rpcError(connect.CodeUnauthenticated, "REJECTED")
		}
		return routeAgreement(ctx)
	})
	s, r := openResourceService(t, t.Context(), resourceConfig(t))
	defer r.Close()
	record := storeRouteServer(t, s, endpoint)
	id := record.Config.Id
	if _, err := r.Resolve(t.Context(), id); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("authentication failure: %v", err)
	}
	before := calls.Load()
	denied.Store(false)
	for range 10 {
		_, _ = r.Resolve(t.Context(), id)
	}
	if calls.Load() != before {
		t.Fatal("authentication failure automatically retried the credential")
	}
	if _, err := s.SetServerCredential(t.Context(), &apb.SetServerCredentialRequest{ServerId: id, Token: []byte("shiki_route_fixture"), ExpectedCredentialRevision: record.CredentialRevision, RequestId: newID("request_")}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(t.Context(), id); err != nil {
		t.Fatal("credential update did not reauthenticate", err)
	}
	state := findServer(t, s, id)
	if state.ConnectionPath != "direct-tls" || state.Config.DaemonId != "daemon_route" || state.RemoteProtocol.GetDaemonId() != "daemon_route" {
		t.Fatal("authenticated path and identity not exposed", state)
	}
	snapshot, err := r.store.View()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := storedServer(snapshot, id)
	if err != nil || persisted.ConnectionPath != "" || persisted.RemoteProtocol != nil {
		t.Fatal("runtime route metadata persisted", persisted, err)
	}
}

func TestReconnectDelayBoundsAndCancellationDoNotBecomeTrustFailure(t *testing.T) {
	now := time.Now()
	var episode reconnectEpisode
	for attempt := 0; attempt < 5; attempt++ {
		episode.failed(now, remoteProbeTimeout{})
		delay := episode.retryAt.Sub(now)
		base := time.Second * time.Duration(1<<attempt)
		if delay < base*8/10 || delay > min(base*12/10, 30*time.Second) || episode.allowed(now) == nil || episode.allowed(episode.retryAt) != nil {
			t.Fatalf("invalid retry delay/eligibility: %s", delay)
		}
	}
	episode.failed(now, remoteProbeTimeout{})
	if !episode.exhausted || episode.allowed(now.Add(time.Hour)) == nil {
		t.Fatal("exhausted episode became eligible later")
	}
	var canceled reconnectEpisode
	canceled.failed(now, context.Canceled)
	if canceled.stopped || canceled.attempts != 0 || canceled.allowed(now.Add(time.Second)) != nil {
		t.Fatal("caller cancellation became a permanent remote failure")
	}
}
