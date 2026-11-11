// Package plugin — PluginManifest reads the static manifest.json that every
// plugin binary must ship alongside itself. The manifest is read by the host
// BEFORE the plugin process starts, so the host can register CLI commands and
// settings sections without launching the plugin.
//
// File location: ~/.shiki/plugins/<id>/manifest.json (local/client side)
//
//	/var/lib/shiki/plugins/<id>/manifest.json (remote/server side)
//
// Schema version "1" is the only supported version. Unknown fields are rejected
// by the strictjson decoder used in internal/plugin/manifest.go.
package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ManifestSchemaVersion is the only accepted schema_version value.
const ManifestSchemaVersion = "1"

// ManifestSource is an optional interface for plugin Implementations that want
// to provide their manifest data to the host at startup. The host checks for
// this interface after GetInfo() and uses the result to populate SettingsRegistry
// and register dynamic CLI commands without reading a file from disk.
//
// Plugins that embed their manifest as Go code (not via //go:embed) SHOULD
// implement this interface. The returned value must not be mutated by the host.
type ManifestSource interface {
	PluginManifest() *PluginManifest
}

// PluginManifest is the full static declaration shipped with every plugin
// binary. The host reads it before launching the process to register UI
// contributions. Fields map 1-to-1 to manifest.json.
type PluginManifest struct {
	// SchemaVersion must be "1".
	SchemaVersion string `json:"schema_version"`
	// ID is the plugin identifier (e.g. "sync", "port", "my-plugin").
	// Must match the containing directory name and the binary name pattern.
	ID string `json:"id"`
	// Version is a semver string matching the host build it was compiled for.
	Version string `json:"version"`
	// DisplayName is a human-readable label shown in settings and status bars.
	DisplayName string `json:"display_name"`
	// Description is a one-line summary shown in plugin lists.
	Description string `json:"description"`
	// Capabilities lists the protocol capabilities this plugin advertises
	// (e.g. "sync.worker.v1", "port.worker.v1"). Must be non-empty.
	Capabilities []string `json:"capabilities"`
	// Sides lists the sides this binary supports: "client", "server", or both.
	Sides []string `json:"sides"`
	// Binary is the filename of the plugin executable adjacent to manifest.json.
	// Must not contain path separators; must start with "shiki-plugin-".
	Binary string `json:"binary"`
	// BinaryFlags are extra flags passed to the binary (e.g. "--side=${side}").
	// The token "${side}" is replaced by the actual side at launch time.
	BinaryFlags []string `json:"binary_flags,omitempty"`
	// ConfigSchema describes all configuration fields this plugin manages.
	// The host renders a settings section from this schema and validates
	// SetConfig calls before forwarding them to the plugin.
	ConfigSchema []ConfigField `json:"config_schema,omitempty"`
	// UI declares the UI surfaces this plugin contributes to.
	UI UIContribution `json:"ui,omitempty"`
}

// validIDRE allows lowercase letters, digits and hyphens, 1-64 chars.
var validIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$|^[a-z]$`)

// ValidateManifest checks structural constraints. It does NOT verify
// cryptographic signatures or binary presence; that is the installer's job.
func ValidateManifest(m *PluginManifest) error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("manifest schema_version %q is not supported (expected %q)", m.SchemaVersion, ManifestSchemaVersion)
	}
	if !validIDRE.MatchString(m.ID) {
		return fmt.Errorf("plugin id %q is invalid: must be lowercase alphanumeric with hyphens", m.ID)
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("manifest version is required")
	}
	if strings.TrimSpace(m.DisplayName) == "" {
		return errors.New("manifest display_name is required")
	}
	if len(m.Capabilities) == 0 {
		return errors.New("manifest capabilities must not be empty")
	}
	if len(m.Sides) == 0 {
		return errors.New("manifest sides must not be empty")
	}
	for _, s := range m.Sides {
		if s != "client" && s != "server" {
			return fmt.Errorf("manifest side %q is invalid: must be client or server", s)
		}
	}
	expectedBinary := "shiki-plugin-" + m.ID
	if m.Binary != expectedBinary {
		return fmt.Errorf("manifest binary must be %q, got %q", expectedBinary, m.Binary)
	}
	if strings.ContainsAny(m.Binary, "/\\") {
		return errors.New("manifest binary must not contain path separators")
	}
	for i, f := range m.ConfigSchema {
		if err := validateConfigField(&f, fmt.Sprintf("config_schema[%d]", i)); err != nil {
			return err
		}
	}
	if err := validateUIContribution(&m.UI); err != nil {
		return err
	}
	return nil
}

func validateConfigField(f *ConfigField, path string) error {
	if strings.TrimSpace(f.Key) == "" {
		return fmt.Errorf("%s: key is required", path)
	}
	switch f.Type {
	case "string", "int", "bool", "float", "array", "object":
	default:
		return fmt.Errorf("%s: type %q is invalid", path, f.Type)
	}
	if f.UIHint != "" {
		switch f.UIHint {
		case "text", "number", "checkbox", "select", "password", "textarea":
		default:
			return fmt.Errorf("%s: ui_hint %q is invalid", path, f.UIHint)
		}
	}
	for i, child := range f.Children {
		if err := validateConfigField(&child, fmt.Sprintf("%s.children[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateUIContribution(ui *UIContribution) error {
	seen := map[string]bool{}
	for _, slot := range ui.StatusSlots {
		if slot.SlotID == "" {
			return errors.New("ui.status_slots: slot_id is required")
		}
		if seen["slot:"+slot.SlotID] {
			return fmt.Errorf("ui.status_slots: duplicate slot_id %q", slot.SlotID)
		}
		seen["slot:"+slot.SlotID] = true
	}
	for _, panel := range ui.DashboardPanels {
		if panel.PanelID == "" {
			return errors.New("ui.dashboard_panels: panel_id is required")
		}
		if seen["panel:"+panel.PanelID] {
			return fmt.Errorf("ui.dashboard_panels: duplicate panel_id %q", panel.PanelID)
		}
		seen["panel:"+panel.PanelID] = true
	}
	if ui.SettingsSection != nil && ui.SettingsSection.SectionID == "" {
		return errors.New("ui.settings_section: section_id is required")
	}
	seenCLI := map[string]bool{}
	for _, cmd := range ui.CLICommands {
		if cmd.Name == "" {
			return errors.New("ui.cli_commands: name is required")
		}
		if cmd.ActionID == "" {
			return fmt.Errorf("ui.cli_commands[%q]: action_id is required", cmd.Name)
		}
		if seenCLI[cmd.Name] {
			return fmt.Errorf("ui.cli_commands: duplicate name %q", cmd.Name)
		}
		seenCLI[cmd.Name] = true
	}
	return nil
}
