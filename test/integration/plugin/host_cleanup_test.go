package plugin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"golang.org/x/sys/unix"
)

// This fixture distinguishes cancellation delivery from completed host cleanup.
// The real worker owns the client side of this authenticated HTTP/2 callback.
type cleanupBarrierCore struct {
	*coreProbe
	entered chan struct{}
	release chan struct{}
	exited  chan struct{}
}

type startupCleanupCore struct {
	*cleanupBarrierCore
	started chan struct{}
}

func (p *startupCleanupCore) GetConfig(ctx context.Context, _ *ppb.ConfigRequest) (*ppb.PluginConfig, error) {
	defer close(p.exited)
	close(p.started)
	<-ctx.Done()
	close(p.entered)
	<-p.release
	return nil, ctx.Err()
}

func TestHostFailedLaunchRetainsCallbackOwnership(t *testing.T) {
	cfg, base := pluginConfig(t, "")
	probe := &startupCleanupCore{cleanupBarrierCore: &cleanupBarrierCore{coreProbe: base, entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}, started: make(chan struct{})}
	cfg.Core = probe
	var once sync.Once
	release := func() { once.Do(func() { close(probe.release) }) }
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		instance, err := host.Launch(ctx, cfg)
		if instance != nil {
			_ = instance.Stop(context.Background())
		}
		result <- err
	}()
	select {
	case <-probe.started:
	case <-time.After(4 * time.Second):
		t.Fatal("real worker did not enter its Init callback")
	}
	cancel()
	select {
	case <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("failed startup did not cancel its callback")
	}
	var launchErr error
	select {
	case launchErr = <-result:
	case <-time.After(750 * time.Millisecond):
		t.Error("Launch cancellation waited for pending process/callback cleanup")
		select {
		case launchErr = <-result:
		case <-time.After(3 * time.Second):
			release()
			launchErr = <-result
			t.Fatal("Launch remained blocked on callback cleanup", launchErr)
		}
	}
	if !errors.Is(launchErr, context.Canceled) && connect.CodeOf(launchErr) != connect.CodeCanceled {
		t.Errorf("original startup cancellation was not preserved: %v", launchErr)
	}
	callbackLeaseHeld(t, cfg.StateDir, true)
	entries, err := os.ReadDir(cfg.RunDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("pending failed startup lost its owned runtime directory: %v %v", entries, err)
	}
	release()
	select {
	case <-probe.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("failed startup callback did not finish after release")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err = os.ReadDir(cfg.RunDir)
		if err == nil && len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failed startup cleanup did not remove its runtime: %v %v", entries, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	callbackLeaseHeld(t, cfg.StateDir, false)
}

func (p *cleanupBarrierCore) SubscribeEvents(ctx context.Context, _ *ppb.SubscribeRequest, stream prpc.CoreAPISubscribeEventsServerStream) error {
	defer close(p.exited)
	if err := stream.Send(&ppb.Event{Type: "ready"}); err != nil {
		return err
	}
	<-ctx.Done()
	close(p.entered)
	// Model bounded deferred cleanup, not a handler ignoring cancellation.
	<-p.release
	return ctx.Err()
}

func callbackLeaseHeld(t *testing.T, stateDir string, wantHeld bool) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(stateDir, "instance.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	}
	if wantHeld {
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			t.Errorf("writer lease released while callback cleanup is pending: %v", err)
		}
	} else if err != nil {
		t.Errorf("writer lease retained after callback and process cleanup: %v", err)
	}
}

func TestHostStopJoinsCallbackCleanup(t *testing.T) {
	for _, mode := range []string{"background", "caller_deadline"} {
		t.Run(mode, func(t *testing.T) {
			cfg, base := pluginConfig(t, "")
			probe := &cleanupBarrierCore{coreProbe: base, entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
			cfg.Core = probe
			var once sync.Once
			release := func() { once.Do(func() { close(probe.release) }) }
			defer release()
			instance, err := host.Launch(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				release()
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if err := instance.Stop(ctx); err != nil {
					t.Errorf("cleanup Stop: %v", err)
				}
				waitStopped(t, instance)
			}()
			stopCtx := context.Background()
			if mode == "caller_deadline" {
				var cancel context.CancelFunc
				stopCtx, cancel = context.WithTimeout(stopCtx, 300*time.Millisecond)
				defer cancel()
			}
			result := make(chan error, 1)
			go func() { result <- instance.Stop(stopCtx) }()
			select {
			case <-probe.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("host did not cancel the real callback")
			}
			returned := false
			if mode == "caller_deadline" {
				select {
				case err := <-result:
					returned = true
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("Stop with pending callback cleanup must respect caller deadline: %v", err)
					}
				case <-time.After(2 * time.Second):
					t.Error("Stop ignored caller deadline while joining callback cleanup")
				}
			} else {
				select {
				case err := <-result:
					returned = true
					t.Errorf("Stop returned before callback cleanup completed: %v", err)
				case <-time.After(1500 * time.Millisecond):
				}
				// A second caller must not block behind another Stop's join mutex.
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				second := make(chan error, 1)
				go func() { second <- instance.Stop(ctx) }()
				select {
				case err := <-second:
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("concurrent Stop did not preserve its deadline: %v", err)
					}
				case <-time.After(time.Second):
					t.Error("concurrent Stop blocked behind pending cleanup")
				}
				cancel()
			}
			select {
			case <-instance.Done():
				t.Error("Done closed while callback cleanup is pending")
			default:
			}
			if state := instance.Snapshot().State; state != "stopping" {
				t.Errorf("pending callback cleanup state = %q; want stopping", state)
			}
			callbackLeaseHeld(t, cfg.StateDir, true)
			release()
			select {
			case <-probe.exited:
			case <-time.After(2 * time.Second):
				t.Fatal("released callback failed to finish")
			}
			if !returned {
				select {
				case err := <-result:
					if mode == "background" && err != nil {
						t.Errorf("Stop after callback cleanup: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Stop failed to finish after callback cleanup")
				}
			}
			waitStopped(t, instance)
			callbackLeaseHeld(t, cfg.StateDir, false)
		})
	}
}
