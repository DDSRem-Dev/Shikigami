//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func TestOptionalPluginManifestsAdvertiseOnlyRunnablePlatformSides(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	t.Chdir(filepath.Dir(filepath.Dir(source)))
	for _, test := range []struct{ id, platform, side string }{
		{"region", "linux", "server"},
		{"browser", "linux", "both"},
		{"browser", "darwin", "client"},
	} {
		t.Run(test.id+"-"+test.platform, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "shiki-plugin-"+test.id), []byte("opaque artifact used only for hashing"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := run(directory, test.id, "v1.2.3", test.platform); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(directory, test.id+".manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest host.Manifest
			if err = json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Side != test.side {
				t.Fatalf("platform side=%s want %s", manifest.Side, test.side)
			}
			if test.platform == "darwin" {
				capabilities := manifest.Protocol.GetCapabilities()
				if len(capabilities) != 2 || capabilities[0] != sdk.CoreCapability || capabilities[1] != sdk.BrowserViewerCapability {
					t.Fatal("Darwin viewer advertises server/runtime capability", capabilities)
				}
				if len(manifest.PluginManifest.Sides) != 1 || manifest.PluginManifest.Sides[0] != "client" {
					t.Fatal("Darwin declaration is not client-only")
				}
				for _, field := range manifest.PluginManifest.ConfigSchema {
					if !strings.HasPrefix(field.Key, "viewer_") {
						t.Fatal("Darwin advertises an unavailable server setting", field.Key)
					}
				}
			}
		})
	}
	if err := run(t.TempDir(), "region", "v1.2.3", "darwin"); err == nil {
		t.Fatal("Darwin region artifact accepted")
	}
}
