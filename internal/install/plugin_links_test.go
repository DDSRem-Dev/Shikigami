package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/pluginmarket"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
)

func managedPluginPlan(t *testing.T) (Plan, *Artifacts, *Bundle) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("managed plugin layout must be exercised as an ordinary user")
	}
	directory, err := os.MkdirTemp("/tmp", "shiki-layout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	home, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		Role: "client", User: account.Username, Home: home,
		Config:       filepath.Join(home, ".shiki", "config.yaml"),
		StateDir:     filepath.Join(home, ".shiki", "agent"),
		TailscaleDir: filepath.Join(home, ".shiki", "tailscale"),
		// This is the actual default plugins_dir, not the custom-directory path
		// covered by the marketplace process integration test.
		PluginsDir: filepath.Join(home, ".shiki", "plugins"),
		BinDir:     filepath.Join(home, ".local", "bin"), Path: "/usr/bin:/bin", Port: 9527,
	}
	artifacts, err := OpenArtifacts(filepath.Join(home, ".shiki", "install"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { artifacts.Close() })
	bundle := bundleFixture(t, buildinfo.Version)
	selected, err := artifacts.Stage(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err = artifacts.switchTo(selected); err != nil {
		t.Fatal(err)
	}
	return plan, artifacts, bundle
}

func configureManagedPlugins(t *testing.T, plan Plan, prefix string) {
	t.Helper()
	data, err := json.Marshal(UserOperation{Prefix: prefix, Plan: plan, ExpectedVersion: buildinfo.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err = UserHelper(t.Context(), "config", bytes.NewReader(data), io.Discard); err != nil {
		t.Fatal(err)
	}
}

func publishManagedPlugin(t *testing.T, base string) map[string][]byte {
	t.Helper()
	store, err := pluginmarket.OpenInstallStore(t.Context(), base)
	if err != nil {
		t.Fatal("default managed plugins_dir is not writable by marketplace:", err)
	}
	stage, err := os.MkdirTemp(base, ".plugin-stage-")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	payloads := map[string][]byte{
		"manifest.json":        []byte(`{"id":"weather","version":"1.0.0","binary":"shiki-plugin-weather"}`),
		"shiki-plugin-weather": []byte("ordinary-user third-party executable fixture"),
	}
	for name, data := range payloads {
		if err = os.WriteFile(filepath.Join(stage, name), data, 0600); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	tx, err := store.Begin("weather", "install", filepath.Base(stage), false, "")
	if err == nil {
		err = store.Publish(tx)
	}
	if err == nil {
		err = store.Finish(tx)
	}
	err = errors.Join(err, store.Close())
	if err != nil {
		t.Fatal(err)
	}
	retained := make(map[string][]byte)
	for name, data := range payloads {
		retained[filepath.Join("weather", name)] = data
	}
	// An installer must preserve even an interrupted marketplace's private
	// journal/stage/backup evidence without interpreting or deleting it.
	for name, data := range map[string][]byte{
		".install-transaction.json":      []byte("private journal evidence"),
		".plugin-stage-retained/marker":  []byte("private stage evidence"),
		".plugin-backup-retained/marker": []byte("private backup evidence"),
	} {
		path := filepath.Join(base, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		retained[name] = data
	}
	return retained
}

func assertManagedPluginBytes(t *testing.T, base string, expected map[string][]byte) {
	t.Helper()
	for name, data := range expected {
		actual, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || !bytes.Equal(data, actual) {
			t.Fatalf("third-party data changed at %s: %v", name, err)
		}
	}
}

func closePluginLease(t *testing.T, lease *pluginLinkLease, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedPluginDefaultDirectoryAllowsMarketplaceAndVerifiedBuiltins(t *testing.T) {
	plan, artifacts, bundle := managedPluginPlan(t)
	configureManagedPlugins(t, plan, artifacts.Prefix)
	info, err := os.Lstat(plan.PluginsDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("default plugins_dir is not a real private directory:", info, err)
	}
	if target, err := os.Readlink(filepath.Join(plan.PluginsDir, ".builtin")); err != nil || target != filepath.Join(artifacts.Prefix, "current") {
		t.Fatal("built-in pointer does not follow the managed current release:", target, err)
	}
	retained := publishManagedPlugin(t, plan.PluginsDir)
	manifest, err := host.ResolveBuiltinManifest(plan.PluginsDir, "sync", "client", bundle.Manifest.Version)
	if err != nil {
		t.Fatal("managed built-in cannot be resolved:", err)
	}
	payload, err := os.ReadFile(manifest.Executable)
	if err != nil || digest(payload) != manifest.SHA256 || filepath.Dir(manifest.Executable) != filepath.Join(plan.PluginsDir, ".builtin") {
		t.Fatal("managed built-in did not preserve its verified payload:", err)
	}
	if info, err := os.Lstat(manifest.Executable); err != nil || !info.Mode().IsRegular() {
		t.Fatal("built-in executable leaf is not a regular file:", err)
	}
	if _, err = Verify(filepath.Join(artifacts.Prefix, releaseName(bundle.Manifest)), bundle.Manifest.Version, bundle.TrustedChecksums); err != nil {
		t.Fatal("marketplace publication wrote into the immutable release:", err)
	}
	if err = removeUserLinks(t.Context(), plan, artifacts.Prefix); err != nil {
		t.Fatal(err)
	}
	assertManagedPluginBytes(t, plan.PluginsDir, retained)
	if _, err = os.Lstat(filepath.Join(plan.PluginsDir, ".builtin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall retained an active built-in pointer:", err)
	}
	configureManagedPlugins(t, plan, artifacts.Prefix)
	assertManagedPluginBytes(t, plan.PluginsDir, retained)
}

func TestManagedPluginStoreSurvivesLegacyMigrationRollbackAndReupgrade(t *testing.T) {
	plan, artifacts, _ := managedPluginPlan(t)
	target := filepath.Join(artifacts.Prefix, "current")
	if err := os.Symlink(target, plan.PluginsDir); err != nil {
		t.Fatal(err)
	}
	configureManagedPlugins(t, plan, artifacts.Prefix)
	retained := publishManagedPlugin(t, plan.PluginsDir)
	original, err := os.Stat(plan.PluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.Stat(filepath.Join(plan.PluginsDir, ".install.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := preparePluginLinks(t.Context(), plan, artifacts.Prefix, pluginLinksLegacy)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the frozen helper's whole-root link operation on the actual
	// migrated directory. Independent public-helper acceptance is run separately.
	if err = managedLink(plan.PluginsDir, target); err != nil {
		lease.Close()
		t.Fatal("old helper's whole-root contract was broken:", err)
	}
	closePluginLease(t, lease, nil)
	parked, _ := pluginLinkPaths(plan.PluginsDir)
	parked = filepath.Join(filepath.Dir(plan.PluginsDir), parked)
	assertManagedPluginBytes(t, parked, retained)
	moved, err := os.Stat(parked)
	if err != nil || !os.SameFile(original, moved) {
		t.Fatal("rollback copied or replaced the private plugin store:", err)
	}
	movedLock, err := os.Stat(filepath.Join(parked, ".install.lock"))
	if err != nil || !os.SameFile(lock, movedLock) {
		t.Fatal("rollback lost marketplace lock identity:", err)
	}
	if store, err := pluginmarket.OpenInstallStore(t.Context(), plan.PluginsDir); err == nil {
		store.Close()
		t.Fatal("marketplace followed the old root link into the release")
	}
	// The actual old uninstall only knows this root link; parked data survives.
	if err = unlinkManaged(plan.PluginsDir, target); err != nil {
		t.Fatal(err)
	}
	assertManagedPluginBytes(t, parked, retained)
	configureManagedPlugins(t, plan, artifacts.Prefix)
	assertManagedPluginBytes(t, plan.PluginsDir, retained)
	restored, err := os.Stat(plan.PluginsDir)
	if err != nil || !os.SameFile(original, restored) {
		t.Fatal("re-upgrade did not restore the original store:", err)
	}
	if _, err = os.Lstat(parked); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("re-upgrade retained an ambiguous parked entry:", err)
	}
}

func TestManagedPluginLayoutRecoversAtomicExchangeBoundaries(t *testing.T) {
	for _, boundary := range []string{"legacy-shadow", "legacy-exchanged", "current-exchanged", "owner-published", "initial-directory"} {
		t.Run(boundary, func(t *testing.T) {
			plan, artifacts, _ := managedPluginPlan(t)
			target := filepath.Join(artifacts.Prefix, "current")
			parked, ownerName := pluginLinkPaths(plan.PluginsDir)
			parentPath := filepath.Dir(plan.PluginsDir)
			parent, err := os.OpenRoot(parentPath)
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Close()
			var retained map[string][]byte
			if boundary == "owner-published" || boundary == "initial-directory" {
				if err = parent.Symlink(target, filepath.Base(plan.PluginsDir)); err != nil {
					t.Fatal(err)
				}
				_, err = createPluginLinkOwner(parent, ownerName, pluginLinkOwner{Format: 1, Prefix: artifacts.Prefix, PluginsDir: plan.PluginsDir})
				if err == nil && boundary == "initial-directory" {
					err = parent.Mkdir(parked, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			} else {
				configureManagedPlugins(t, plan, artifacts.Prefix)
				retained = publishManagedPlugin(t, plan.PluginsDir)
				if boundary == "legacy-shadow" {
					err = parent.Symlink(target, parked)
				} else {
					lease, prepareErr := preparePluginLinks(t.Context(), plan, artifacts.Prefix, pluginLinksLegacy)
					closePluginLease(t, lease, prepareErr)
					if boundary == "current-exchanged" {
						fd, openErr := parent.Open(".")
						if openErr != nil {
							t.Fatal(openErr)
						}
						err = movePluginRoot(int(fd.Fd()), parked, filepath.Base(plan.PluginsDir), true)
						fd.Close()
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			// A new helper instance derives the interrupted transition from the
			// durable marker, directory and managed link; no data cleanup is guessed.
			configureManagedPlugins(t, plan, artifacts.Prefix)
			assertManagedPluginBytes(t, plan.PluginsDir, retained)
			if info, err := os.Lstat(plan.PluginsDir); err != nil || !info.IsDir() {
				t.Fatal("recovery did not restore a real default plugin directory:", err)
			}
			if _, err = os.Lstat(filepath.Join(parentPath, parked)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("recovery left an ambiguous parked entry:", err)
			}
		})
	}
}

func TestManagedPluginLayoutRefusesForeignPathsAndSymlinkMetadata(t *testing.T) {
	for _, fault := range []string{"existing-directory", "foreign-root-link", "foreign-parked-directory", "owner-symlink", "lock-symlink", "builtin-leaf"} {
		t.Run(fault, func(t *testing.T) {
			plan, artifacts, _ := managedPluginPlan(t)
			parked, ownerName := pluginLinkPaths(plan.PluginsDir)
			parent := filepath.Dir(plan.PluginsDir)
			outside := filepath.Join(plan.Home, "must-not-change")
			if err := os.WriteFile(outside, []byte("original bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "existing-directory":
				if err := os.Mkdir(plan.PluginsDir, 0700); err != nil {
					t.Fatal(err)
				}
			case "foreign-root-link":
				if err := os.Symlink(plan.Home, plan.PluginsDir); err != nil {
					t.Fatal(err)
				}
			case "foreign-parked-directory":
				if err := os.Mkdir(filepath.Join(parent, parked), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				configureManagedPlugins(t, plan, artifacts.Prefix)
				path := filepath.Join(parent, ownerName)
				if fault == "lock-symlink" {
					path = filepath.Join(plan.PluginsDir, ".install.lock")
				} else if fault == "builtin-leaf" {
					path = filepath.Join(plan.PluginsDir, ".builtin")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if fault == "builtin-leaf" {
					if err := os.WriteFile(path, []byte("unmanaged reserved leaf"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := installUserLinks(t.Context(), plan, artifacts.Prefix); err == nil {
				t.Fatal("unsafe plugin layout was accepted:", fault)
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "original bytes" {
				t.Fatal("symlink target changed:", err)
			}
		})
	}
}

func TestManagedPluginLayoutWaitsForMarketplaceLockAndCancels(t *testing.T) {
	plan, artifacts, _ := managedPluginPlan(t)
	configureManagedPlugins(t, plan, artifacts.Prefix)
	retained := publishManagedPlugin(t, plan.PluginsDir)
	store, err := pluginmarket.OpenInstallStore(t.Context(), plan.PluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	lease, err := preparePluginLinks(ctx, plan, artifacts.Prefix, pluginLinksLegacy)
	cancel()
	if err == nil {
		lease.Close()
		store.Close()
		t.Fatal("layout transition bypassed the active marketplace lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		store.Close()
		t.Fatal("layout lock cancellation lost its cause:", err)
	}
	if info, err := os.Lstat(plan.PluginsDir); err != nil || !info.IsDir() {
		store.Close()
		t.Fatal("blocked layout transition changed the default root:", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err = preparePluginLinks(t.Context(), plan, artifacts.Prefix, pluginLinksLegacy)
	closePluginLease(t, lease, err)
	parked, _ := pluginLinkPaths(plan.PluginsDir)
	assertManagedPluginBytes(t, filepath.Join(filepath.Dir(plan.PluginsDir), parked), retained)
}

func TestManagedPluginDirectoryNoReplaceNeverOverwritesAnExistingRoot(t *testing.T) {
	plan, _, _ := managedPluginPlan(t)
	parent, err := os.OpenRoot(filepath.Dir(plan.PluginsDir))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	for _, name := range []string{"old-store", "new-store"} {
		if err = parent.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	old, _ := parent.Lstat("old-store")
	current, _ := parent.Lstat("new-store")
	fd, err := parent.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	if err = movePluginRoot(int(fd.Fd()), "old-store", "new-store", false); !errors.Is(err, os.ErrExist) {
		t.Fatal("no-replace publication did not refuse an occupied destination:", err)
	}
	oldAfter, _ := parent.Lstat("old-store")
	currentAfter, _ := parent.Lstat("new-store")
	if !os.SameFile(old, oldAfter) || !os.SameFile(current, currentAfter) {
		t.Fatal("no-replace publication replaced a directory")
	}
}
