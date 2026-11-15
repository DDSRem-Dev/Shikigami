//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

type startingRuntimeManager struct {
	*testManager
	ready     <-chan struct{}
	inspected chan browserruntime.Spec
}

func (manager *startingRuntimeManager) Inspect(ctx context.Context, spec browserruntime.Spec) (browserruntime.Snapshot, error) {
	select {
	case manager.inspected <- spec:
	default:
	}
	select {
	case <-manager.ready:
		return manager.testManager.Inspect(ctx, spec)
	case <-ctx.Done():
		return browserruntime.Snapshot{}, ctx.Err()
	}
}

func TestPluginReattachmentWaitsForExistingStartingRuntime(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	first := newWorker(t, state, "boot", manager)
	putProfile(t, first, "one", false)
	started := startProfile(t, first, "one", "original-launch")
	first.mu.Lock()
	first.db.State = "starting"
	err := first.saveLocked()
	first.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	pending := &startingRuntimeManager{testManager: manager, ready: ready, inspected: make(chan browserruntime.Spec, 1)}
	second := NewWithManager(pending)
	second.bootID = func() (string, error) { return "boot", nil }
	if err := second.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: state}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- second.Start(ctx) }()
	select {
	case spec := <-pending.inspected:
		if spec.InstanceID != started.InstanceId {
			t.Fatalf("reattached another persisted instance: %s", spec.InstanceID)
		}
	case <-ctx.Done():
		t.Fatal("restart did not inspect the persisted runtime")
	}
	select {
	case err := <-result:
		t.Fatalf("restart returned before existing-unit readiness: %v", err)
	default:
	}
	manager.mu.Lock()
	launches, stops := manager.launches, manager.stops
	manager.mu.Unlock()
	if launches != 1 || stops != 0 {
		t.Fatalf("reattachment relaunched/stopped the independent runtime: %d/%d", launches, stops)
	}
	close(ready)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("restart did not complete after existing-unit readiness")
	}
	status, err := second.GetBrowserStatus(context.Background(), &epb.BrowserScope{ProfileId: "one"})
	if err != nil || status.State != "running" || status.InstanceId != started.InstanceId {
		t.Fatalf("reattached runtime identity/state changed: %#v %v", status, err)
	}
	manager.mu.Lock()
	launches, stops = manager.launches, manager.stops
	manager.mu.Unlock()
	if launches != 1 || stops != 0 {
		t.Fatalf("readiness relaunched/stopped the runtime: %d/%d", launches, stops)
	}
}
