package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func bundleFixture(t *testing.T, version string) *Bundle {
	t.Helper()
	directory := t.TempDir()
	for _, name := range names() {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture "+version+" "+name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"sync", "port"} {
		data, _ := os.ReadFile(filepath.Join(directory, "shiki-plugin-"+id))
		sum := sha256.Sum256(data)
		protocol, _ := sdk.BuiltinProtocol(id, "both")
		manifest := host.Manifest{Name: id, Version: version, Executable: "shiki-plugin-" + id, SHA256: hex.EncodeToString(sum[:]), Side: "both", Protocol: protocol}
		data, _ = json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(directory, id+".manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	pin, err := CreateManifest(directory, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := Verify(directory, version, pin)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

type fakeLifecycle struct {
	events          []string
	failPrepare     bool
	failProbe       bool
	prefix          string
	backupHook      func() error
	startErr        error
	startContextErr error
}

func (f *fakeLifecycle) Stop(context.Context, Plan) error {
	f.events = append(f.events, "stop")
	return nil
}
func (f *fakeLifecycle) Backup(context.Context, Plan, string) (string, error) {
	f.events = append(f.events, "backup")
	if f.backupHook != nil {
		if err := f.backupHook(); err != nil {
			return "", err
		}
	}
	return "private-backup", nil
}
func (f *fakeLifecycle) Prepare(_ context.Context, _ Plan, release string) error {
	f.events = append(f.events, "prepare")
	target, err := os.Readlink(filepath.Join(f.prefix, "current"))
	if err != nil || filepath.Join(f.prefix, target) != release {
		return errors.New("prepare did not receive selected release")
	}
	if f.failPrepare {
		return errors.New("injected initialization failure")
	}
	return nil
}
func (f *fakeLifecycle) Start(ctx context.Context, _ Plan) error {
	f.events = append(f.events, "start")
	f.startContextErr = ctx.Err()
	return f.startErr
}
func (f *fakeLifecycle) Probe(context.Context, Plan) (string, error) {
	f.events = append(f.events, "probe")
	if f.failProbe {
		return "", errors.New("injected readiness failure")
	}
	return "node_needs_login", nil
}
func (f *fakeLifecycle) RemoveService(context.Context, Plan) error {
	f.events = append(f.events, "remove_service")
	return nil
}
func testArtifacts(t *testing.T) *Artifacts {
	t.Helper()
	a, err := OpenArtifacts(filepath.Join(t.TempDir(), "install"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func testPlan(t *testing.T) Plan {
	return Plan{Role: "client", User: "ordinary", Home: t.TempDir(), Config: filepath.Join(t.TempDir(), "client.yaml"), StateDir: t.TempDir(), PluginsDir: filepath.Join(t.TempDir(), "plugins"), Path: "/usr/bin:/bin", Port: 9527}
}

func TestStageUpgradeRollbackAndPreservation(t *testing.T) {
	ctx := context.Background()
	a := testArtifacts(t)
	plan := testPlan(t)
	first := bundleFixture(t, "v1.0.0")
	second := bundleFixture(t, "v1.1.0")
	f := &fakeLifecycle{prefix: a.Prefix}
	result, err := a.Install(ctx, first, plan, true, f)
	if err != nil || result.State != "staged" || len(f.events) != 0 {
		t.Fatalf("stage performed activation: %v", err)
	}
	if current, _ := a.Current(); current != "" {
		t.Fatal("stage switched active version")
	}
	if _, err = os.Stat(result.ReviewFile); err != nil {
		t.Fatal("staged service was not reviewable")
	}
	if err = os.WriteFile(plan.Config, []byte("existing private config"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Install(ctx, first, plan, false, f); err != nil {
		t.Fatal(err)
	}
	expected := []string{"stop", "backup", "prepare", "start", "probe"}
	if !reflect.DeepEqual(f.events, expected) {
		t.Fatal(f.events)
	}
	before := len(f.events)
	if _, err = a.Rollback(ctx, f); err == nil || len(f.events) != before {
		t.Fatal("rollback without a previous release changed the active installation")
	}
	if current, _ := a.Current(); current != releaseName(first.Manifest) {
		t.Fatal("rollback without a previous release removed the current link")
	}
	if _, err = a.Install(ctx, first, plan, false, f); err != nil || len(f.events) != before {
		t.Fatal("repeat installation restarted services")
	}
	if _, err = a.Install(ctx, second, plan, false, f); err != nil {
		t.Fatal(err)
	}
	if current, _ := a.Current(); current != releaseName(second.Manifest) {
		t.Fatal("upgrade did not switch atomically")
	}
	if _, err = a.Rollback(ctx, f); err != nil {
		t.Fatal(err)
	}
	if current, _ := a.Current(); current != releaseName(first.Manifest) {
		t.Fatal("rollback did not select old version")
	}
	data, _ := os.ReadFile(plan.Config)
	if string(data) != "existing private config" {
		t.Fatal("upgrade or rollback rewrote configuration")
	}
	if _, err = a.Uninstall(ctx, f); err != nil {
		t.Fatal(err)
	}
	if current, _ := a.Current(); current != "" {
		t.Fatal("uninstall left an active executable")
	}
	if _, err = os.Stat(plan.Config); err != nil {
		t.Fatal("uninstall deleted config")
	}
	if _, err = os.Stat(filepath.Join(a.Prefix, releaseName(first.Manifest))); err != nil {
		t.Fatal("rollback artifacts were destroyed")
	}
}
func TestFailedActivationAndInterruptedSwitchRecoverOldRelease(t *testing.T) {
	ctx := context.Background()
	a := testArtifacts(t)
	plan := testPlan(t)
	first := bundleFixture(t, "v1")
	second := bundleFixture(t, "v2")
	f := &fakeLifecycle{prefix: a.Prefix}
	if _, err := a.Install(ctx, first, plan, false, f); err != nil {
		t.Fatal(err)
	}
	f.failProbe = true
	if _, err := a.Install(ctx, second, plan, false, f); err == nil {
		t.Fatal("failed readiness accepted")
	}
	if current, _ := a.Current(); current != releaseName(first.Manifest) {
		t.Fatal("failure did not restore previous artifact")
	}
	receipt, err := a.Receipt()
	if err != nil || receipt.Pending != nil {
		t.Fatal("successful failure recovery left a pending transaction")
	}
	next := Selection{Release: releaseName(second.Manifest), Version: second.Manifest.Version, Checksums: second.TrustedChecksums}
	receipt.Pending = &next
	receipt.PendingAction = "install"
	if err = a.writeJSON("installation.json", receipt); err != nil {
		t.Fatal(err)
	}
	if err = a.switchTo(next.Release); err != nil {
		t.Fatal(err)
	}
	f.failProbe = false
	if _, err = a.Install(ctx, second, plan, false, f); err == nil {
		t.Fatal("interrupted transaction silently resumed")
	}
	if _, err = a.Rollback(ctx, f); err != nil {
		t.Fatal(err)
	}
	if current, _ := a.Current(); current != releaseName(first.Manifest) {
		t.Fatal("interrupted switch did not recover")
	}
}
func TestTrustAndPayloadValidationFailBeforeMutation(t *testing.T) {
	bundle := bundleFixture(t, "v1")
	a := testArtifacts(t)
	if _, err := Verify(bundle.Directory, "v1", strings.Repeat("0", 64)); err == nil {
		t.Fatal("untrusted checksum accepted")
	}
	if _, err := Verify(bundle.Directory, "v2", bundle.TrustedChecksums); err == nil {
		t.Fatal("wrong version accepted")
	}
	target := filepath.Join(bundle.Directory, "shiki-agent")
	if err := os.WriteFile(target, []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(bundle.Directory, "v1", bundle.TrustedChecksums); err == nil {
		t.Fatal("tampered payload accepted")
	}
	if _, err := a.Stage(bundle); err == nil {
		t.Fatal("payload changed after verification accepted during copy")
	}
	if current, _ := a.Current(); current != "" {
		t.Fatal("verification failure changed active version")
	}
	os.Remove(target)
	if err := os.Symlink("shiki", target); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(bundle.Directory, "v1", bundle.TrustedChecksums); err == nil {
		t.Fatal("symlink payload accepted")
	}
}
func TestBackupRejectsRunningWriterAndPreservesPrivateEvidence(t *testing.T) {
	a := testArtifacts(t)
	state := t.TempDir()
	config := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(config, []byte("private config"), 0600)
	os.WriteFile(filepath.Join(state, "state.json"), []byte("identity evidence"), 0600)
	lock, err := os.OpenFile(filepath.Join(state, "state.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	n := &Native{}
	plan := Plan{Config: config, StateDir: state}
	if _, err = n.Backup(context.Background(), plan, a.Prefix); err == nil {
		t.Fatal("backup raced a live writer")
	}
	lock.Close()
	path, err := n.Backup(context.Background(), plan, a.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "identity evidence") {
		t.Fatal("backup lost state evidence")
	}
}
func TestServicePreservesRuntimeIdentityAndRejectsInjection(t *testing.T) {
	plan := Plan{Role: "daemon", User: "ordinary", Home: "/home/ordinary", Path: "/usr/bin:/bin", Config: "/etc/shiki/daemon.yaml"}
	data, err := RenderService(plan, "/opt/shiki")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"User=ordinary", "WorkingDirectory=~\n", "KillMode=process", "PrivateTmp=false", "UMask=0077", "--config", "/etc/shiki/daemon.yaml"} {
		if !strings.Contains(string(data), required) {
			t.Fatal(required)
		}
	}
	if strings.Contains(string(data), "User=root") || strings.Contains(string(data), "tailscaled") {
		t.Fatal("service violates runtime ownership")
	}
	plan.Config = "/etc/config\nExecStart=/evil"
	if _, err = RenderService(plan, "/opt/shiki"); err == nil {
		t.Fatal("service injection accepted")
	}
}

func TestManifestReplacementBySpecialFileDoesNotBlock(t *testing.T) {
	bundle := bundleFixture(t, "v1")
	manifestPath := filepath.Join(bundle.Directory, ManifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			os.Remove(manifestPath)
			unix.Mkfifo(manifestPath, 0600)
			runtime.Gosched()
			os.Remove(manifestPath)
			os.WriteFile(manifestPath, data, 0600)
			runtime.Gosched()
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			_, _ = Verify(bundle.Directory, "v1", bundle.TrustedChecksums)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		close(stop)
		<-writerDone
		t.Fatal("special-file replacement blocked release verification")
	}
	close(stop)
	<-writerDone
}

func TestFirstActivationFailuresAndCrashRemainRecoverable(t *testing.T) {
	for _, phase := range []string{"prepare", "probe", "crash"} {
		t.Run(phase, func(t *testing.T) {
			a := testArtifacts(t)
			plan := testPlan(t)
			bundle := bundleFixture(t, "v1")
			f := &fakeLifecycle{prefix: a.Prefix}
			if phase == "crash" {
				release, err := a.Stage(bundle)
				if err != nil {
					t.Fatal(err)
				}
				pending := Selection{Release: release, Version: bundle.Manifest.Version, Checksums: bundle.TrustedChecksums}
				if err = a.writeJSON("installation.json", &Receipt{Format: 1, Plan: plan, Pending: &pending, PendingAction: "install"}); err != nil {
					t.Fatal(err)
				}
				if err = a.switchTo(release); err != nil {
					t.Fatal(err)
				}
				result, err := a.Rollback(context.Background(), f)
				if err != nil || result.State != "initial_activation_rolled_back" {
					t.Fatalf("initial crash could not recover: %v", err)
				}
			} else {
				f.failPrepare = phase == "prepare"
				f.failProbe = phase == "probe"
				if _, err := a.Install(context.Background(), bundle, plan, false, f); err == nil {
					t.Fatal("injected failure succeeded")
				}
			}
			found := false
			for _, event := range f.events {
				found = found || event == "remove_service"
			}
			if !found {
				t.Fatal("initial failed activation did not clean managed links/service")
			}
			receipt, err := a.Receipt()
			if err != nil || receipt.Pending != nil || receipt.Active.Release != "" {
				t.Fatal("initial recovery left a dead-end receipt")
			}
			if current, _ := a.Current(); current != "" {
				t.Fatal("initial recovery left a current artifact")
			}
			f.failPrepare = false
			f.failProbe = false
			if _, err = a.Install(context.Background(), bundle, plan, false, f); err != nil {
				t.Fatalf("retry after initial recovery failed: %v", err)
			}
		})
	}
}
func TestSystemPrefixRejectsOrdinaryUserWritableAncestor(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only ownership boundary; exercised in isolated Linux container")
	}
	parent := filepath.Join(t.TempDir(), "ordinary-owned")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "installation")
	if a, err := OpenArtifacts(target); err == nil {
		a.Close()
		t.Fatal("privileged installation accepted replaceable ancestor")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsafe ancestor was mutated before rejection")
	}
}
func TestServiceExecDollarIsLiteralAndEnvironmentUnchanged(t *testing.T) {
	plan := Plan{Role: "daemon", User: "ordinary", Home: "/home/literal$HOME", Path: "/usr/bin:/literal$PATH", Config: "/config/${HOME}/daemon.yaml"}
	data, err := RenderService(plan, "/opt/shiki")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `/config/$${HOME}/daemon.yaml`) {
		t.Fatal("ExecStart expanded a literal dollar")
	}
	if !strings.Contains(string(data), `Environment="HOME=/home/literal$HOME"`) {
		t.Fatal("Environment dollar was incorrectly doubled")
	}
}
func TestServiceReadRejectsFIFOAndOversizedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unit")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := readManagedFile(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO service accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO service read blocked")
	}
	os.Remove(path)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (64<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readManagedFile(path); err == nil {
		t.Fatal("oversized service accepted")
	}
}

func TestPendingReceiptFailureRestartsOldServiceOutsideCanceledContext(t *testing.T) {
	a := testArtifacts(t)
	plan := testPlan(t)
	f := &fakeLifecycle{prefix: a.Prefix}
	first := bundleFixture(t, "v1")
	if _, err := a.Install(context.Background(), first, plan, false, f); err != nil {
		t.Fatal(err)
	}
	second := bundleFixture(t, "v2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restartFailure := errors.New("injected old-service restart failure")
	f.startErr = restartFailure
	f.backupHook = func() error { cancel(); return a.root.Close() }
	before := len(f.events)
	_, err := a.Install(ctx, second, plan, false, f)
	if err == nil || !errors.Is(err, restartFailure) {
		t.Fatalf("receipt/recovery failure was hidden: %v", err)
	}
	if !reflect.DeepEqual(f.events[before:], []string{"stop", "backup", "start"}) {
		t.Fatal("old service was not restarted after journal failure", f.events[before:])
	}
	if f.startContextErr != nil {
		t.Fatal("recovery reused canceled transaction context")
	}
	if selected, err := os.Readlink(filepath.Join(a.Prefix, "current")); err != nil || selected != releaseName(first.Manifest) {
		t.Fatal("journal failure changed active artifact")
	}
}
