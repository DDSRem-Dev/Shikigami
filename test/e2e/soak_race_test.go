//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Production workers intentionally receive a filtered environment and discard
// unstructured stderr. A test-only executable wrapper sets the race runtime's
// options after that filtering, then execs the unchanged race-built worker.
// The manifest hashes this wrapper, and resource sampling tracks the real exe.
func configureSoakRace(t *testing.T, directory, bin string) []string {
	t.Helper()
	if applicationSoakDuration(t) == 0 {
		return nil
	}
	reports := filepath.Join(directory, "race-report")
	options := "halt_on_error=1 log_path=" + reports
	plugins := []string{"sync"}
	if mixedSoakClients(t) > 0 {
		plugins = append(plugins, "port")
	}
	for _, id := range plugins {
		plugin := filepath.Join(bin, "shiki-plugin-"+id)
		actual := plugin + ".race"
		if err := os.Rename(plugin, actual); err != nil {
			t.Fatal(err)
		}
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
		script := fmt.Sprintf("#!/bin/sh\nGORACE=%s exec %s \"$@\"\n", quote(options), quote(actual))
		if err := os.WriteFile(plugin, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Registered before the core process cleanups, so their LIFO cleanup runs
	// first and a report produced during normal/failed shutdown is also found.
	t.Cleanup(func() {
		files, err := filepath.Glob(reports + ".*")
		if err != nil {
			t.Error(err)
		}
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("cannot read child race report: %v", err)
				continue
			}
			t.Errorf("shipped child produced a race report %s:\n%s", filepath.Base(path), data[:min(len(data), 64<<10)])
		}
	})
	return []string{"GORACE=" + options}
}
