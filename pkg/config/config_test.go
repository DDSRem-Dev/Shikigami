package config

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func privateTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func ordinaryAccount(t *testing.T) *user.User {
	t.Helper()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if account.Uid == "0" {
		account, err = user.Lookup("nobody")
		if err != nil {
			t.Skip("requires a known ordinary account")
		}
	}
	return account
}
func privateConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(privateTestDir(t), "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDaemonDefaultsUseRunUserHomeAndExplicitTLS(t *testing.T) {
	account := ordinaryAccount(t)
	path := privateConfig(t, "run_user: "+account.Username+"\nstate_dir: ~/.shiki-daemon\n")
	cfg, err := LoadDaemon(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MigratedFrom == nil || *cfg.MigratedFrom != 0 {
		t.Fatal("legacy schema migration not reported")
	}
	if cfg.StateDir != filepath.Join(account.HomeDir, ".shiki-daemon") {
		t.Fatal("~ expanded against installer instead of run_user")
	}
	if cfg.Server.Port != 9527 || !cfg.Tailscale.Enabled || cfg.Claude.Executable != "claude" || cfg.ResourceLimits.MaxSessions != 10 {
		t.Fatal("wrong defaults")
	}
	if cfg.Claude.AutoRestart.Enabled {
		t.Fatal("automatic restart enabled by default")
	}
	if err = SaveDaemon(path, cfg); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".previous")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(backup), "schema_version") {
		t.Fatal("migration did not preserve original bytes")
	}
	loaded, err := LoadDaemon(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MigratedFrom != nil || loaded.SchemaVersion != 1 {
		t.Fatal("migration not persisted")
	}
	loaded.Server.DirectTLS = DirectTLS{Enabled: true, Address: ":9528", CertFile: "/tmp/cert.pem"}
	if err = loaded.NormalizeAndValidate(); err == nil {
		t.Fatal("partial TLS accepted")
	}
	loaded.Server.DirectTLS.KeyFile = "/tmp/key.pem"
	loaded.Tailscale.Enabled = false
	if err = loaded.NormalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	loaded.RunUser = "root"
	if err = loaded.NormalizeAndValidate(); err == nil {
		t.Fatal("root daemon accepted")
	}
}

func TestStrictDecodeRejectsUnknownKeysSecretsDuplicatesAndDurations(t *testing.T) {
	account := ordinaryAccount(t)
	prefix := "run_user: " + account.Username + "\nstate_dir: /tmp/shiki-test-state\n"
	for name, extra := range map[string]string{
		"unknown":           "unexpected: value\n",
		"secret":            "tailscale:\n  auth_key: secret-never-echo\n",
		"duplicate":         "state_dir: /tmp/another\n",
		"duration":          "claude:\n  auto_restart:\n    backoff: 7d\n",
		"negative-duration": "claude:\n  auto_restart:\n    backoff: -1s\n",
		"duration-type":     "claude:\n  auto_restart:\n    backoff: 1\n",
		"multiple-docs":     "---\nrun_user: other\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := privateConfig(t, prefix+extra)
			_, err := LoadDaemon(path)
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), "secret-never-echo") {
				t.Fatal("secret leaked in error")
			}
		})
	}
}

func TestClientSaveConflictAndCorruptFilePreservation(t *testing.T) {
	path := filepath.Join(privateTestDir(t), "config.yaml")
	cfg := DefaultClient()
	cfg.Agent.Socket = "/tmp/shiki-test/agent.sock"
	if err := SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	one, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}
	two, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}
	one.Agent.AutoStart = false
	if err = SaveClient(path, one); err != nil {
		t.Fatal(err)
	}
	two.Tailscale.Hostname = "second-writer"
	if err = SaveClient(path, two); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("lost update accepted: %v", err)
	}
	if err = os.WriteFile(path, []byte("[broken yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = SaveClient(path, one); err == nil {
		t.Fatal("corrupt config overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "[broken yaml" {
		t.Fatal("corrupt evidence changed")
	}
	info, _ := os.Stat(path + ".previous")
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup mode is not 0600")
	}
}

func TestFutureSchemaAndSymlinkNeverOverwritten(t *testing.T) {
	path := privateConfig(t, "schema_version: 999\n")
	if _, err := LoadClient(path); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("got %v", err)
	}
	cfg := DefaultClient()
	cfg.Agent.Socket = "/tmp/shiki-test/agent.sock"
	if err := SaveClient(path, cfg); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("future config save: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "schema_version: 999\n" {
		t.Fatal("future config overwritten")
	}
	link := filepath.Join(privateTestDir(t), "config.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClient(link); err == nil {
		t.Fatal("symlink config accepted")
	}
	if err := SaveClient(link, cfg); err == nil {
		t.Fatal("symlink config replaced")
	}
}

func TestClientReferencesAndPaths(t *testing.T) {
	cfg := DefaultClient()
	cfg.Agent.Socket = "/tmp/shiki-test/agent.sock"
	cfg.Servers["vps"] = Server{DirectTLSAddress: "vps.example:443", TLSServerName: "vps.example", TokenRef: "laptop"}
	cfg.Projects["~/myapp"] = ProjectMapping{Server: "vps", ProjectID: "proj_one", SessionHints: map[string]string{"frontend": "web"}}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if _, ok := cfg.Projects[filepath.Join(home, "myapp")]; !ok {
		t.Fatal("mapping ~ not expanded")
	}
	server := cfg.Servers["vps"]
	server.TokenRef = "shiki_secret"
	cfg.Servers["vps"] = server
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("token accepted in config")
	}
	server.TokenRef = "laptop"
	cfg.Servers["vps"] = server
	mapping := cfg.Projects[filepath.Join(home, "myapp")]
	mapping.SessionHints = map[string]string{"../escape": "bad"}
	cfg.Projects[filepath.Join(home, "myapp")] = mapping
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("escaping hint accepted")
	}
}

func TestConfigPermissionsAndNoDefaultOverwrite(t *testing.T) {
	path := privateConfig(t, "schema_version: 1\n")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClient(path); err == nil {
		t.Fatal("public config accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultClient()
	cfg.Agent.Socket = "/tmp/shiki-test/agent.sock"
	if err := SaveClient(path, cfg); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("defaults overwrote existing file: %v", err)
	}
}

func TestDurationRoundTrip(t *testing.T) {
	account := ordinaryAccount(t)
	path := filepath.Join(privateTestDir(t), "daemon.yaml")
	cfg := DefaultDaemon(account.Username, "/tmp/shiki-state")
	cfg.Claude.Args = []string{"--model", "sonnet"}
	cfg.Claude.AutoRestart.Backoff = Duration(1500 * time.Millisecond)
	if err := SaveDaemon(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDaemon(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Claude.AutoRestart.Backoff.Value() != 1500*time.Millisecond || len(loaded.Claude.Args) != 2 {
		t.Fatal("duration/argv round trip lost")
	}
}
