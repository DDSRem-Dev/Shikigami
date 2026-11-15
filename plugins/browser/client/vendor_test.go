package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedNoVNCImmutableSourcesAndLicenses(t *testing.T) {
	encoded, err := webFiles.ReadFile("web/novnc/UPSTREAM.json")
	if err != nil {
		t.Fatal(err)
	}
	var upstream struct {
		Version       string            `json:"version"`
		Commit        string            `json:"commit"`
		ArchiveSHA256 string            `json:"archive_sha256"`
		Files         map[string]string `json:"files"`
	}
	if err := json.Unmarshal(encoded, &upstream); err != nil {
		t.Fatal(err)
	}
	if upstream.Version != "1.6.0" || upstream.Commit != "a8dfd6a3ea3c74244f5ebdaa5a7f1023007a7820" || upstream.ArchiveSHA256 != "ebb8eabcd3670ce1a02e4025f03976826c289121cf631183647bd8325b003403" {
		t.Fatal("noVNC provenance changed without a reviewed pin update")
	}
	for _, required := range []string{"core/rfb.js", "core/websock.js", "vendor/pako/lib/zlib/inflate.js", "LICENSE.txt", "AUTHORS", "docs/LICENSE.MPL-2.0", "docs/LICENSE.BSD-2-Clause", "docs/LICENSE.BSD-3-Clause", "vendor/pako/LICENSE"} {
		if upstream.Files[required] == "" {
			t.Fatalf("missing vendored source/license: %s", required)
		}
	}
	imports := regexp.MustCompile(`(?m)^\s*import\s+.*?from\s+["']([^"']+)["']`)
	for name, expected := range upstream.Files {
		data, err := webFiles.ReadFile("web/novnc/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expected {
			t.Errorf("upstream source was modified: %s", name)
		}
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		for _, dependency := range imports.FindAllSubmatch(data, -1) {
			module := string(dependency[1])
			if !strings.HasPrefix(module, "./") && !strings.HasPrefix(module, "../") {
				t.Errorf("nonlocal module %q in %s", module, name)
				continue
			}
			target := path.Clean(path.Join(path.Dir(name), module))
			if upstream.Files[target] == "" {
				t.Errorf("missing local module %q imported by %s", target, name)
			}
		}
	}
}
