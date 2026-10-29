//go:build linux

package fault_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

// These helpers use real install transactions with inert lifecycle callbacks.
// They never register services or launch the fixture payloads.
type stageLifecycle struct{ calls int }

func (f *stageLifecycle) Stop(context.Context, install.Plan) error { f.calls++; return nil }
func (f *stageLifecycle) Backup(context.Context, install.Plan, string) (string, error) {
	f.calls++
	return "private-backup", nil
}
func (f *stageLifecycle) Prepare(context.Context, install.Plan, string) error { f.calls++; return nil }
func (f *stageLifecycle) Start(context.Context, install.Plan) error           { f.calls++; return nil }
func (f *stageLifecycle) Probe(context.Context, install.Plan) (string, error) {
	f.calls++
	return "application_pending", nil
}
func (f *stageLifecycle) RemoveService(context.Context, install.Plan) error { f.calls++; return nil }

type stageFixture struct {
	Pin     string       `json:"pin"`
	Receipt []byte       `json:"receipt"`
	Current string       `json:"current"`
	Plan    install.Plan `json:"plan"`
}

func stageRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("SHIKI_STAGE_ROOT")
	if root == "" {
		t.Skip("opt-in install fsync acceptance; run scripts/test-install-stage.py")
	}
	if os.Geteuid() == 0 || !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
		t.Fatal("explicit private root and ordinary UID required")
	}
	return root
}
func stageWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
func stageBundle(t *testing.T, root, version string) *install.Bundle {
	t.Helper()
	directory := filepath.Join(root, version)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port", "install.sh", "install-client.sh", "uninstall.sh", "LICENSE"} {
		stageWrite(t, filepath.Join(directory, name), []byte(version+" fixture "+name), 0700)
	}
	for _, id := range []string{"sync", "port"} {
		sum := sha256.Sum256(readBytes(t, filepath.Join(directory, "shiki-plugin-"+id)))
		protocol, _ := sdk.BuiltinProtocol(id, "both")
		data, err := json.Marshal(host.Manifest{Name: id, Version: version, Executable: "shiki-plugin-" + id, SHA256: hex.EncodeToString(sum[:]), Side: "both", Protocol: protocol})
		if err != nil {
			t.Fatal(err)
		}
		stageWrite(t, filepath.Join(directory, id+".manifest.json"), data, 0600)
	}
	pin, err := install.CreateManifest(directory, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := install.Verify(directory, version, pin)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}
func TestPrepareInstallStage(t *testing.T) {
	root := stageRoot(t)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := install.OpenArtifacts(filepath.Join(root, "prefix"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	old := stageBundle(t, root, "v1.0.0")
	next := stageBundle(t, root, "v1.1.0")
	plan := install.Plan{Role: "client", User: "shikitest", Home: root, Config: filepath.Join(root, "config.yaml"), StateDir: filepath.Join(root, "state"), PluginsDir: filepath.Join(root, "plugins"), Path: "/usr/bin:/bin", Port: 9527}
	stageWrite(t, plan.Config, []byte("preserved config"), 0600)
	if err := os.Mkdir(plan.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	stageWrite(t, filepath.Join(plan.StateDir, "identity"), []byte("preserved identity"), 0600)
	if _, err = a.Install(t.Context(), old, plan, false, &stageLifecycle{}); err != nil {
		t.Fatal(err)
	}
	current, err := a.Current()
	if err != nil {
		t.Fatal(err)
	}
	fixture := stageFixture{Pin: next.TrustedChecksums, Receipt: readBytes(t, filepath.Join(a.Prefix, "installation.json")), Current: current, Plan: plan}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	stageWrite(t, filepath.Join(root, "fixture.json"), data, 0600)
	if os.Getenv("SHIKI_STAGE_EXISTING") == "1" {
		if _, err = a.Stage(next); err != nil {
			t.Fatal(err)
		}
	}
}
func openStageFixture(t *testing.T) (*install.Artifacts, *install.Bundle, stageFixture) {
	t.Helper()
	root := stageRoot(t)
	var f stageFixture
	if err := json.Unmarshal(readBytes(t, filepath.Join(root, "fixture.json")), &f); err != nil {
		t.Fatal(err)
	}
	a, err := install.OpenArtifacts(filepath.Join(root, "prefix"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	b, err := install.Verify(filepath.Join(root, "v1.1.0"), "v1.1.0", f.Pin)
	if err != nil {
		t.Fatal(err)
	}
	return a, b, f
}
func unchangedStage(t *testing.T, a *install.Artifacts, f stageFixture) {
	t.Helper()
	current, err := a.Current()
	if err != nil || current != f.Current {
		t.Fatalf("active link changed: %q %v", current, err)
	}
	if !bytes.Equal(readBytes(t, filepath.Join(a.Prefix, "installation.json")), f.Receipt) {
		t.Fatal("receipt changed")
	}
	if string(readBytes(t, f.Plan.Config)) != "preserved config" || string(readBytes(t, filepath.Join(f.Plan.StateDir, "identity"))) != "preserved identity" {
		t.Fatal("config/state changed")
	}
	receipt, err := a.Receipt()
	if err != nil || receipt.Pending != nil {
		t.Fatalf("unexpected pending receipt: %v", err)
	}
	if _, err := install.Verify(filepath.Join(a.Prefix, receipt.Active.Release), receipt.Active.Version, receipt.Active.Checksums); err != nil {
		t.Fatalf("old release damaged: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(a.Prefix, ".stage-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("staging leftovers: %v %v", leftovers, err)
	}
}
func TestObserveInstallStage(t *testing.T) {
	a, b, f := openStageFixture(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := a.Stage(b); err != nil {
		t.Fatal(err)
	}
	unchangedStage(t, a, f)
}
func TestInjectedInstallStage(t *testing.T) {
	a, b, f := openStageFixture(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	life := &stageLifecycle{}
	result, err := a.Install(t.Context(), b, f.Plan, false, life)
	if !errors.Is(err, syscall.EIO) || result != nil {
		t.Fatalf("expected failed install on actual fsync EIO, result=%+v error=%v", result, err)
	}
	if life.calls != 0 {
		t.Fatalf("stage failure reached lifecycle: %d", life.calls)
	}
	unchangedStage(t, a, f)
}
func TestRecoverInstallStage(t *testing.T) {
	a, b, f := openStageFixture(t)
	unchangedStage(t, a, f)
	for range 2 {
		if _, err := a.Stage(b); err != nil {
			t.Fatal(err)
		}
		unchangedStage(t, a, f)
	}
}
