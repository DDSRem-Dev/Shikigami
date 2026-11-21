//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

type statusPhaseKey struct{}

type statusGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
	failure error
}

func (g *statusGate) wait() error {
	g.calls.Add(1)
	g.once.Do(func() { close(g.entered) })
	<-g.release // Deliberately permit a late result even after cancellation.
	return g.failure
}

type statusManager struct {
	*testManager
	gate *statusGate
}

func (m *statusManager) Inspect(ctx context.Context, spec browserruntime.Spec) (browserruntime.Snapshot, error) {
	snapshot, err := m.testManager.Inspect(ctx, spec)
	if ctx.Value(statusPhaseKey{}) == "inspect" {
		if failure := m.gate.wait(); failure != nil {
			return browserruntime.Snapshot{}, failure
		}
	}
	return snapshot, err
}

type statusRegion struct {
	sdk.RegionClient
	gate *statusGate
}

func (r statusRegion) InspectRegion(ctx context.Context, _ *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	if ctx.Value(statusPhaseKey{}) == "region" {
		if err := r.gate.wait(); err != nil {
			return nil, err
		}
	}
	return &epb.RegionStatus{Revision: "region-one"}, nil
}

type statusCore struct {
	sdk.CoreAPI
	region sdk.RegionClient
}

func (c statusCore) Region() sdk.RegionClient { return c.region }
func (statusCore) Browser() sdk.BrowserClient { return nil }

func statusWorker(t *testing.T) (*Worker, *statusManager, *statusGate) {
	t.Helper()
	gate := &statusGate{entered: make(chan struct{}), release: make(chan struct{})}
	manager := &statusManager{testManager: &testManager{}, gate: gate}
	worker := NewWithManager(manager)
	worker.bootID = func() (string, error) { return "test-boot", nil }
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := worker.Init(t.Context(), sdk.InitContext{Side: "server", StateDir: dir, Core: statusCore{region: statusRegion{gate: gate}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := worker.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "initial-start")
	return worker, manager, gate
}

func TestStatusInspectionDoesNotBlockOrOverwriteConcurrentChanges(t *testing.T) {
	for _, phase := range []string{"inspect", "region"} {
		for _, change := range []string{"stop", "delete", "restart", "profile", "config", "observation"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				worker, manager, gate := statusWorker(t)
				var release sync.Once
				unblock := func() { release.Do(func() { close(gate.release) }) }
				defer unblock()
				ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), statusPhaseKey{}, phase), 3*time.Second)
				defer cancel()
				result := make(chan error, 1)
				go func() { _, err := worker.GetBrowserStatus(ctx, &epb.BrowserScope{ProfileId: "one"}); result <- err }()
				select {
				case <-gate.entered:
				case <-ctx.Done():
					t.Fatal("status did not enter blocked inspection")
				}
				changed := make(chan error, 1)
				go func() {
					var err error
					switch change {
					case "stop", "restart":
						_, err = worker.StopBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "explicit-stop", Confirm: true})
						if err == nil && change == "restart" {
							_, err = worker.StartBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "explicit-restart"})
						}
					case "delete", "profile":
						worker.mu.Lock()
						profile := proto.Clone(worker.db.Profiles["one"]).(*epb.BrowserProfile)
						worker.mu.Unlock()
						if change == "delete" {
							_, err = worker.DeleteBrowserProfile(t.Context(), &epb.DeleteBrowserProfileRequest{ProfileId: "one", ExpectedRevision: profile.Revision, RequestId: "explicit-delete", Confirm: true})
						} else {
							profile.Name = "updated"
							_, err = worker.PutBrowserProfile(t.Context(), &epb.PutBrowserProfileRequest{Profile: profile, ExpectedRevision: profile.Revision, RequestId: "explicit-update"})
						}
					case "config":
						worker.mu.Lock()
						worker.config.Width++
						worker.mu.Unlock()
					case "observation":
						manager.setState("crashed")
						err = worker.observeRuntime(t.Context())
					}
					changed <- err
				}()
				select {
				case err := <-changed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(300 * time.Millisecond):
					unblock()
					<-changed
					<-result
					t.Fatal("slow status I/O held the worker state lock")
				}
				worker.mu.Lock()
				before := cloneDatabase(worker.db)
				worker.mu.Unlock()
				unblock()
				select {
				case err := <-result:
					if connect.CodeOf(err) != connect.CodeAborted {
						t.Fatalf("stale status must abort: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("status did not finish")
				}
				worker.mu.Lock()
				after := cloneDatabase(worker.db)
				worker.mu.Unlock()
				if !reflect.DeepEqual(before, after) {
					t.Fatal("late status overwrote concurrent state")
				}
				if gate.calls.Load() != 1 {
					t.Fatal("status retried slow I/O")
				}
			})
		}
	}
}

func TestStatusInspectionCancellationAndErrorsDoNotPublishLateResults(t *testing.T) {
	for _, phase := range []string{"inspect", "region"} {
		for _, failure := range []string{"cancel", "external-error"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				worker, manager, gate := statusWorker(t)
				if phase == "inspect" {
					manager.setState("crashed")
				}
				want := errors.New("controlled inspection failure")
				if failure == "external-error" {
					gate.failure = want
				}
				var release sync.Once
				unblock := func() { release.Do(func() { close(gate.release) }) }
				defer unblock()
				ctx, cancel := context.WithCancel(context.WithValue(t.Context(), statusPhaseKey{}, phase))
				defer cancel()
				result := make(chan error, 1)
				go func() { _, err := worker.GetBrowserStatus(ctx, &epb.BrowserScope{ProfileId: "one"}); result <- err }()
				select {
				case <-gate.entered:
				case <-time.After(time.Second):
					t.Fatal("inspection did not begin")
				}
				if failure == "cancel" {
					cancel()
					want = context.Canceled
				}
				unblock()
				if err := <-result; !errors.Is(err, want) {
					t.Fatalf("lost original failure: %v", err)
				}
				worker.mu.Lock()
				state := worker.db.State
				worker.mu.Unlock()
				if state != "running" {
					t.Fatalf("canceled/failed inspect published state: %s", state)
				}
			})
		}
	}
}

func TestStatusInspectionSavesVerifiedCrashDespiteRegionFailure(t *testing.T) {
	worker, manager, gate := statusWorker(t)
	manager.setState("crashed")
	want := errors.New("region observation failed")
	gate.failure = want
	close(gate.release)
	ctx := context.WithValue(t.Context(), statusPhaseKey{}, "region")
	if _, err := worker.GetBrowserStatus(ctx, &epb.BrowserScope{ProfileId: "one"}); !errors.Is(err, want) {
		t.Fatalf("lost region failure: %v", err)
	}
	worker.mu.Lock()
	state := worker.db.State
	worker.mu.Unlock()
	var persisted database
	if err := browserruntime.ReadJSON(filepath.Join(worker.stateDir, "profiles.json"), &persisted, maxDatabaseBytes); err != nil {
		t.Fatal(err)
	}
	if state != "crashed" || persisted.State != "crashed" {
		t.Fatal("region failure discarded verified runtime state", state, persisted.State)
	}
}

func TestStatusInspectionReturnsCurrentControlAfterSlowRegion(t *testing.T) {
	worker, _, gate := statusWorker(t)
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	defer unblock()
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), statusPhaseKey{}, "region"), 3*time.Second)
	defer cancel()
	result := make(chan *epb.BrowserStatus, 1)
	failure := make(chan error, 1)
	go func() {
		status, err := worker.GetBrowserStatus(ctx, &epb.BrowserScope{ProfileId: "one"})
		result <- status
		failure <- err
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal("region inspection did not begin")
	}
	// The actual grant comes from control events. Status must report the latest
	// revision rather than one captured before the slow external observation.
	worker.control.Lock()
	worker.controller, worker.controlRevision = "current-viewer", "current-revision"
	worker.control.Unlock()
	unblock()
	status := <-result
	if err := <-failure; err != nil {
		t.Fatal(err)
	}
	if status.ControllerId != "current-viewer" || status.ControlRevision != "current-revision" || status.State != "running" || status.RestartRequired {
		t.Fatalf("stale/inconsistent returned status: %v", status)
	}
}
