package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedPluginBackupExcludesOnlyRetainedStoreArtifacts(t *testing.T) {
	for _, layout := range []string{"current", "parked"} {
		t.Run(layout, func(t *testing.T) {
			plan, artifacts, _ := managedPluginPlan(t)
			// This is the daemon's default nesting, also legal for a custom client:
			// executable packages are under StateDir, durable worker state is not
			// inside the package store itself.
			plan.StateDir = filepath.Join(plan.Home, ".shiki", "daemon-state")
			plan.PluginsDir = filepath.Join(plan.StateDir, "plugins")
			configureManagedPlugins(t, plan, artifacts.Prefix)
			retained := publishManagedPlugin(t, plan.PluginsDir)
			keep := map[string][]byte{
				"host-state.json":                   []byte("durable host state"),
				"plugin-state/weather/state.json":   []byte("durable third-party worker state"),
				".ordinary-hidden-state":            []byte("unrelated private state"),
				".plugins.shiki-store-other/marker": []byte("not the configured parked store"),
				".plugins.shiki-owner.json.bak":     []byte("not the configured ownership marker"),
			}
			for name, data := range keep {
				path := filepath.Join(plan.StateDir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// A sparse private staging artifact proves space estimation never
			// counts or copies package data as ordinary state. No large allocation
			// or file content transfer is needed to exercise the 8 GiB gate.
			large := filepath.Join(plan.PluginsDir, ".plugin-stage-retained", "oversized-evidence")
			if err := os.WriteFile(large, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(large, maxBackupBytes+1); err != nil {
				t.Fatal(err)
			}
			packageRoot := plan.PluginsDir
			if layout == "parked" {
				lease, err := preparePluginLinks(t.Context(), plan, artifacts.Prefix, pluginLinksLegacy)
				closePluginLease(t, lease, err)
				parked, _ := pluginLinkPaths(plan.PluginsDir)
				packageRoot = filepath.Join(plan.StateDir, parked)
			}
			native := &Native{}
			inventory, err := native.collectBackup(t.Context(), plan, false)
			if err != nil {
				t.Fatal("package artifacts entered the host backup inventory:", err)
			}
			defer inventory.Close()
			seen := make(map[string]bool)
			for _, entry := range inventory.entries {
				name := entry.header.Name
				seen[name] = true
				for _, excluded := range []string{"state/plugins", "state/.plugins.shiki-store", "state/.plugins.shiki-owner.json"} {
					if name == excluded || strings.HasPrefix(name, excluded+"/") {
						t.Fatalf("retained package store ownership or content entered backup: %s", name)
					}
				}
			}
			for name := range keep {
				if !seen[filepath.ToSlash(filepath.Join("state", name))] {
					t.Fatalf("ordinary durable state was excluded with the package store: %s", name)
				}
			}
			if inventory.bytes > 1<<20 {
				t.Fatal("package bytes inflated backup space estimates:", inventory.bytes)
			}
			assertManagedPluginBytes(t, packageRoot, retained)
			if info, err := os.Stat(filepath.Join(packageRoot, ".plugin-stage-retained", "oversized-evidence")); err != nil || info.Size() != maxBackupBytes+1 {
				t.Fatal("backup inventory modified retained package evidence:", err)
			}
		})
	}
}
