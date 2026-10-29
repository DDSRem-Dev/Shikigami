package server

import (
	"bytes"
	"context"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func probePort(t *testing.T, client *connect.Client, ctx context.Context, method string) error {
	t.Helper()
	api := rpc.NewShikiDaemonClient(client)
	switch method {
	case "ListPorts":
		_, err := api.ListPorts(ctx, &pb.ListPortsRequest{})
		return err
	case "CheckPort":
		_, err := api.CheckPort(ctx, &pb.CheckPortRequest{})
		return err
	case "WatchPorts":
		stream, err := api.WatchPorts(ctx, &pb.WatchPortsRequest{})
		if err != nil {
			return err
		}
		defer stream.Close()
		_, err = stream.Receive()
		return err
	}
	t.Fatal("unknown port probe")
	return nil
}

func TestPortAdmissionSharesBudgetBeforeFirstFrame(t *testing.T) {
	for _, method := range []string{"ListPorts", "CheckPort", "WatchPorts"} {
		t.Run(method, func(t *testing.T) {
			f := newAdmissionFixture(t)
			second := f.client(t, "*")
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			var held []*silentAdmissionCall
			for range 32 {
				held = append(held, f.silentRequest(t, ctx, method, ""))
			}
			for _, call := range held {
				select {
				case <-call.done:
					t.Fatal("silent port first-frame request already ended", call.err, string(call.body))
				default:
				}
			}
			var observed error
			until := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(until) {
				observed = probePort(t, second, ctx, method)
				if connect.CodeOf(observed) == connect.CodeResourceExhausted {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if connect.CodeOf(observed) != connect.CodeResourceExhausted {
				t.Fatalf("33rd %s on independent HTTP2 connection/token bypassed host first-frame budget: %v", method, observed)
			}
			f.mu.Lock()
			connections := len(f.peers)
			f.mu.Unlock()
			if connections < 2 {
				t.Fatal("fixture did not create independent connections")
			}
		})
	}
}

func TestPortAdmissionIndependentCategoriesAndReclamation(t *testing.T) {
	for _, method := range []string{"ListPorts", "WatchPorts"} {
		t.Run(method, func(t *testing.T) {
			f := newAdmissionFixture(t)
			client := f.client(t, "*")
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			counter, maximum := f.service.portCalls.budget("/shiki.v1.ShikiDaemon/" + method)
			var held []*silentAdmissionCall
			for range maximum {
				held = append(held, f.silentRequest(t, ctx, method, ""))
			}
			waitFileCount(t, counter, maximum)
			for _, probe := range []string{"ListPorts", "CheckPort", "WatchPorts"} {
				other, _ := f.service.portCalls.budget("/shiki.v1.ShikiDaemon/" + probe)
				want := connect.CodeFailedPrecondition
				if other == counter {
					want = connect.CodeResourceExhausted
				}
				if err := probePort(t, client, ctx, probe); connect.CodeOf(err) != want {
					t.Fatalf("%s under saturated %s: %v", probe, method, err)
				}
			}
			if err := probeFile(t, client, ctx, "ListFiles"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("port budget blocked files", err)
			}
			api := rpc.NewShikiDaemonClient(client)
			forward, err := api.ForwardPort(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_ = forward.Send(&pb.PortData{})
			_, err = forward.Receive()
			_ = forward.Close()
			if connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("port metadata budget blocked independent forwarding", err)
			}
			terminal, err := api.TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_ = terminal.Send(&pb.TerminalInput{})
			_, err = terminal.Receive()
			_ = terminal.Close()
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatal("port budget blocked terminal", err)
			}
			for _, call := range held {
				call.cancel()
				_ = call.writer.Close()
			}
			waitFileCount(t, counter, 0)
			if err := probePort(t, client, ctx, method); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("released port slot was not reusable", err)
			}
		})
	}
}

func TestPortAdmissionAuthorizationAndSharedHandlers(t *testing.T) {
	f := newAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	for range limits.PortMetadataCalls {
		f.silentRequest(t, ctx, "ListPorts", "")
	}
	waitFileCount(t, &f.service.portCalls.metadata, limits.PortMetadataCalls)
	denied := f.client(t, "system.info")
	if err := probePort(t, denied, ctx, "ListPorts"); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("port gate revealed before authorization", err)
	}
	other := httptest.NewUnstartedServer(f.service.Handler())
	other.EnableHTTP2 = true
	other.StartTLS()
	defer other.Close()
	second := (&admissionFixture{auth: f.auth, service: f.service, remote: other}).client(t, "*")
	if err := probePort(t, second, ctx, "CheckPort"); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("same Service Handler reset port budget", err)
	}
	if _, err := rpc.NewShikiDaemonClient(denied).HealthCheck(ctx, &pb.Empty{}); err != nil {
		t.Fatal("port budget blocked independent health", err)
	}
}

func TestPortAdmissionFirstFrameDeadlineAndCancellation(t *testing.T) {
	t.Run("default-five-seconds", func(t *testing.T) {
		f := newAdmissionFixture(t)
		started := time.Now()
		call := f.silentRequest(t, t.Context(), "ListPorts", "")
		waitFileCount(t, &f.service.portCalls.metadata, 1)
		select {
		case <-call.done:
		case <-time.After(7 * time.Second):
			t.Fatal("port first-frame Receive did not unblock")
		}
		if elapsed := time.Since(started); elapsed < limits.PortFirstFrameTimeout || elapsed > 7*time.Second {
			t.Fatal("wrong default deadline", elapsed)
		}
		if call.err != nil || !bytes.Contains(call.body, []byte("deadline_exceeded")) || !bytes.Contains(call.body, []byte("PORT_FIRST_FRAME_DEADLINE")) {
			t.Fatalf("port deadline result: %v %q", call.err, call.body)
		}
		waitFileCount(t, &f.service.portCalls.metadata, 0)
		if err := probePort(t, f.client(t, "*"), t.Context(), "CheckPort"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("deadline retained port budget", err)
		}
	})
	t.Run("earlier-wire-parent", func(t *testing.T) {
		f := newAdmissionFixture(t)
		call := f.silentRequest(t, t.Context(), "WatchPorts", "120")
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Fatal("earlier port parent deadline left Receive blocked")
		}
		if call.err != nil || !bytes.Contains(call.body, []byte("deadline_exceeded")) {
			t.Fatalf("port parent deadline: %v %q", call.err, call.body)
		}
		waitFileCount(t, &f.service.portCalls.watches, 0)
	})
	t.Run("cancellation", func(t *testing.T) {
		f := newAdmissionFixture(t)
		call := f.silentRequest(t, t.Context(), "CheckPort", "")
		waitFileCount(t, &f.service.portCalls.metadata, 1)
		call.cancel()
		waitFileCount(t, &f.service.portCalls.metadata, 0)
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Fatal("canceled port request helper did not join")
		}
	})
}
