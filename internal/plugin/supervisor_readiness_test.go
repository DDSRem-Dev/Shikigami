package plugin

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/structpb"
)

type blockedStopLifecycle struct {
	sdk.Lifecycle
	entered chan struct{}
	release chan struct{}
}

func (l *blockedStopLifecycle) Stop(ctx context.Context) error {
	close(l.entered)
	select {
	case <-l.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Hold the supervisor publication at ready while the real Instance.Stop is
// blocked. Expired admission must disappear immediately, before process join.
func TestSupervisorDoesNotAdvertiseEndedInstance(t *testing.T) {
	for _, mode := range []string{"credential expiry during Stop", "instance Done"} {
		t.Run(mode, func(t *testing.T) {
			manager, err := NewSupervisor(t.Context(), SupervisorOptions{
				StateDir: filepath.Join(t.TempDir(), "intent"),
				Validate: func(string, *structpb.Struct) error { return nil },
				Config:   func(string, *structpb.Struct) (Config, error) { panic("test must not start supervisor loop") },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			want, err := manager.desired("sync")
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := manager.Manage(t.Context(), &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, RequestId: "enable", ExpectedRevision: want.Revision})
			if err != nil {
				t.Fatal(err)
			}
			want, err = manager.desired("sync")
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Minute)
			if mode == "credential expiry during Stop" {
				deadline = time.Now().Add(time.Second)
			}
			lifetime, cancel := context.WithDeadline(t.Context(), deadline)
			defer cancel()
			worker := &blockedStopLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
			instance := &Instance{
				grant: &grant{valid: true, lifetime: lifetime, cancel: cancel, deadline: deadline},
				done:  make(chan struct{}), dir: t.TempDir(), lifecycle: worker,
				snapshot: Snapshot{InstanceID: "plugin_live", State: "ready", Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{sdk.FileCapability}}},
			}
			manager.publish("sync", manager.slots["sync"], want, instance, "ready", "")
			if manager.ReadyInstanceID("sync") != "plugin_live" {
				t.Fatal("healthy instance is not ready")
			}
			if err := manager.Call(t.Context(), "sync", sdk.FileCapability, func(context.Context, sdk.Lifecycle) error { return nil }); err != nil {
				t.Fatal("healthy worker call refused", err)
			}
			if mode == "credential expiry during Stop" {
				stopped := make(chan struct{})
				go func() {
					<-lifetime.Done()
					_ = instance.Stop(context.Background())
					close(stopped)
				}()
				defer func() { close(worker.release); <-stopped }()
				select {
				case <-worker.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("expiry did not enter Stop")
				}
				select {
				case <-instance.Done():
					t.Fatal("Stop was not held open")
				default:
				}
			} else {
				close(instance.done)
			}
			list, err := manager.List()
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range list.Plugins {
				if status.PluginId != "sync" {
					continue
				}
				if status.State == "ready" || status.Protocol != nil {
					t.Errorf("ended worker still advertised: %v", status)
				}
				if !status.Enabled || status.Revision != accepted.Revision {
					t.Error("runtime expiry changed durable desired intent", status)
				}
			}
			if manager.current("sync") != nil || manager.ReadyInstanceID("sync") != "" {
				t.Error("ended worker retained a current ready identity")
			}
			called := false
			err = manager.Call(t.Context(), "sync", sdk.FileCapability, func(context.Context, sdk.Lifecycle) error { called = true; return nil })
			if called || (connect.CodeOf(err) != connect.CodeFailedPrecondition && connect.CodeOf(err) != connect.CodeUnavailable) {
				t.Errorf("ended worker admitted call: called=%v err=%v", called, err)
			}
		})
	}
}
