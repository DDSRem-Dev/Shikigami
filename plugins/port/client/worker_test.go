package client

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type dummyCore struct{ rpc.CoreAPIClient }

func worker(t *testing.T, fallback bool) *Worker {
	t.Helper()
	w := New()
	cfg, _ := structpb.NewStruct(map[string]any{"fallback_port_on_conflict": fallback})
	if err := w.Init(context.Background(), sdk.InitContext{Core: dummyCore{}, Side: "client", StateDir: t.TempDir(), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := w.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return w
}
func TestListenerBindingIdempotenceAndExplicitFallback(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	spec := &ppb.ForwardSpec{Id: "forward", ServerId: "server", RemotePort: 3000, LocalPort: uint32(occupied.Addr().(*net.TCPAddr).Port), Enabled: true}
	w := worker(t, false)
	if _, err = w.StartForward(context.Background(), spec); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("binding conflict not reported", err)
	}
	w = worker(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	state, err := w.StartForward(ctx, spec)
	cancel()
	if err != nil || state.BoundPort == 0 || state.BoundPort == spec.LocalPort {
		t.Fatal(state, err)
	}
	duplicate, err := w.StartForward(context.Background(), spec)
	if err != nil || duplicate.BoundPort != state.BoundPort {
		t.Fatal("duplicate acquired another listener", duplicate, err)
	}
	changed := proto.Clone(spec).(*ppb.ForwardSpec)
	changed.RemotePort++
	if _, err = w.StartForward(context.Background(), changed); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatal("active spec overwritten", err)
	}
	list, err := w.ListForwards(context.Background())
	if err != nil || len(list.Forwards) != 1 || list.Forwards[0].State != "listening" {
		t.Fatal("caller cancellation stopped listener", list, err)
	}
	if err = w.StopForward(context.Background(), &ppb.ForwardID{Id: spec.Id}); err != nil {
		t.Fatal(err)
	}
	list, _ = w.ListForwards(context.Background())
	if list.Forwards[0].BoundPort != 0 || list.Forwards[0].Connections != 0 || list.Forwards[0].State != "stopped" {
		t.Fatal(list)
	}
	if err = w.StopForward(context.Background(), &ppb.ForwardID{Id: spec.Id}); err != nil {
		t.Fatal(err)
	}
}
func TestListenersRejectNonloopback(t *testing.T) {
	w := worker(t, false)
	for _, address := range []string{"0.0.0.0", "192.168.0.1", "localhost", "::"} {
		_, err := w.StartForward(context.Background(), &ppb.ForwardSpec{Id: "forward", ServerId: "server", RemotePort: 3000, LocalAddress: address, Enabled: true})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal(address, err)
		}
	}
}

func TestConfiguredBindDefaultIsApplied(t *testing.T) {
	w := New()
	cfg, _ := structpb.NewStruct(map[string]any{"auto_forward": false, "bind_address": "::1"})
	if err := w.Init(context.Background(), sdk.InitContext{Core: dummyCore{}, Side: "client", StateDir: t.TempDir(), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop(context.Background())
	state, err := w.StartForward(context.Background(), &ppb.ForwardSpec{Id: "configured", ServerId: "server", RemotePort: 3000, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if state.Spec.LocalAddress != "::1" {
		t.Fatal("configured default ignored", state)
	}
}

func TestCanceledLifetimeCannotReportReadyOrBind(t *testing.T) {
	w := New()
	if err := w.Init(context.Background(), sdk.InitContext{Core: dummyCore{}, Side: "client", StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	health, err := w.Health(context.Background())
	if err != nil || health.Status == 0 {
		t.Fatal("canceled lifetime reported healthy", health, err)
	}
	_, err = w.StartForward(context.Background(), &ppb.ForwardSpec{Id: "ended", ServerId: "server", RemotePort: 3000, Enabled: true})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("bound listener after lifetime ended", err)
	}
	if err = w.Start(context.Background()); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("ended worker claimed restart", err)
	}
	w.Stop(context.Background())
}

func TestInactiveSnapshotsDoNotConsumeListenerQuotaForever(t *testing.T) {
	w := worker(t, false)
	for i := 0; i < 270; i++ {
		_, err := w.StartForward(context.Background(), &ppb.ForwardSpec{Id: fmt.Sprintf("stopped_%d", i), ServerId: "server", RemotePort: 3000})
		if err != nil {
			t.Fatalf("stopped runtime snapshot exhausted listener quota: %v", err)
		}
	}
	state, err := w.StartForward(context.Background(), &ppb.ForwardSpec{Id: "active", ServerId: "server", RemotePort: 3000, Enabled: true})
	if err != nil || state.BoundPort == 0 {
		t.Fatal(state, err)
	}
}
