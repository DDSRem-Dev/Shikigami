package client

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

func pluginSessionConfig(t *testing.T) (string, *config.Client) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shiki-plugin-session-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.PluginsDir = filepath.Join(root, "custom-plugins")
	cfg.Agent.AutoStart = true
	cfg.Tailscale.Enabled = false
	path := filepath.Join(root, "config.yaml")
	if err := config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func TestOfflinePluginInstallExcludesAgentStartup(t *testing.T) {
	path, cfg := pluginSessionConfig(t)
	session, err := OpenPluginInstallSession(t.Context(), path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Client != nil || session.Directory != cfg.PluginsDir || session.offline == nil {
		t.Fatalf("offline session unexpectedly started an agent or ignored configuration: %#v", session)
	}
	lock, err := privateFile(filepath.Join(cfg.Agent.StateDir, "instance.lock"), os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("offline installation did not exclude agent startup")
	}
	if _, err := os.Stat(cfg.Agent.Socket); !os.IsNotExist(err) {
		t.Fatalf("offline installation auto-started an agent: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("installer did not release lifetime exclusion: %v", err)
	}
}

func TestPluginInstallRefusesUnavailableLiveAgent(t *testing.T) {
	path, cfg := pluginSessionConfig(t)
	if err := os.MkdirAll(cfg.Agent.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(cfg.Agent.StateDir, "instance.lock")
	lock, err := privateFile(name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.WriteString("12345\n"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if session, err := OpenPluginInstallSession(t.Context(), path, time.Second); err == nil || !strings.Contains(err.Error(), "running or stopping") {
		if session != nil {
			session.Close()
		}
		t.Fatalf("missing IPC incorrectly treated as a safe offline agent: %v", err)
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "12345\n" {
		t.Fatalf("installer changed lifetime lock record: %q %v", data, err)
	}
	if _, err := os.Stat(cfg.PluginsDir); !os.IsNotExist(err) {
		t.Fatalf("unsafe installation began modifying plugin files: %v", err)
	}
}
