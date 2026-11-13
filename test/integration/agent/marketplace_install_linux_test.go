//go:build linux

package agent_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/pluginmarket"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

// Linux's SSL_CERT_FILE lets real CLI subprocesses authenticate the isolated
// HTTPS marketplace without adding a test transport or bypass to product code.
func TestSignedMarketplaceCLIInstallUpdateRecoveryAndUninstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	root, err := os.MkdirTemp("/tmp", "shiki-market-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	build := func(args ...string) {
		t.Helper()
		command := exec.CommandContext(ctx, "go", append([]string{"build", "-race", "-buildvcs=false", "-mod=readonly"}, args...)...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, output)
		}
	}
	build("-o", bin+string(filepath.Separator), "./cmd/shiki", "./cmd/shiki-agent")
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(root, "node")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Agent.Socket = filepath.Join(root, "run", "agent.sock")
	cfg.PluginsDir = filepath.Join(root, "custom-plugins")
	configuration := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(configuration, cfg); err != nil {
		t.Fatal(err)
	}

	var mu sync.RWMutex
	resources := make(map[string][]byte)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		data, ok := resources[r.URL.Path]
		mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	ca := filepath.Join(root, "market-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "HOME="+root, "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+root,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GORACE=halt_on_error=1 log_path="+filepath.Join(root, "race"))
	cli := func(call context.Context, args ...string) ([]byte, error) {
		command := exec.CommandContext(call, filepath.Join(bin, "shiki"), append([]string{"--config", configuration, "--timeout", "45s"}, args...)...)
		command.Env = environment
		return command.CombinedOutput()
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := cli(ctx, args...)
		if err != nil {
			t.Fatalf("shiki %v: %v\n%s", args, err, out)
		}
		return out
	}
	reject := func(args ...string) {
		t.Helper()
		if out, err := cli(ctx, args...); err == nil {
			t.Fatalf("unsafe command succeeded: %v\n%s", args, out)
		}
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		_, _ = cli(cleanup, "agent", "stop")
		reports, err := filepath.Glob(filepath.Join(root, "race.*"))
		if err != nil || len(reports) != 0 {
			t.Errorf("subprocess race reports: %v, %v", reports, err)
		}
	}()

	newKey := func(id string) ed25519.PrivateKey {
		t.Helper()
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, id+".pem")
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		run("plugins", "trust", "add", id, path)
		return private
	}
	publisher := newKey("publisher")
	other := newKey("other")
	sign := func(key ed25519.PrivateKey, keyID string, value any) []byte {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		message := append([]byte("Shikigami plugin distribution\x00format=1\x00"), payload...)
		data, err := json.Marshal(pluginmarket.SignedEnvelope{Format: 1, KeyID: keyID, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	makeArchive := func(version, binaryVersion string, requiredKeys ...string) (pluginmarket.Entry, []byte) {
		t.Helper()
		binary := filepath.Join(root, "fixture-"+binaryVersion)
		if _, err := os.Stat(binary); os.IsNotExist(err) {
			build("-ldflags", "-X main.buildVersion="+binaryVersion, "-o", binary, "./test/fixtures/thirdparty")
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		schema := make([]map[string]any, 0, len(requiredKeys))
		for _, key := range requiredKeys {
			schema = append(schema, map[string]any{"key": key, "type": "string", "required": true})
		}
		manifest, err := json.Marshal(map[string]any{
			"schema_version": "1", "id": "fixture", "version": version, "display_name": "Fixture", "description": "Signed process acceptance",
			"capabilities": []string{"core.scope.v1", "ui.action.v1"}, "sides": []string{"client", "server"},
			"binary": "shiki-plugin-fixture", "binary_flags": []string{"--fixture-token=installed"},
			"config_schema": schema,
			"ui":            map[string]any{"cli_commands": []any{map[string]any{"name": "show", "description": "Echo fixture values", "action_id": "echo", "flags": []any{map[string]any{"name": "json", "type": "bool", "description": "JSON output", "default": "false"}, map[string]any{"name": "value", "type": "string", "description": "Echo value", "default": ""}}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		for _, member := range []struct {
			name string
			data []byte
			mode int64
		}{{"manifest.json", manifest, 0600}, {"shiki-plugin-fixture", data, 0700}} {
			if err := tw.WriteHeader(&tar.Header{Name: member.name, Mode: member.mode, Size: int64(len(member.data)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(member.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("fixture-%s-%s-%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
		digest := fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
		entry := pluginmarket.Entry{ID: "fixture", Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, URL: server.URL + "/" + name, SHA256: digest, SignatureURL: server.URL + "/" + name + ".sig.json", KeyID: "publisher"}
		metadata := pluginmarket.ArchiveMetadata{ID: entry.ID, Version: version, OS: entry.OS, Arch: entry.Arch, Archive: name, ArchiveSHA256: digest, ArchiveSize: int64(archive.Len())}
		mu.Lock()
		resources["/"+name] = archive.Bytes()
		resources["/"+name+".sig.json"] = sign(publisher, "publisher", metadata)
		mu.Unlock()
		return entry, manifest
	}
	old, _ := makeArchive("1.2.3-beta.9", "1.2.3-beta.9")
	current, currentManifest := makeArchive("1.2.3-beta.10", "1.2.3-beta.10")
	// Authenticated metadata and manifest agree, but the executable advertises
	// another version. Activation must fail and restore the old working package.
	broken, _ := makeArchive("1.2.3-beta.11", "1.2.3-beta.10")
	schemaRejected, _ := makeArchive("1.2.3-beta.12", "1.2.3-beta.10", "new_required_setting")
	publish := func(key ed25519.PrivateKey, keyID string, entries ...pluginmarket.Entry) {
		t.Helper()
		data := sign(key, keyID, pluginmarket.Index{Format: 1, Publisher: "isolated acceptance", GeneratedAt: "2026-11-13T00:00:00Z", Entries: entries})
		mu.Lock()
		resources["/index.json"] = data
		mu.Unlock()
	}
	publish(publisher, "publisher", old)
	run("plugins", "market", "add", server.URL+"/index.json")
	// First install is allowed without starting an agent; nothing executes yet.
	run("plugins", "install", "fixture")
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "fixture", "manifest.json")); err != nil {
		t.Fatal("configured plugins_dir ignored:", err)
	}
	run("agent", "start")
	reject("plugins", "fixture", "show", "--json")
	run("plugins", "enable", "fixture", "--side", "local")
	reject("plugins", "fixture", "show", "--server", "must-not-run-locally")
	reject("plugins", "action", "invoke", "fixture", "echo", "--side", "remote")
	invoke := func(want string) float64 {
		t.Helper()
		out := run("plugins", "fixture", "show", "--json", "--flag", "value=roundtrip")
		var result struct {
			Kind string `json:"kind"`
			JSON struct {
				Version string            `json:"version"`
				Side    string            `json:"side"`
				PID     float64           `json:"pid"`
				Flags   map[string]string `json:"flags"`
			} `json:"json"`
		}
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatalf("action JSON: %v\n%s", err, out)
		}
		if result.Kind != "json" || result.JSON.Version != want || result.JSON.Side != "client" || result.JSON.PID <= 0 || result.JSON.Flags["value"] != "roundtrip" || result.JSON.Flags["json"] != "true" {
			t.Fatalf("wrong live action response: %s", out)
		}
		return result.JSON.PID
	}
	oldPID := invoke(old.Version)
	publish(publisher, "publisher", old, current)
	run("plugins", "update", "fixture")
	if pid := invoke(current.Version); pid == oldPID {
		t.Fatal("update reused old executable process")
	}
	installed, err := os.ReadFile(filepath.Join(cfg.PluginsDir, "fixture", "manifest.json"))
	if err != nil || !bytes.Equal(installed, currentManifest) {
		t.Fatal("installer changed the publisher manifest:", err)
	}
	publish(publisher, "publisher", broken)
	reject("plugins", "update", "fixture")
	invoke(current.Version)
	installed, err = os.ReadFile(filepath.Join(cfg.PluginsDir, "fixture", "manifest.json"))
	if err != nil || !bytes.Equal(installed, currentManifest) {
		t.Fatal("failed activation did not restore old package:", err)
	}
	// This enable is rejected by the host before a new desired revision exists,
	// unlike the executable/manifest mismatch above which fails after acceptance.
	publish(publisher, "publisher", schemaRejected)
	reject("plugins", "update", "fixture")
	invoke(current.Version)
	installed, err = os.ReadFile(filepath.Join(cfg.PluginsDir, "fixture", "manifest.json"))
	if err != nil || !bytes.Equal(installed, currentManifest) {
		t.Fatal("pre-acceptance rejection did not restore old package:", err)
	}
	publish(publisher, "publisher", old)
	reject("plugins", "update", "fixture", "--version", old.Version)
	invoke(current.Version)
	changedPublisher := broken
	changedPublisher.KeyID = "other"
	publish(other, "other", changedPublisher)
	reject("plugins", "update", "fixture")
	invoke(current.Version)
	// Enabled intent and verified artifacts must survive an actual host restart.
	run("agent", "stop")
	run("agent", "start")
	invoke(current.Version)
	run("plugins", "uninstall", "fixture", "--yes")
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "fixture")); !os.IsNotExist(err) {
		t.Fatal("uninstall left plugin package:", err)
	}
	reject("plugins", "fixture", "show", "--json")
	run("agent", "stop")
	run("agent", "start")
	reject("plugins", "fixture", "show", "--json")
}
