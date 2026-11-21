//go:build linux

package server

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

type testManager struct {
	mu           sync.Mutex
	launches     int
	stops        int
	snapshot     browserruntime.Snapshot
	inspectError error
	launchError  error
	launchState  string
	peers        []net.Conn
}

func (manager *testManager) Prepare(_ context.Context, state, profile, profileRevision, instance string, region *epb.RegionConfig, revision string, width, height int, mode string) (browserruntime.Spec, error) {
	spec := browserruntime.Spec{ProfileID: profile, ProfileRevision: profileRevision, InstanceID: instance, Token: browserruntime.NewID(), Unit: "shiki-browser-test", ConfigPath: filepath.Join(state, "runtime", instance+".launch.json"), SocketDir: filepath.Join("/tmp", instance), ProfileDir: filepath.Join(state, "profiles", profile), ExecutableSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", Region: region, RegionRevision: revision, Width: width, Height: height, Mode: mode}
	return spec, browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody)
}
func (manager *testManager) Launch(_ context.Context, spec browserruntime.Spec) (browserruntime.Snapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.launches++
	manager.snapshot = browserruntime.Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, RegionRevision: spec.RegionRevision, State: "running", Mode: spec.BrowserMode()}
	if manager.launchState != "" {
		manager.snapshot.State = manager.launchState
	}
	return manager.snapshot, manager.launchError
}
func (manager *testManager) Inspect(_ context.Context, spec browserruntime.Spec) (browserruntime.Snapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.inspectError != nil {
		return browserruntime.Snapshot{}, manager.inspectError
	}
	snapshot := manager.snapshot
	if snapshot.InstanceID != spec.InstanceID {
		snapshot = browserruntime.Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, RegionRevision: spec.RegionRevision, State: "crashed"}
	}
	return snapshot, nil
}
func (manager *testManager) Stop(context.Context, browserruntime.Spec) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.stops++
	manager.snapshot.State = "crashed"
	return nil
}
func (manager *testManager) Dial(context.Context, browserruntime.Spec) (net.Conn, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	left, right := net.Pipe()
	manager.peers = append(manager.peers, right)
	return left, nil
}
func (manager *testManager) setState(state string) {
	manager.mu.Lock()
	manager.snapshot.State = state
	manager.mu.Unlock()
}
func (manager *testManager) Diagnose(context.Context, browserruntime.Spec, *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	return &epb.DiagnosticReport{}, nil
}
func newWorker(t *testing.T, state, boot string, manager *testManager) *Worker {
	t.Helper()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	worker := NewWithManager(manager)
	worker.bootID = func() (string, error) { return boot, nil }
	if err := worker.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := worker.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return worker
}
func putProfile(t *testing.T, worker *Worker, id string, boot bool) *epb.BrowserProfile {
	t.Helper()
	profile, err := worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Id: id, Name: id, BootStart: boot}, RequestId: "put-" + id})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
func startProfile(t *testing.T, worker *Worker, id, request string) *epb.BrowserStatus {
	t.Helper()
	status, err := worker.StartBrowser(context.Background(), &epb.BrowserActionRequest{ProfileId: id, RequestId: request})
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestProfilesRevisionDedupeAndOneBootTarget(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot-one", &testManager{})
	first := putProfile(t, worker, "first", true)
	request := &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Id: "second", Name: "second", BootStart: true}, RequestId: "put-second"}
	if _, err := worker.PutBrowserProfile(context.Background(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("boot target collision: %v", err)
	}
	request.ReplaceBootProfile = true
	second, err := worker.PutBrowserProfile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := worker.PutBrowserProfile(context.Background(), request)
	if err != nil || replayed.Revision != second.Revision {
		t.Fatalf("dedupe: %v %#v", err, replayed)
	}
	request.Profile.Name = "changed"
	if _, err := worker.PutBrowserProfile(context.Background(), request); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("changed request reuse: %v", err)
	}
	if _, err := worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: first, ExpectedRevision: first.Revision, RequestId: "stale-first"}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("boot replacement must revise old target: %v", err)
	}
	profiles, err := worker.ListBrowserProfiles(context.Background(), &epb.BrowserScope{})
	if err != nil {
		t.Fatal(err)
	}
	boots := 0
	for _, profile := range profiles.Profiles {
		if profile.BootStart {
			boots++
		}
	}
	if boots != 1 {
		t.Fatalf("boot count %d", boots)
	}
}
func TestPluginRestartPreservesBrowserAndSameBootNeverResurrectsStop(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	first := newWorker(t, state, "boot-one", manager)
	putProfile(t, first, "persistent", true)
	status := startProfile(t, first, "persistent", "start-one")
	if err := first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.stops != 0 {
		t.Fatal("lifecycle Stop stopped independent browser")
	}
	second := newWorker(t, state, "boot-one", manager)
	reattached, err := second.GetBrowserStatus(context.Background(), &epb.BrowserScope{ProfileId: "persistent"})
	if err != nil || reattached.InstanceId != status.InstanceId || manager.launches != 1 {
		t.Fatalf("reattach %#v %v", reattached, err)
	}
	if _, err := second.StopBrowser(context.Background(), &epb.BrowserActionRequest{ProfileId: "persistent", RequestId: "stop-one", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	second.Stop(context.Background())
	third := newWorker(t, state, "boot-one", manager)
	stopped, err := third.GetBrowserStatus(context.Background(), &epb.BrowserScope{ProfileId: "persistent"})
	if err != nil || stopped.State != "stopped" || manager.launches != 1 {
		t.Fatalf("same boot resurrected stop: %#v %v", stopped, err)
	}
	third.Stop(context.Background())
	fourth := newWorker(t, state, "boot-two", manager)
	if manager.launches != 2 {
		t.Fatalf("new boot did not launch selected target: %d", manager.launches)
	}
	fourth.Stop(context.Background())
}
func TestCrashDoesNotRestartAndSwitchDeleteRequireExplicitDecision(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot", manager)
	first := putProfile(t, worker, "one", true)
	putProfile(t, worker, "two", false)
	startProfile(t, worker, "one", "start-one")
	if _, err := worker.StartBrowser(context.Background(), &epb.BrowserActionRequest{ProfileId: "two", RequestId: "unsafe-switch"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("switch without confirmation: %v", err)
	}
	if manager.stops != 0 {
		t.Fatal("unconfirmed switch stopped browser")
	}
	if _, err := worker.DeleteBrowserProfile(context.Background(), &epb.DeleteBrowserProfileRequest{ProfileId: "one", ExpectedRevision: first.Revision, RequestId: "unsafe-delete"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("delete without confirmation: %v", err)
	}
	manager.setState("crashed")
	worker.Stop(context.Background())
	worker = newWorker(t, state, "boot", manager)
	if manager.launches != 1 {
		t.Fatal("plugin restart relaunched crashed Chrome")
	}
	result, err := worker.DeleteBrowserProfile(context.Background(), &epb.DeleteBrowserProfileRequest{ProfileId: "one", ExpectedRevision: first.Revision, RequestId: "delete-one", Confirm: true})
	if err != nil || result.State != "deleted" || manager.stops != 1 {
		t.Fatalf("verified delete %#v %v", result, err)
	}
	startProfile(t, worker, "two", "start-two")
}
func TestDisableOnlySkipsBootAndUnavailableControl(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot-one", manager)
	putProfile(t, worker, "one", true)
	startProfile(t, worker, "one", "start")
	worker.Stop(context.Background())
	t.Setenv("SHIKI_BROWSER_DISABLE_ONLY", "1")
	manager.inspectError = errors.New("control socket damaged")
	cleanup := newWorker(t, state, "boot-two", manager)
	if _, err := cleanup.StopBrowser(context.Background(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "disable-stop", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if manager.launches != 1 || manager.stops != 1 {
		t.Fatalf("cleanup launch/stop %d/%d", manager.launches, manager.stops)
	}
}
func TestProfileDeletionRemovesOnlyOwnedProfile(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	profile := putProfile(t, worker, "one", false)
	putProfile(t, worker, "two", false)
	directory, err := worker.stage(context.Background(), "one", "uploads")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.DeleteBrowserProfile(context.Background(), &epb.DeleteBrowserProfileRequest{ProfileId: "one", ExpectedRevision: profile.Revision, RequestId: "delete", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worker.stateDir, "profiles", "one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile data remains: %v", err)
	}
	profiles, _ := worker.ListBrowserProfiles(context.Background(), &epb.BrowserScope{})
	if len(profiles.Profiles) != 1 || profiles.Profiles[0].Id != "two" {
		t.Fatal("unrelated profile changed")
	}
}

func TestFailedStartRequestNeverRelaunchesAndNewRequestIsExplicit(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{launchError: errors.New("keyring locked")}
	worker := newWorker(t, state, "boot", manager)
	putProfile(t, worker, "one", false)
	request := &epb.BrowserActionRequest{ProfileId: "one", RequestId: "attempt"}
	if _, err := worker.StartBrowser(context.Background(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal(err)
	}
	manager.setState("crashed")
	if _, err := worker.StartBrowser(context.Background(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition || manager.launches != 1 {
		t.Fatalf("failed request relaunched: %v launches=%d", err, manager.launches)
	}
	worker.Stop(context.Background())
	worker = newWorker(t, state, "boot", manager)
	if _, err := worker.StartBrowser(context.Background(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition || manager.launches != 1 {
		t.Fatalf("failed receipt lost: %v launches=%d", err, manager.launches)
	}
	manager.launchError = nil
	startProfile(t, worker, "one", "new-explicit-attempt")
	if manager.launches != 2 {
		t.Fatal("new manual start did not launch")
	}
}

func TestInterruptedUploadIsCleanedOnPluginReattachment(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot", manager)
	putProfile(t, worker, "one", false)
	directory, err := worker.stage(context.Background(), "one", "uploads")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".upload-123456789"), []byte("unfinished"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "keep.txt"), []byte("committed"), 0600); err != nil {
		t.Fatal(err)
	}
	worker.Stop(context.Background())
	worker = newWorker(t, state, "boot", manager)
	if _, err := os.Stat(filepath.Join(directory, ".upload-123456789")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted temp remains: %v", err)
	}
	files, err := worker.ListBrowserFiles(context.Background(), &epb.BrowserFileRequest{ProfileId: "one", Area: "uploads"})
	if err != nil || len(files.Files) != 1 || files.Files[0].Name != "keep.txt" {
		t.Fatalf("cleanup changed committed file %#v %v", files, err)
	}
}

func TestCrashObserverUpdatesProfilesWithoutRestartOrStatusCall(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "start")
	manager.setState("crashed")
	if err := worker.observeRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	profiles, err := worker.ListBrowserProfiles(context.Background(), &epb.BrowserScope{})
	if err != nil || len(profiles.Profiles) != 1 || profiles.Profiles[0].State != "crashed" {
		t.Fatalf("crash not reflected %#v %v", profiles, err)
	}
	if manager.launches != 1 || manager.stops != 0 {
		t.Fatal("observer restarted or stopped runtime")
	}
}
