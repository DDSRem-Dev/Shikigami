package plugin_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
)

func TestBoundedPluginResourceRecovery(t *testing.T) {
	count := resources.Cycles(t)
	if count == 0 {
		t.Skip("opt-in resource regression; run scripts/test-resources.sh")
	}
	cfg, _ := pluginConfig(t, "publish")
	runCycle := func(i int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		probe := &coreProbe{closed: make(chan struct{})}
		cfg.Core = probe
		instance, err := host.Launch(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Stop(context.Background())
		if _, err = instance.Health(ctx); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			if err = instance.Stop(ctx); err != nil {
				t.Fatal(err)
			}
		} else {
			cancel()
		}
		waitStopped(t, instance)
		select {
		case <-probe.closed:
		case <-time.After(time.Second):
			t.Fatal("reaped plugin retained a CoreAPI subscription")
		}
		entries, err := os.ReadDir(cfg.RunDir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("reaped plugin retained runtime endpoints: %v %v", entries, err)
		}
	}
	for i := 0; i < 2; i++ {
		runCycle(i)
	}
	baseline := resources.Capture(t, os.Getpid())
	for i := 0; i < count; i++ {
		runCycle(i)
		if (i+1)%4 == 0 || i+1 == count {
			resources.Restored(t, fmt.Sprintf("plugin launch/stop/cancel %d/%d", i+1, count), baseline, 2, 4)
		}
	}
}
