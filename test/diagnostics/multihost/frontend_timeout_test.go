package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type timeoutAgent struct {
	arpc.UnimplementedAgentHandler
	next     atomic.Int64
	wait     atomic.Bool
	entered  chan struct{}
	canceled chan error
}

func (p *timeoutAgent) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: fmt.Sprint(p.next.Add(1)),
		Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"agent.control.v1"}}}, nil
}
func (*timeoutAgent) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (*timeoutAgent) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (p *timeoutAgent) ResolveConflict(ctx context.Context, _ *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	_, ok := ctx.Deadline()
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "missing caller deadline")
	}
	if p.wait.Load() {
		p.entered <- struct{}{}
		<-ctx.Done()
		p.canceled <- ctx.Err()
		return nil, ctx.Err()
	}
	return &ppb.SyncState{}, nil
}

type wireTimeoutObservation struct {
	header string
	at     time.Time
}

func TestFrontendExplicitTimeoutIsScopedAndPreservesEarlierCallerDeadline(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shiki-fb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	h := &helper{root: root, config: filepath.Join(root, "client.yaml"), fronts: map[string]*frontend.Client{}}
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "agent")
	cfg.Agent.AutoStart = false
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(root, "tailscale")
	cfg.PluginsDir = filepath.Join(root, "plugins")
	if err := config.SaveClient(h.config, cfg); err != nil {
		t.Fatal(err)
	}
	listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	probe := &timeoutAgent{entered: make(chan struct{}, 1), canceled: make(chan error, 1)}
	wire := make(chan wireTimeoutObservation, 8)
	router := connect.NewServer()
	arpc.RegisterAgentHandler(router, probe)
	lifetime, cancel := context.WithCancel(t.Context())
	handler := connection.NewRPCHandler(router)
	server := connection.NewHTTPServer(lifetime, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == arpc.AgentResolveConflictProcedure {
			wire <- wireTimeoutObservation{r.Header.Get("Connect-Timeout-Ms"), time.Now()}
			// Connect transports a relative budget and reconstructs the server
			// deadline at dispatch. Deliberately separate those instants: the
			// server's absolute deadline cannot prove the client's deadline.
			timer := time.NewTimer(25 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		for _, f := range h.fronts {
			_ = f.Close()
		}
		cancel()
		_ = server.Close()
		<-done
	})
	for _, test := range []struct {
		name      string
		seconds   int
		want      time.Duration
		parent    time.Duration
		openAgain bool
	}{
		{"default", 0, 5 * time.Second, 120 * time.Second, true},
		{"collision", 90, 90 * time.Second, 120 * time.Second, true},
		{"default", 0, 5 * time.Second, 120 * time.Second, false},
		{"short", 1, time.Second, 120 * time.Second, true},
		{"collision", 90, 2 * time.Second, 2 * time.Second, false},
	} {
		if test.openAgain {
			data, err := json.Marshal(map[string]any{"name": test.name, "timeout_seconds": test.seconds})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.frontendOp(t.Context(), request{Op: "frontend.open", Args: data}); err != nil {
				t.Fatal(err)
			}
		}
		f, err := h.front(t.Context(), test.name)
		if err != nil {
			t.Fatal(err)
		}
		call, stop := context.WithTimeout(t.Context(), test.parent)
		started := time.Now()
		parentDeadline, _ := call.Deadline()
		_, err = f.Agent().ResolveConflict(call, &ppb.ResolveConflictRequest{})
		stop()
		if err != nil {
			t.Fatal(err)
		}
		observation := <-wire
		millis, err := strconv.ParseInt(observation.header, 10, 64)
		if err != nil || millis <= 0 {
			t.Fatalf("frontend %s invalid wire timeout %q", test.name, observation.header)
		}
		budget := time.Duration(millis) * time.Millisecond
		maximum := min(test.want, parentDeadline.Sub(started))
		// Serialization may occur anywhere between call start and ingress.
		// Connect rounds down to milliseconds, with a minimum of one.
		minimum := max(time.Millisecond, maximum-observation.at.Sub(started)-time.Millisecond)
		if budget > max(time.Millisecond, maximum) || budget < minimum {
			t.Fatalf("frontend %s wire budget=%v expected [%v, %v]", test.name, budget, minimum, maximum)
		}
	}

	// The configured 90-second frontend must retain caller cancellation on a
	// real in-flight request with an earlier deadline. Synchronize with handler
	// admission instead of assuming dispatch completes within a short timeout.
	probe.wait.Store(true)
	f, err := h.front(t.Context(), "collision")
	if err != nil {
		t.Fatal(err)
	}
	call, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	returned := make(chan error, 1)
	go func() {
		_, err := f.Agent().ResolveConflict(call, &ppb.ResolveConflictRequest{})
		returned <- err
	}()
	select {
	case <-probe.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not admit the cancellation probe")
	}
	stop()
	select {
	case err := <-returned:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("caller cancellation did not end RPC: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RPC continued after caller cancellation")
	}
	select {
	case err := <-probe.canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("server request was not canceled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server retained the canceled request")
	}
}

func TestFrontendTimeoutRejectsUnboundedAndInvalidValuesBeforeOpening(t *testing.T) {
	for _, value := range []string{"-1", "91", "1.5", `"90"`, "999999999999999999999999999999999"} {
		h := &helper{fronts: map[string]*frontend.Client{}}
		_, err := h.frontendOp(t.Context(), request{Op: "frontend.open", Args: json.RawMessage(`{"name":"collision","timeout_seconds":` + value + `}`)})
		if connect.CodeOf(err) != connect.CodeInvalidArgument || len(h.fronts) != 0 {
			t.Fatalf("accepted invalid timeout %s: %v", value, err)
		}
	}
}
