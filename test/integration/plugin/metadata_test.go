package plugin_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/proto"
)

func TestBuiltinMetadataIsValidatedAndSideAware(t *testing.T) {
	for _, id := range []string{"sync", "port"} {
		t.Run(id, func(t *testing.T) {
			declaration, err := host.ReadPluginManifest(filepath.Join("../../../plugins", id))
			if err != nil {
				t.Fatal(err)
			}
			declaration.Version = "dev"
			offer, err := sdk.BuiltinProtocol(id, "both")
			if err != nil {
				t.Fatal(err)
			}
			manifest := host.Manifest{Name: id, Version: "dev", Side: "both", Executable: "shiki-plugin-" + id, Protocol: offer, PluginManifest: declaration}
			if err := host.ValidateBuiltinMetadata(manifest); err != nil {
				t.Fatal("actual built-in source metadata rejected:", err)
			}
			directory := t.TempDir()
			write := func(manifest host.Manifest) {
				t.Helper()
				data, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, id+".manifest.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(manifest)
			managedRoot := t.TempDir()
			if err := os.Symlink(directory, filepath.Join(managedRoot, ".builtin")); err != nil {
				t.Fatal(err)
			}
			if selected, err := host.ResolveBuiltinManifest(managedRoot, id, "client", "dev"); err != nil || filepath.Dir(selected.Executable) != filepath.Join(managedRoot, ".builtin") {
				t.Fatal("managed built-in directory was not resolved:", selected, err)
			}
			for _, side := range []string{"client", "server"} {
				selected, err := host.ReadInstalledManifest(directory, id, side, "dev")
				if err != nil {
					t.Fatal(err)
				}
				if selected.PluginManifest == nil || !slices.Equal(selected.PluginManifest.Sides, []string{side}) {
					t.Fatal("metadata does not select the actual host side:", selected)
				}
				hasAction := slices.Contains(selected.Protocol.Capabilities, sdk.UIActionCapability)
				if hasAction != (side == "client") || slices.Contains(selected.PluginManifest.Capabilities, sdk.UIActionCapability) != hasAction {
					t.Fatal("built-in action capability does not match its implementation side:", side, selected)
				}
				if side == "server" && (len(selected.PluginManifest.UI.CLICommands) > 0 || len(selected.PluginManifest.ConfigSchema) > 0) {
					t.Fatal("server metadata advertises client-only UI/configuration")
				}
			}
			for _, mutation := range []string{"id", "version", "side", "capability", "schema", "flags"} {
				metadata := *declaration
				switch mutation {
				case "id":
					metadata.ID, metadata.Binary = "other", "shiki-plugin-other"
				case "version":
					metadata.Version = "different"
				case "side":
					metadata.Sides = []string{"client"}
				case "capability":
					metadata.Capabilities = append(slices.Clone(metadata.Capabilities), "not.implemented.v1")
				case "schema":
					metadata.ConfigSchema = []sdk.ConfigField{{Key: "invalid", Type: "int", DefaultJSON: `"not-a-number"`}}
				case "flags":
					metadata.BinaryFlags = []string{"--side=server"}
				}
				invalid := manifest
				invalid.PluginManifest = &metadata
				if err := host.ValidateBuiltinMetadata(invalid); err == nil {
					t.Fatal("mismatched metadata accepted:", mutation)
				}
				write(invalid)
				if _, err := host.ReadInstalledManifest(directory, id, "client", "dev"); err == nil {
					t.Fatal("installed metadata bypassed validation:", mutation)
				}
			}
			// Older release manifests have neither nested metadata nor action
			// capability. They remain loadable, but negotiation cannot invent
			// ui.action.v1 just because the new host knows about it.
			legacy := manifest
			legacy.PluginManifest = nil
			legacy.Protocol = proto.Clone(offer).(*pb.ProtocolOffer)
			legacy.Protocol.Capabilities = slices.DeleteFunc(legacy.Protocol.Capabilities, func(capability string) bool { return capability == sdk.UIActionCapability })
			write(legacy)
			for _, side := range []string{"client", "server"} {
				selected, err := host.ReadInstalledManifest(directory, id, side, "dev")
				if err != nil || selected.PluginManifest != nil {
					t.Fatal("legacy metadata-free manifest was rejected or invented UI metadata:", selected, err)
				}
				agreement, err := version.Negotiate(selected.Protocol, legacy.Protocol, "dev")
				if err != nil || slices.Contains(agreement.GetCapabilities(), sdk.UIActionCapability) {
					t.Fatal("legacy negotiation invented an action implementation:", agreement, err)
				}
			}
		})
	}
}

func TestRegistryAcceptsProtectedManagedRootButNotPackageSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "release")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, "plugins")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	registry := host.NewPluginRegistry(root, "client")
	if err := registry.Reload(); err != nil {
		t.Fatal("protected legacy installed root was rejected:", err)
	}
	if err := os.Symlink(target, filepath.Join(target, "fixture")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Reload(); err != nil {
		t.Fatal(err)
	}
	if registry.Get("fixture") != nil || len(registry.List()) != 0 {
		t.Fatal("a symlink package was admitted")
	}
	if err := os.Chmod(target, 0777); err != nil {
		t.Fatal(err)
	}
	if err := registry.Reload(); err == nil {
		t.Fatal("replaceable configured root was accepted")
	}
	if len(registry.List()) != 0 {
		t.Fatal("unsafe root retained a cached package")
	}
}
