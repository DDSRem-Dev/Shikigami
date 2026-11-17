package agent

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func TestVerifiedRouteProbeUsesRemainingCallerBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
	probe.probe = func(context.Context) (*pb.ProtocolAgreement, error) {
		return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", Capabilities: []string{"terminal.v1"}}, nil
	}
	s, r := openResourceService(t, ctx, resourceConfig(t))
	defer r.Close()
	record := storeRouteServer(t, s, terminalProbeEndpoint(t, probe))
	client, err := r.Resolve(ctx, record.Config.Id)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan time.Time, 1)
	release := make(chan struct{})
	defer close(release)
	r.mu.Lock()
	original := r.pools[record.Config.Id]
	original.nextCheck = time.Now().Add(time.Hour)
	original.base = connect.NewClient(clientTransport{original.base}, func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			deadline, _ := ctx.Deadline()
			entered <- deadline
			select {
			case <-release:
				return next(ctx, spec)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	})
	r.mu.Unlock()
	call, stop := context.WithTimeout(ctx, 4*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() {
		next, err := r.Resolve(call, record.Config.Id)
		if err == nil && next != client {
			err = fmt.Errorf("successful verification replaced the shared client")
		}
		done <- err
	}()
	select {
	case deadline := <-entered:
		t.Logf("probe entered with %s remaining", time.Until(deadline))
	case <-call.Done():
		t.Fatal("identity probe never entered", call.Err())
	}
	// Release after the former half-budget expires, with 1.5 seconds still
	// available for authenticated HTTP/2 negotiation and the business call.
	deadline, _ := call.Deadline()
	timer := time.NewTimer(time.Until(deadline.Add(-1500 * time.Millisecond)))
	defer timer.Stop()
	select {
	case err := <-done:
		t.Fatalf("verified route abandoned a delayed identity check while its caller still had %s: %v", time.Until(deadline), err)
	case <-timer.C:
	}
	select {
	case release <- struct{}{}:
	case <-call.Done():
		t.Fatal("caller deadline elapsed before the fixture could release the probe", call.Err())
	}
	if err := <-done; err != nil {
		t.Fatal("identity check did not use the remaining caller budget", err)
	}
	terminal, err := rpc.NewShikiDaemonClient(client).TerminalStream(call)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	if err := terminal.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess_delayed"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.Receive(); err != nil {
		t.Fatal("business call failed inside the original caller budget", err)
	}
	if call.Err() != nil {
		t.Fatal("verification extended the caller budget", call.Err())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pools[record.Config.Id] != original || original.bad.Load() || original.episode.attempts != 0 || original.probeTimeouts != 0 {
		t.Fatal("successful delayed identity check changed the pool or failure budget")
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.count != 1 {
		t.Fatal("business operation was replayed", probe.count)
	}
}

// Exercise a real HTTP/2 stream while an independent identity probe exhausts
// its bounded deadline or the caller deadline. The cancellation-ordering case
// forces a local transport status. Both active and legitimately idle terminals must survive
// brief stalls; only sustained failure without authenticated progress retires
// their shared route. Trust rejections never receive this timeout grace.
func TestProbeTimeoutPreservesSharedTerminalUntilSustainedFailure(t *testing.T) {
	for _, outcome := range []string{"short callers repeat", "medium caller full probe", "local cancellation after deadline", "full success resets", "active progress resets", "idle sustained timeout", "authentication rejection", "identity rejection", "remote deadline", "remote cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			actions := make(chan string, 1)
			release := make(chan struct{})
			defer close(release) // Unblock every blackhole handler before server cleanup joins it.
			probe := &cancellableRouteProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}}
			probe.probe = func(context.Context) (*pb.ProtocolAgreement, error) {
				select {
				case action := <-actions:
					switch action {
					case "timeout":
						// Send no response, including a remote deadline error: this
						// scenario must exhaust the real client's local probe budget.
						<-release
						return nil, context.Canceled
					case "remote deadline":
						return nil, rpcError(connect.CodeDeadlineExceeded, "REMOTE_REQUEST_DEADLINE")
					case "remote cancellation":
						return nil, rpcError(connect.CodeCanceled, "REMOTE_REQUEST_CANCELED")
					case "authentication rejection":
						return nil, rpcError(connect.CodeUnauthenticated, "REJECTED")
					case "identity rejection":
						return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_other"}, nil
					}
				default:
				}
				return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", Capabilities: []string{"terminal.v1"}}, nil
			}
			config := terminalProbeEndpoint(t, probe)
			s, r := openResourceService(t, ctx, resourceConfig(t))
			defer r.Close()
			record := storeRouteServer(t, s, config)
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
			ready, err := terminal.Receive()
			if err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			original := r.pools[record.Config.Id]
			original.nextCheck = time.Now().Add(time.Hour)
			if outcome == "local cancellation after deadline" {
				// Force the transport cancellation ordering seen on CI: the
				// probe expires, but its transport reports local Canceled.
				// Keep the original real HTTP/2 terminal independently alive.
				original.base = connect.NewClient(clientTransport{original.base}, func(connect.ClientFunc) connect.ClientFunc {
					return func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
						<-ctx.Done()
						return nil, connect.NewError(connect.CodeCanceled, "transport cleanup canceled request")
					}
				})
			}
			r.mu.Unlock()
			resolve := func(action string, fullBudget bool) (*connect.Client, error) {
				t.Helper()
				// Serialize with maintenance before arming exactly one probe.
				r.mu.Lock()
				defer r.mu.Unlock()
				actions <- action
				call, stop := context.WithCancel(ctx)
				if outcome == "medium caller full probe" {
					stop()
					call, stop = context.WithTimeout(ctx, 6*time.Second)
				}
				if !fullBudget {
					stop()
					call, stop = context.WithTimeout(ctx, 400*time.Millisecond)
				}
				defer stop()
				snapshot, err := r.store.View()
				if err != nil {
					t.Fatal(err)
				}
				next, err := r.resolveLocked(call, record.Config.Id, snapshot)
				if fullBudget || action != "timeout" {
					if call.Err() != nil {
						t.Fatal("fixture exhausted parent deadline, not just the probe budget", call.Err())
					}
				} else if !errors.Is(call.Err(), context.DeadlineExceeded) {
					t.Fatal("short probe did not retain its caller deadline", call.Err())
				}
				return next, err
			}
			exchange := func(n int) {
				t.Helper()
				message := fmt.Sprintf("same-terminal-%d", n)
				if err := terminal.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte(message)}}); err != nil {
					t.Fatal("unrelated original terminal canceled", err)
				}
				frame, err := terminal.Receive()
				if err != nil || string(frame.GetData()) != message || frame.AttachmentId != ready.AttachmentId {
					t.Fatal("original terminal stopped exchanging data", frame, err)
				}
			}
			preserved := func() {
				t.Helper()
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.pools[record.Config.Id] != original || original.bad.Load() || original.ctx.Err() != nil || original.episode.attempts != 0 || !original.episode.retryAt.IsZero() {
					t.Fatal("isolated probe timeout replaced the shared pool or consumed its retry budget")
				}
			}
			fullBudget := outcome == "medium caller full probe" || outcome == "full success resets" || outcome == "active progress resets" || outcome == "idle sustained timeout"
			wantTimeout := connect.CodeDeadlineExceeded
			if fullBudget {
				wantTimeout = connect.CodeUnavailable
			}
			if next, err := resolve("timeout", fullBudget); next != nil || connect.CodeOf(err) != wantTimeout {
				t.Fatalf("first probe timeout must fail only its caller: client=%p error=%v original_bad=%v", next, err, original.bad.Load())
			}
			preserved()
			if outcome != "idle sustained timeout" && outcome != "full success resets" {
				exchange(1)
			}
			ageFailure := func() {
				t.Helper()
				r.mu.Lock()
				defer r.mu.Unlock()
				if original.probeFailedSince.IsZero() {
					t.Fatal("full-budget failure did not begin a recovery observation")
				}
				// Advance only the observation window. Probes still perform real
				// HTTP/2 calls and exhaust their actual five-second deadlines.
				original.probeFailedSince = time.Now().Add(-probeFailureRecoveryWindow)
			}
			switch outcome {
			case "medium caller full probe":
				r.mu.Lock()
				if original.probeTimeouts != 1 || original.probeFailedSince.IsZero() {
					t.Error("six-second caller did not admit a full five-second failure observation")
				}
				r.mu.Unlock()
			case "local cancellation after deadline":
				// The shared terminal and retry budget were checked above.
			case "short callers repeat":
				for n := 2; n <= 4; n++ {
					if next, err := resolve("timeout", false); next != nil || connect.CodeOf(err) != connect.CodeDeadlineExceeded {
						t.Fatal("short caller budgets were treated as route failure", err)
					}
					preserved()
					r.mu.Lock()
					if original.probeTimeouts != 0 || !original.probeFailedSince.IsZero() {
						t.Error("caller deadline consumed the route failure observation budget")
					}
					r.mu.Unlock()
					exchange(n)
				}
			case "full success resets":
				ageFailure()
				if next, err := resolve("success", true); err != nil || next != client {
					t.Fatal("successful verification replaced the route", err)
				}
				for n := 0; n < 2; n++ {
					if next, err := resolve("timeout", true); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
						t.Fatal("successful verification did not reset the failure sequence and observation window", err)
					}
					preserved()
				}
				exchange(2)
			case "active progress resets":
				for n := 2; n <= 3; n++ {
					ageFailure()
					if next, err := resolve("timeout", true); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
						t.Fatal("identity timeout ignored successful application traffic", err)
					}
					preserved()
					exchange(n)
				}
			case "idle sustained timeout":
				if next, err := resolve("timeout", false); next != nil || connect.CodeOf(err) != connect.CodeDeadlineExceeded {
					t.Fatal("short caller invalidated the route", err)
				}
				preserved()
				if next, err := resolve("timeout", true); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
					t.Fatal("two five-second stalls canceled a legitimately idle terminal", err)
				}
				preserved()
				ageFailure()
				next, err := resolve("timeout", true)
				if err != nil || next == nil || next == client || !original.bad.Load() || original.ctx.Err() == nil {
					t.Fatal("sustained no-progress probe failures did not replace the failed route", err)
				}
				probe.waitClosed(t, ready.AttachmentId)
				probe.mu.Lock()
				count := probe.count
				probe.mu.Unlock()
				if count != 1 {
					t.Fatal("route recovery replayed the terminal operation", count)
				}
			case "remote deadline", "remote cancellation":
				_, err := resolve(outcome, false)
				want := connect.CodeDeadlineExceeded
				if outcome == "remote cancellation" {
					want = connect.CodeCanceled
				}
				if connect.CodeOf(err) != want {
					t.Fatal("remote request error was not preserved", err)
				}
				preserved()
				exchange(2)
			default:
				_, err := resolve(outcome, false)
				want := connect.CodeUnauthenticated
				if outcome == "identity rejection" {
					want = connect.CodePermissionDenied
				}
				if connect.CodeOf(err) != want || !original.bad.Load() || original.ctx.Err() == nil {
					t.Fatal("probe timeout grace delayed a trust rejection", err)
				}
				probe.waitClosed(t, ready.AttachmentId)
			}
		})
	}
}

func TestProbeDeadlineDoesNotHideRemoteOrTrustErrors(t *testing.T) {
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	for _, err := range []error{
		connect.NewError(connect.CodeCanceled, "remote canceled").WithRemote(),
		connect.NewError(connect.CodeDeadlineExceeded, "remote deadline").WithRemote(),
		connect.NewError(connect.CodeUnauthenticated, "remote authentication").WithRemote(),
		connect.NewError(connect.CodeUnauthenticated, "authentication rejected"),
		connect.NewError(connect.CodePermissionDenied, "identity rejected"),
		connect.NewError(connect.CodeFailedPrecondition, "protocol incompatible"),
		x509.UnknownAuthorityError{},
	} {
		t.Run(err.Error(), func(t *testing.T) {
			if got := classifyProbeError(t.Context(), expired, err); got != err {
				t.Fatalf("probe deadline hid an independent rejection: got %v, want %v", got, err)
			}
		})
	}
	localCanceled := connect.NewError(connect.CodeCanceled, "local canceled")
	if got := classifyProbeError(t.Context(), t.Context(), localCanceled); got != localCanceled {
		t.Fatal("live probe cancellation became a timeout", got)
	}
	parent, cancel := context.WithCancel(t.Context())
	cancel()
	if got := classifyProbeError(parent, expired, localCanceled); !errors.Is(got, context.Canceled) {
		t.Fatal("probe deadline hid caller cancellation", got)
	}
	if got := classifyProbeError(expired, expired, localCanceled); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatal("probe deadline hid caller deadline", got)
	}
}

func terminalProbeEndpoint(t *testing.T, probe *cancellableRouteProbe) *apb.ServerConfig {
	t.Helper()
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, probe)
	httpServer := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	t.Cleanup(httpServer.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return &apb.ServerConfig{DirectTlsAddress: httpServer.Listener.Addr().String(), TlsServerName: "example.com", TlsCaFile: ca}
}
