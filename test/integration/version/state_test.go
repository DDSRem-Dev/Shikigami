package version_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func unchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("persistent file changed: %s (%v)", path, err)
	}
}
func stateBytes(schema int) []byte {
	data, _ := json.Marshal(map[string]any{"schema_version": schema, "revision": 7, "buckets": map[string]any{}, "requests": map[string]any{}})
	return data
}
func seedFuture(t *testing.T, dir string) ([]byte, []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	future, previous := stateBytes(99), stateBytes(1)
	for name, data := range map[string][]byte{"state.json": future, "state.previous.json": previous} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return future, previous
}
func assertFutureUnchanged(t *testing.T, dir string, future, previous []byte) {
	t.Helper()
	unchanged(t, filepath.Join(dir, "state.json"), future)
	unchanged(t, filepath.Join(dir, "state.previous.json"), previous)
	matches, _ := filepath.Glob(filepath.Join(dir, "state.corrupt.*"))
	if len(matches) != 0 {
		t.Fatal("future schema treated as corruption")
	}
}

func TestIndependentProcessesRefuseFutureStateAndPreserveMigrationSource(t *testing.T) {
	lab := newLaboratory(t)
	t.Run("agent-config", func(t *testing.T) {
		_, path := lab.clientConfiguration(t, "future-config")
		future := []byte("schema_version: 99\n")
		if err := os.WriteFile(path, future, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.CommandContext(t.Context(), lab.agent, "--config", path).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "newer") {
			t.Fatalf("wrong future config refusal: %v %s", err, out)
		}
		unchanged(t, path, future)
	})
	t.Run("agent-resources", func(t *testing.T) {
		cfg, path := lab.clientConfiguration(t, "future-agent-state")
		dir := filepath.Join(cfg.Agent.StateDir, "resources")
		future, previous := seedFuture(t, dir)
		child := lab.start(t, lab.agent, nil, "--config", path)
		client := openFrontend(t, path)
		status, err := client.Agent().GetStatus(t.Context(), &pb.Empty{})
		if err != nil || status.State != "degraded" || status.ReasonCode != "AGENT_STATE_UNAVAILABLE" {
			t.Fatalf("future state not diagnosed: %v %v", status, err)
		}
		child.stop(t)
		assertFutureUnchanged(t, dir, future, previous)
	})
	t.Run("daemon-state", func(t *testing.T) {
		account, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		if account.Uid == "0" {
			t.Fatal("version acceptance requires an ordinary user; root is not an accepted skip")
		}
		dir := lab.directory(t, "future-daemon-state")
		future, previous := seedFuture(t, dir)
		cfg := config.DefaultDaemon(account.Username, dir)
		cfg.Tailscale.Enabled = false
		cfg.ProjectsDir = filepath.Join(dir, "projects")
		cfg.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: "127.0.0.1:9527", CertFile: lab.cert, KeyFile: lab.key}
		path := filepath.Join(lab.root, "daemon.yaml")
		if err := config.SaveDaemon(path, cfg); err != nil {
			t.Fatal(err)
		}
		out, err := exec.CommandContext(t.Context(), lab.daemon, "--config", path, "serve").CombinedOutput()
		if err == nil || !strings.Contains(string(out), "state schema is newer") {
			t.Fatalf("wrong future daemon-state refusal: %v %s", err, out)
		}
		assertFutureUnchanged(t, dir, future, previous)
	})
	t.Run("sdk-migration", func(t *testing.T) {
		dir := lab.directory(t, "migration-state")
		original := append(stateBytes(0), '\n')
		path := filepath.Join(dir, "state.json")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		child, fixture := lab.fixture(t, "migration-process", fixtureConfiguration{Mode: "state", Address: dir, Migrate: true})
		<-child.done
		if child.err != nil {
			t.Fatal(child.err)
		}
		ready, err := os.ReadFile(readFixture(t, fixture).Ready)
		if err != nil || !bytes.Contains(ready, []byte("opened")) {
			t.Fatalf("migration failed: %s %v", ready, err)
		}
		backups, err := filepath.Glob(path + ".pre-migration-v0-*.json")
		if err != nil || len(backups) != 1 {
			t.Fatalf("backup missing: %v %v", backups, err)
		}
		unchanged(t, backups[0], original)
		info, err := os.Stat(backups[0])
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("migration backup not private", err)
		}
		current, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			Schema   int    `json:"schema_version"`
			Revision uint64 `json:"revision"`
		}
		if err := json.Unmarshal(current, &snapshot); err != nil || snapshot.Schema != 1 || snapshot.Revision != 8 || !bytes.Contains(current, []byte("committed")) {
			t.Fatalf("migration not committed: %s %v", current, err)
		}
	})
}
