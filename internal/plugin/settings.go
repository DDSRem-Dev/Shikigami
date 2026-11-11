package plugin

import (
	"sort"
	"sync"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

// SettingsRegistry tracks the config schema and UI contribution for each
// active plugin instance. The host registers entries at Init time.
type SettingsRegistry struct {
	mu      sync.RWMutex
	entries map[string]*PluginSettings
}

// PluginSettings is the per-plugin record in the SettingsRegistry.
type PluginSettings struct {
	PluginID     string
	DisplayName  string
	ConfigSchema []sdk.ConfigField
	UI           sdk.UIContribution
	// Manifest is non-nil if the plugin has a manifest.json (user/builtin new-style).
	Manifest *sdk.PluginManifest
}

// NewSettingsRegistry creates an empty SettingsRegistry.
func NewSettingsRegistry() *SettingsRegistry {
	return &SettingsRegistry{entries: map[string]*PluginSettings{}}
}

// Register adds or replaces the settings record for the plugin.
func (r *SettingsRegistry) Register(settings *PluginSettings) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[settings.PluginID] = settings
}

// Unregister removes the settings record for the given plugin ID.
func (r *SettingsRegistry) Unregister(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, pluginID)
}

// Get returns the settings record for the given plugin ID, or nil.
func (r *SettingsRegistry) Get(pluginID string) *PluginSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[pluginID]
}

// List returns all registered plugin settings records.
func (r *SettingsRegistry) List() []*PluginSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*PluginSettings, 0, len(r.entries))
	for _, s := range r.entries {
		out = append(out, s)
	}
	return out
}

// CLICommands returns all CLI commands across registered plugins.
func (r *SettingsRegistry) CLICommands() []sdk.UICLICommand {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []sdk.UICLICommand
	for _, s := range r.entries {
		out = append(out, s.UI.CLICommands...)
	}
	return out
}

// StatusSlots returns all status slots across registered plugins.
func (r *SettingsRegistry) StatusSlots() []sdk.UIStatusSlot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []sdk.UIStatusSlot
	for _, s := range r.entries {
		out = append(out, s.UI.StatusSlots...)
	}
	return out
}

// DashboardPanels returns all dashboard panels ordered by Priority.
func (r *SettingsRegistry) DashboardPanels() []sdk.UIDashboardPanel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []sdk.UIDashboardPanel
	for _, s := range r.entries {
		out = append(out, s.UI.DashboardPanels...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}
