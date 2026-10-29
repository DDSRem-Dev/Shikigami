package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These tests use only the pre-existing public loaders, so the same tests can
// run against the original implementation to verify the missing diagnostic.
func configLoaders() map[string]func(string) error {
	return map[string]func(string) error{
		"client": func(path string) error { _, err := LoadClient(path); return err },
		"daemon": func(path string) error { _, err := LoadDaemon(path); return err },
	}
}

func TestLoadRecoveryDiagnosticPreservesEvidence(t *testing.T) {
	const secret = "token-key-value-must-not-appear"
	for name, load := range configLoaders() {
		t.Run(name, func(t *testing.T) {
			prefix := "agent:\n  socket: /tmp/shiki-recovery-test/agent.sock\n"
			if name == "daemon" {
				prefix = "run_user: " + ordinaryAccount(t).Username + "\nstate_dir: /tmp/shiki-recovery-test\n"
			}
			for kind, data := range map[string]string{
				"syntax":   "[" + secret,
				"unknown":  "unexpected: " + secret + "\n",
				"type":     "schema_version: " + secret + "\n",
				"schema":   "schema_version: -1\n# " + secret + "\n",
				"semantic": "tailscale:\n  hostname: '" + secret + " invalid'\n",
				"size":     strings.Repeat("#", (1<<20)+1) + secret,
			} {
				data = prefix + data
				t.Run(kind, func(t *testing.T) {
					for _, backupPresent := range []bool{false, true} {
						label := "no-backup"
						if backupPresent {
							label = "with-backup"
						}
						t.Run(label, func(t *testing.T) {
							path := privateConfig(t, data)
							backup := []byte(prefix + "schema_version: 1\n# preserved-backup-" + secret + "\n")
							if backupPresent {
								if err := os.WriteFile(path+".previous", backup, 0600); err != nil {
									t.Fatal(err)
								}
								if err := load(path + ".previous"); err != nil {
									t.Fatalf("backup fixture must be valid: %v", err)
								}
							}
							err := load(path)
							if err == nil {
								t.Fatal("invalid configuration accepted or backup silently restored")
							}
							for _, phrase := range []string{"original file was left unchanged", "preserve the original file", ".previous backup", "only after validating", "explicitly restart"} {
								if !strings.Contains(err.Error(), phrase) {
									t.Errorf("missing recovery instruction %q: %v", phrase, err)
								}
							}
							if strings.Contains(err.Error(), secret) {
								t.Error("configuration or backup contents leaked in diagnostic")
							}
							assertConfigBytes(t, path, []byte(data))
							if backupPresent {
								assertConfigBytes(t, path+".previous", backup)
							} else if _, err := os.Lstat(path + ".previous"); !errors.Is(err, os.ErrNotExist) {
								t.Fatalf("loading created a backup: %v", err)
							}
						})
					}
				})
			}
		})
	}
}

func TestLoadRecoveryDiagnosticFutureSchema(t *testing.T) {
	for name, load := range configLoaders() {
		t.Run(name, func(t *testing.T) {
			data := []byte("schema_version: 999\n# future-private-value\n")
			path := privateConfig(t, string(data))
			err := load(path)
			if !errors.Is(err, ErrFutureSchema) {
				t.Fatalf("future schema classification lost: %v", err)
			}
			for _, phrase := range []string{"compatible program version", "preserve the original file", "validating its compatibility", "do not lower schema_version", "explicitly restart"} {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("missing future-schema instruction %q: %v", phrase, err)
				}
			}
			if strings.Contains(err.Error(), "future-private-value") || strings.Contains(err.Error(), "corrupt") {
				t.Errorf("future schema contents disclosed or misclassified: %v", err)
			}
			assertConfigBytes(t, path, data)
		})
	}
}

func TestLoadRecoveryDiagnosticKeepsMissingAndAccessErrors(t *testing.T) {
	for name, load := range configLoaders() {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(privateTestDir(t), "config.yaml")
			if err := load(path); !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "restore") {
				t.Fatalf("first-run missing-file behavior changed: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 0 {
				t.Fatalf("missing-file load created files: %v, %v", entries, err)
			}
			data := []byte("schema_version: 1\n")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := load(path); err == nil || !strings.Contains(err.Error(), "mode 0600") {
				t.Fatalf("permission refusal changed: %v", err)
			}
			assertConfigBytes(t, path, data)
			link := filepath.Join(filepath.Dir(path), "link.yaml")
			if err := os.Symlink(path, link); err != nil {
				t.Fatal(err)
			}
			if err := load(link); !errors.Is(err, syscall.ELOOP) {
				t.Fatalf("no-follow OS error classification lost: %v", err)
			}
		})
	}
}

func assertConfigBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("configuration evidence changed at %s: %v", filepath.Base(path), err)
	}
}
