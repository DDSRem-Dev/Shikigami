package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestConfigurationCommandsUseAgentRevision(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-config-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := config.DefaultClient()
	cfg.Agent.StateDir, cfg.Agent.Socket = filepath.Join(dir, "state"), filepath.Join(dir, "run/agent.sock")
	cfg.Agent.AutoStart, cfg.Tailscale.Enabled = false, false
	cfg.PluginsDir = filepath.Join(dir, "plugins")
	path := filepath.Join(dir, "config.yaml")
	if err = config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx, path, "test") }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("agent did not stop")
		}
	})
	var c *client.Client
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		c, err = client.Open(ctx, path, client.Options{Timeout: time.Second})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	before, err := runManagement(t, path, "config", "get")
	if err != nil {
		t.Fatal(err)
	}
	startup, err := runManagement(t, path, "config", "startup")
	if err != nil {
		t.Fatal(err)
	}
	policy := new(apb.StartupPolicy)
	if err := protojson.Unmarshal([]byte(startup), policy); err != nil || policy.CliAutoStart != "disabled" || policy.ConfigPath != path || policy.LoginStart == "enabled" {
		t.Fatalf("startup policy: %s %v", startup, err)
	}
	unchanged, err := runManagement(t, path, "config", "get")
	if err != nil || unchanged != before {
		t.Fatal("startup query changed editable preferences", err)
	}
	if _, err := runManagement(t, path, "config", "startup", "--server", "unrelated"); err == nil {
		t.Fatal("startup query accepted remote scope")
	}
	stale := filepath.Join(dir, "edit.json")
	if err = os.WriteFile(stale, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := runManagement(t, path, "config", "set", "no_color", "true")
	if err != nil {
		t.Fatal(err)
	}
	prefs := new(apb.Preferences)
	if err = protojson.Unmarshal([]byte(changed), prefs); err != nil || !prefs.NoColor {
		t.Fatalf("not persisted: %s %v", changed, err)
	}
	oldPrefs := new(apb.Preferences)
	if err = protojson.Unmarshal([]byte(before), oldPrefs); err != nil {
		t.Fatal(err)
	}
	candidate := &apb.Preferences{Revision: oldPrefs.Revision, NoColor: true, DetachKey: "ctrl-]"}
	frontend := &app{output: &bytes.Buffer{}}
	if err = frontend.saveConfiguration(ctx, c, oldPrefs, candidate); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("concurrent edit bypassed host revision: %v", err)
	}
	if _, err = runManagement(t, path, "config", "edit", "--file", stale); err == nil {
		t.Fatal("stale edit overwrote newer preferences")
	}
	for _, args := range [][]string{{"set", "revision", "invented"}, {"set", "token", "secret"}, {"set", "no_color", "yes"}, {"set", "detach_key", "ctrl-c"}, {"edit"}} {
		if _, err = runManagement(t, path, append([]string{"config"}, args...)...); err == nil {
			t.Fatalf("invalid edit accepted: %v", args)
		}
	}
	prefs.DetachKey = "ctrl-]"
	data, _ := protojson.Marshal(prefs)
	if err = os.WriteFile(stale, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = runManagement(t, path, "config", "edit", "--file", stale); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("frontend edited bootstrap YAML instead of using IPC")
	}
	server, err := c.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{Config: &apb.ServerConfig{Name: "demo", DirectTlsAddress: "127.0.0.1:1", TlsServerName: "localhost"}, RequestId: "server"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runManagement(t, path, "config", "set", "name", "renamed", "--server", server.Config.Id)
	if err != nil || !strings.Contains(result, "renamed") {
		t.Fatalf("server edit failed: %s %v", result, err)
	}
	if _, err = runManagement(t, path, "config", "set", "id", "srv_other", "--server", server.Config.Id); err == nil {
		t.Fatal("server identity edited")
	}
}

func TestConfigurationEditFileAndEditorBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.json")
	for _, data := range []string{`{"revision":"one","revision":"two"}`, `{"token":"secret"}`, `{"no_color":true} {}`} {
		prefs := new(apb.Preferences)
		if err := protojson.Unmarshal([]byte(data), prefs); err == nil {
			t.Fatalf("malformed edit accepted: %s", data)
		}
	}
	if err := os.WriteFile(path, []byte(`{"revision":"old"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigurationEdit(path); err == nil {
		t.Fatal("public edit file accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigurationEdit(link); err == nil {
		t.Fatal("symlink edit accepted")
	}
	fifo := filepath.Join(dir, "fifo") // Opening a special file must not wait for its writer.
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigurationEdit(fifo); err == nil {
		t.Fatal("FIFO accepted")
	}
	editor := filepath.Join(dir, "editor with spaces")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf '%s' '{\"revision\":\"old\",\"no_color\":true}' > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "'"+editor+"'")
	a := &app{output: &bytes.Buffer{}, errorOutput: &bytes.Buffer{}}
	data, err := a.editConfiguration(t.Context(), &apb.Preferences{Revision: "old"})
	if err != nil {
		t.Fatal(err)
	}
	prefs := new(apb.Preferences)
	if err = protojson.Unmarshal(data, prefs); err != nil || !prefs.NoColor {
		t.Fatalf("editor output: %s %v", data, err)
	}
	t.Setenv("VISUAL", "$(touch "+filepath.Join(dir, "injected")+")")
	if _, err = a.editConfiguration(t.Context(), prefs); err == nil {
		t.Fatal("shell substitution accepted")
	}
	if _, err = os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("shell substitution executed")
	}
}
