package plugin_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSupervisorCloseJoinsCallbackCleanup(t *testing.T) {
	for _, mode := range []string{"ready_instance", "failed_launch"} {
		t.Run(mode, func(t *testing.T) {
			cfg, base := pluginConfig(t, "")
			probe := &cleanupBarrierCore{coreProbe: base, entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
			cfg.Core = probe
			var startup *startupCleanupCore
			if mode == "failed_launch" {
				startup = &startupCleanupCore{cleanupBarrierCore: probe, started: make(chan struct{})}
				cfg.Core = startup
			}
			var once sync.Once
			release := func() { once.Do(func() { close(probe.release) }) }
			defer release()
			manager, err := host.NewSupervisor(t.Context(), host.SupervisorOptions{
				StateDir: filepath.Join(t.TempDir(), "intent"),
				Validate: func(string, *structpb.Struct) error { return nil },
				Config:   func(string, *structpb.Struct) (host.Config, error) { return cfg, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { release(); _ = manager.Close() }()
			list, err := manager.List()
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range list.Plugins {
				if status.PluginId == "sync" {
					_, err = manager.Manage(t.Context(), &pb.ManagePluginRequest{PluginId: "sync", RequestId: "enable", ExpectedRevision: status.Revision, Action: pb.ManagePluginRequest_ENABLE})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			manager.Start()
			if startup != nil {
				select {
				case <-startup.started:
				case <-time.After(4 * time.Second):
					t.Fatal("supervisor did not reach the real Init callback")
				}
			} else {
				deadline := time.Now().Add(4 * time.Second)
				for manager.ReadyInstanceID("sync") == "" {
					if time.Now().After(deadline) {
						t.Fatal("supervisor did not publish the real ready worker")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			result := make(chan error, 1)
			go func() { result <- manager.Close() }()
			select {
			case <-probe.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("supervisor shutdown failed to cancel the callback")
			}
			hold := 1500 * time.Millisecond
			if startup == nil {
				// Exceed Supervisor.stop's five-second per-call deadline. Its
				// owner must keep joining after the individual Stop times out.
				hold = 5200 * time.Millisecond
			}
			returned := false
			select {
			case err := <-result:
				returned = true
				t.Errorf("Supervisor.Close returned with a callback cleanup still owned: %v", err)
			case <-time.After(hold):
			}
			callbackLeaseHeld(t, cfg.StateDir, true)
			release()
			if !returned {
				select {
				case err := <-result:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Supervisor.Close failed to join released cleanup")
				}
			}
			select {
			case <-probe.exited:
			default:
				t.Error("supervisor closed its store before callback completion")
			}
			callbackLeaseHeld(t, cfg.StateDir, false)
		})
	}
}
