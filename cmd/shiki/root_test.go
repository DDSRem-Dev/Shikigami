package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestCommandsRejectMissingArgumentsWithoutReadingStdin(t *testing.T) {
	for _, args := range [][]string{{}, {"attach"}, {"projects", "create"}, {"sessions", "delete"}, {"servers", "unknown"}, {"projects", "unknown"}, {"sessions", "unknown"}, {"connect", "--token", "shiki_secret"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			input, writer, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			defer input.Close()
			defer writer.Close()
			if _, e = writer.Write([]byte("pipeline must remain unread")); e != nil {
				t.Fatal(e)
			}
			var out, stderr bytes.Buffer
			command := newRoot(input, &out, &stderr)
			command.SetArgs(args)
			if e = command.Execute(); e == nil {
				t.Fatal("invalid invocation succeeded")
			}
			if out.Len() != 0 {
				t.Fatalf("error polluted stdout: %q", out.String())
			}
			writer.Close()
			unread, e := io.ReadAll(input)
			if e != nil || string(unread) != "pipeline must remain unread" {
				t.Fatalf("pipeline consumed: %q %v", unread, e)
			}
		})
	}
}
func TestJSONAndVersionOutput(t *testing.T) {
	var plain [2]string
	for i, args := range [][]string{{"version"}, {"--version"}} {
		var out, stderr bytes.Buffer
		cmd := newRoot(nil, &out, &stderr)
		cmd.SetArgs(args)
		if e := cmd.Execute(); e != nil {
			t.Fatal(e)
		}
		plain[i] = out.String()
	}
	if plain[0] != plain[1] {
		t.Fatalf("inconsistent build metadata: %q", plain)
	}
	var out, stderr bytes.Buffer
	cmd := newRoot(nil, &out, &stderr)
	cmd.SetArgs([]string{"version", "--json"})
	if e := cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	var build map[string]any
	if e := json.Unmarshal(out.Bytes(), &build); e != nil {
		t.Fatal(e)
	}
	if stderr.Len() != 0 || build["version"] == nil {
		t.Fatalf("invalid output: %s %s", out.String(), stderr.String())
	}
	out.Reset()
	a := &app{json: true, output: &out}
	if e := a.print(&pb.Session{Id: "sess_test", ProjectId: "proj_test"}); e != nil {
		t.Fatal(e)
	}
	var result map[string]any
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result["project_id"] != "proj_test" || result["session"] != nil {
		t.Fatalf("unexpected JSON contract: %s", out.String())
	}
}
func TestCredentialBoundaries(t *testing.T) {
	input, writer, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer input.Close()
	defer writer.Close()
	var out, stderr bytes.Buffer
	a := &app{input: input, output: &out, errorOutput: &stderr}
	if _, e = a.credential(context.Background(), &serverFlags{}, true); e == nil {
		t.Fatal("implicit stdin credential read permitted")
	}
	root := t.TempDir()
	file := filepath.Join(root, "token")
	secret := "shiki_sensitive-secret"
	if e = os.WriteFile(file, []byte(secret+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	got, e := a.credential(context.Background(), &serverFlags{tokenFile: file}, true)
	if e != nil || string(got) != secret {
		t.Fatalf("credential not loaded: %v", e)
	}
	clear(got)
	if e = os.Chmod(file, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = a.credential(context.Background(), &serverFlags{tokenFile: file}, true); e == nil {
		t.Fatal("public credential accepted")
	}
	if e = os.Chmod(file, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "link")
	if e = os.Symlink(file, link); e != nil {
		t.Fatal(e)
	}
	if _, e = a.credential(context.Background(), &serverFlags{tokenFile: link}, true); e == nil {
		t.Fatal("symlink credential accepted")
	}
	if strings.Contains(out.String()+stderr.String(), secret) {
		t.Fatal("credential printed")
	}
}

func TestTailnetAddressDefaultsAndExplicitTLSBoundary(t *testing.T) {
	for input, want := range map[string]string{"shiki-vps": "shiki-vps:9527", "100.64.1.2": "100.64.1.2:9527", "shiki-vps:1234": "shiki-vps:1234", "fd7a:115c:a1e0::1": "[fd7a:115c:a1e0::1]:9527", "[fd7a:115c:a1e0::1]:9528": "[fd7a:115c:a1e0::1]:9528"} {
		got, e := normalizeTailnetAddress(input)
		if e != nil || got != want {
			t.Fatalf("%q -> %q %v", input, got, e)
		}
	}
	for _, input := range []string{"https://host:9527", "host:0", "host:65536", ":9527", "host name", "user@host", "host:port", ""} {
		if _, e := normalizeTailnetAddress(input); e == nil {
			t.Fatalf("invalid address accepted: %q", input)
		}
	}
}
