//go:build linux

package install

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
)

func ordinaryExtensionTestUser(t *testing.T) string {
	t.Helper()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if account.Uid == "0" {
		account, err = user.Lookup("nobody")
		if err != nil {
			t.Skip("fixture requires an ordinary account")
		}
	}
	return account.Username
}

func extensionRootFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte("ID=debian\nVERSION_ID=\"12\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func signedExtensionFixture(t *testing.T, bundle *Bundle) ExtensionOptions {
	t.Helper()
	root := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(root, "private.pem")
	pub := filepath.Join(root, "public.pem")
	if err = os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pub, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "shikigami-"+bundle.Manifest.Version+"-"+runtime.GOOS+"-"+runtime.GOARCH+".tar.gz")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	names := []string{ManifestName, ChecksumsName}
	for _, entry := range bundle.Manifest.Files {
		names = append(names, entry.Name)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(bundle.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	pin := filepath.Join(root, "pin")
	if err = os.WriteFile(pin, []byte(bundle.TrustedChecksums+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	signature := archive + ".sig.json"
	if err = releaseauth.Sign(t.Context(), releaseauth.SignOptions{Archive: archive, ChecksumsPin: pin, PrivateKey: key, Signature: signature, Version: bundle.Manifest.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}); err != nil {
		t.Fatal(err)
	}
	return ExtensionOptions{Component: "all", User: ordinaryExtensionTestUser(t), Bundle: bundle.Directory, Version: bundle.Manifest.Version, Archive: archive, Signature: signature, PublicKey: pub, RootPrefix: extensionRootFixture(t), StageOnly: true, Yes: true}
}

func TestSupportStageIsSignedAndInertWithNoAlternateProductionRoot(t *testing.T) {
	bundle := optionalBundleFixture(t)
	options := signedExtensionFixture(t, bundle)
	plan, err := ExtensionPlatformPlan(t.Context(), "setup", options)
	if err != nil || len(plan.Blocked) != 0 {
		t.Fatalf("invalid fixture plan: %v %+v", err, plan)
	}
	if len(plan.Commands) != 0 {
		t.Fatal("stage-only fixture contains executable host actions", plan.Commands)
	}
	if err = ApplyExtensionPlan(t.Context(), "setup", options); err != nil {
		t.Fatal(err)
	}
	for _, name := range linuxSupportNames {
		data, err := os.ReadFile(filepath.Join(options.RootPrefix, "usr/lib/shikigami", name))
		if err != nil || string(data) != "inert signed support "+name {
			t.Fatal("signed support did not stage", name, err)
		}
	}
	if _, err = os.Stat(filepath.Join(options.RootPrefix, "var/lib/shikigami-install/STAGED-NOT-ACTIVE")); err != nil {
		t.Fatal(err)
	}
	options.StageOnly = false
	if _, err = ExtensionPlatformPlan(t.Context(), "setup", options); err == nil {
		t.Fatal("production accepted an arbitrary root prefix")
	}
}

func TestSupportStageRejectsTamperingBeforeAnyArtifactWrite(t *testing.T) {
	bundle := optionalBundleFixture(t)
	options := signedExtensionFixture(t, bundle)
	if err := os.WriteFile(filepath.Join(bundle.Directory, "shiki-region-helper"), []byte("untrusted helper"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ApplyExtensionPlan(t.Context(), "setup", options); err == nil {
		t.Fatal("tampered helper accepted")
	}
	if _, err := os.Lstat(filepath.Join(options.RootPrefix, "usr")); !os.IsNotExist(err) {
		t.Fatal("failed authentication wrote staging artifacts", err)
	}
}

func TestSupportPlanNeedsExplicitYesAndNeverAcceptsArbitraryInputs(t *testing.T) {
	options := ExtensionOptions{Component: "all", User: ordinaryExtensionTestUser(t), StageOnly: true, RootPrefix: extensionRootFixture(t)}
	if err := ApplyExtensionPlan(t.Context(), "setup", options); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	for _, component := range []string{"apt", "browser --allow-unauthenticated", "https://arbitrary.invalid/repo"} {
		options.Component = component
		if _, err := ExtensionPlatformPlan(t.Context(), "setup", options); err == nil {
			t.Fatal("arbitrary component accepted", component)
		}
	}
}

func TestRegionGrantHasOnlyExactUserScopedActions(t *testing.T) {
	policy := regionSudoers(1234)
	grant := "#1234 ALL=(root) NOPASSWD: "
	var actions []string
	for _, line := range strings.Split(policy, "\n") {
		if strings.HasPrefix(line, grant) {
			actions = strings.Split(strings.TrimPrefix(line, grant), ", ")
		}
	}
	allowed := map[string]bool{}
	for _, action := range []string{"inspect", "plan", "apply", "confirm", "rollback", "operation"} {
		allowed[RegionHelperPath+" "+action] = true
	}
	if len(actions) != len(allowed) {
		t.Fatal("incorrect grant action count", policy)
	}
	for _, action := range actions {
		if !allowed[action] {
			t.Fatal("unrestricted helper grant", action)
		}
	}
	for _, forbidden := range []string{"*", " activate", " prepare-uninstall", " guard", " --root"} {
		if strings.Contains(policy, forbidden) {
			t.Fatal("privileged administrative action leaked into sudoers", forbidden)
		}
	}
}

func TestRegionUninstallRequiresCleanQuiescedProof(t *testing.T) {
	for _, status := range []regionInstallStatus{
		{State: "ready"},
		{State: "rollback_failed"},
		{State: "rollback_conflict"},
		{State: "dns_recovery_conflict"},
		{State: "awaiting_confirmation", PendingOperationID: "op_pending"},
		{State: "disabled", PendingOperationID: "op_pending"},
		{},
	} {
		if err := regionUninstallReady(status); err == nil {
			t.Fatalf("unsafe recovery state accepted: %+v", status)
		}
	}
	if err := regionUninstallReady(regionInstallStatus{State: "disabled"}); err != nil {
		t.Fatal(err)
	}
}

func TestFreshChromeDefaultsKeepSourceRegistrationExplicit(t *testing.T) {
	defaults := string(chromeInitialDefaults())
	if !strings.Contains(defaults, "repo_add_once=\"false\"") || !strings.Contains(defaults, "repo_reenable_on_distupgrade=\"false\"") {
		t.Fatal("fresh Chrome would add an implicit repository")
	}
}

func TestVerboseExecutionDoesNotConfuseTruncatedLogsWithFailure(t *testing.T) {
	data, err := extensionExec(t.Context(), "", "/usr/bin/head", false, "-c", "70000", "/dev/zero")
	if err != nil || len(data) != 64<<10 {
		t.Fatalf("successful verbose command was rejected: bytes=%d err=%v", len(data), err)
	}
	if _, err = extensionCommand(t.Context(), "", "/usr/bin/head", "-c", "70000", "/dev/zero"); err == nil {
		t.Fatal("parsed command output was silently truncated")
	}
}

func TestBrowserDependenciesInstallNativeGTK3Bridge(t *testing.T) {
	for _, component := range []string{"region", "browser", "all"} {
		bridge := 0
		for _, pkg := range extensionPackages(ExtensionOptions{Component: component}) {
			if pkg == "ibus" || pkg == "ibus-daemon" || pkg == "ibus-gtk3" {
				t.Fatal("browser support must not install a second IME daemon", pkg)
			}
			if pkg == "fcitx5-frontend-gtk3" {
				bridge++
			}
		}
		expected := 1
		if component == "region" {
			expected = 0
		}
		if bridge != expected {
			t.Fatalf("%s native GTK3 bridge count = %d, want %d", component, bridge, expected)
		}
	}
}
