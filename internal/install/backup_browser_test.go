package install

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPrivateBrowserStateIsPreservedOutsideHostBackupAndSpaceEstimate(t *testing.T) {
	for _, role := range []string{"daemon", "client"} {
		t.Run(role, func(t *testing.T) {
			state := t.TempDir()
			business := "plugin-data"
			if role == "client" {
				business = "plugin-state"
			}
			browser := filepath.Join(state, business, "browser")
			if err := os.MkdirAll(filepath.Join(browser, "profiles", "account"), 0700); err != nil {
				t.Fatal(err)
			}
			cookie := filepath.Join(browser, "profiles", "account", "Cookies")
			if err := os.WriteFile(cookie, []byte("private login evidence must never enter host snapshot"), 0600); err != nil {
				t.Fatal(err)
			}
			large, err := os.OpenFile(filepath.Join(browser, "profiles", "account", "large-profile"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err = large.Truncate(maxBackupBytes + 1); err != nil {
				large.Close()
				t.Fatal(err)
			}
			large.Close()
			lock, err := os.OpenFile(filepath.Join(browser, "browser.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Join(state, business, "weather"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(state, business, "weather", "state.json"), []byte("retain ordinary plugin backup"), 0600); err != nil {
				t.Fatal(err)
			}
			plan := Plan{Role: role, StateDir: state, Config: filepath.Join(t.TempDir(), "absent-config")}
			for _, lockWriters := range []bool{false, true} {
				inventory, err := (&Native{}).collectBackup(t.Context(), plan, lockWriters)
				if err != nil {
					t.Fatal("running browser or private profile affected host backup", err)
				}
				ordinary := false
				for _, entry := range inventory.entries {
					if strings.Contains(entry.header.Name, "/browser") {
						inventory.Close()
						t.Fatal("browser account data entered host snapshot", entry.header.Name)
					}
					if strings.HasSuffix(entry.header.Name, "weather/state.json") {
						ordinary = true
					}
				}
				inventory.Close()
				if !ordinary {
					t.Fatal("unrelated plugin state was excluded")
				}
			}
			if data, err := os.ReadFile(cookie); err != nil || !strings.Contains(string(data), "private login evidence") {
				t.Fatal("profile was changed or removed", err)
			}
		})
	}
}

func TestBroadStateBackupNeverCapturesUserKeyringStores(t *testing.T) {
	home := t.TempDir()
	for _, relative := range []string{".local/share/keyrings/login.keyring", ".local/share/shikigami/browser-keyring/keyrings/login.keyring"} {
		path := filepath.Join(home, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("encrypted keyring fixture must remain in place"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "ordinary-state"), []byte("normal backup remains"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory, err := (&Native{}).collectBackup(t.Context(), Plan{Role: "daemon", Home: home, StateDir: home, Config: filepath.Join(t.TempDir(), "absent")}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer inventory.Close()
	ordinary := false
	for _, entry := range inventory.entries {
		if strings.Contains(entry.header.Name, "/keyrings") || strings.Contains(entry.header.Name, "/browser-keyring") {
			t.Fatal("ordinary host snapshot contains a keyring store", entry.header.Name)
		}
		if strings.HasSuffix(entry.header.Name, "ordinary-state") {
			ordinary = true
		}
	}
	if !ordinary {
		t.Fatal("unrelated state was excluded with keyrings")
	}
}
