package agent

import (
	"context"
	"encoding/pem"
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

// Exercise a real HTTP/2 stream while an independent identity probe exhausts
// its own fair-share deadline. No parent cancellation or transport error is
// injected: the existing authenticated terminal continues to exchange bytes.
func TestProbeTimeoutPreservesSharedTerminalUntilConsecutiveFailure(t *testing.T) {
	for _, outcome := range []string{"short callers repeat", "full success resets", "full consecutive timeout", "authentication rejection", "identity rejection", "remote deadline", "remote cancellation"} {
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
			r.mu.Unlock()
			resolve := func(action string, fullBudget bool) (*connect.Client, error) {
				t.Helper()
				// Serialize with maintenance before arming exactly one probe.
				r.mu.Lock()
				defer r.mu.Unlock()
				actions <- action
				call, stop := context.WithCancel(ctx)
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
				if call.Err() != nil {
					t.Fatal("fixture exhausted parent deadline, not just the probe budget", call.Err())
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
			fullBudget := outcome == "full success resets" || outcome == "full consecutive timeout"
			if next, err := resolve("timeout", fullBudget); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatalf("first probe timeout must fail only its caller: client=%p error=%v original_bad=%v", next, err, original.bad.Load())
			}
			preserved()
			exchange(1)
			switch outcome {
			case "short callers repeat":
				for n := 2; n <= 4; n++ {
					if next, err := resolve("timeout", false); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
						t.Fatal("short caller budgets were treated as route failure", err)
					}
					preserved()
					exchange(n)
				}
			case "full success resets":
				if next, err := resolve("success", true); err != nil || next != client {
					t.Fatal("successful verification replaced the route", err)
				}
				if next, err := resolve("timeout", true); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
					t.Fatal("successful verification did not reset the failure sequence", err)
				}
				preserved()
				exchange(2)
			case "full consecutive timeout":
				if next, err := resolve("timeout", false); next != nil || connect.CodeOf(err) != connect.CodeUnavailable {
					t.Fatal("short caller invalidated the route", err)
				}
				preserved()
				exchange(2)
				next, err := resolve("timeout", true)
				if err != nil || next == nil || next == client || !original.bad.Load() || original.ctx.Err() == nil {
					t.Fatal("consecutive probe failures did not replace the failed route", err)
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
