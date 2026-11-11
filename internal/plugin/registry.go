package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

// PluginRegistry scans a plugin directory for user-installed plugins
// (third-party and future plugins, NOT the built-in sync/port).
// Built-in plugin manifests are served from plugins/sync/manifest.json
// and plugins/port/manifest.json via embedded assets or install path.
type PluginRegistry struct {
	mu        sync.RWMutex
	dir       string
	manifests map[string]*sdk.PluginManifest
}

// NewPluginRegistry creates a registry backed by dir (e.g. ~/.shiki/plugins).
// The directory is scanned immediately; Reload re-scans.
func NewPluginRegistry(dir string) *PluginRegistry {
	r := &PluginRegistry{dir: dir, manifests: map[string]*sdk.PluginManifest{}}
	if err := r.Reload(); err != nil {
		slog.Error("plugin registry: initial scan failed", "dir", dir, "err", err)
	}
	return r
}

// Reload re-scans the plugin directory and updates the registry.
func (r *PluginRegistry) Reload() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	next := make(map[string]*sdk.PluginManifest)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		m, err := ReadPluginManifest(filepath.Join(r.dir, name))
		if err != nil {
			slog.Error("plugin registry: skipping invalid manifest", "dir", name, "err", err)
			continue
		}
		if m.ID == "sync" || m.ID == "port" {
			// Built-in IDs are managed separately; skip silently.
			continue
		}
		next[m.ID] = m
	}
	r.mu.Lock()
	r.manifests = next
	r.mu.Unlock()
	return nil
}

// List returns all registered plugin manifests.
func (r *PluginRegistry) List() []*sdk.PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*sdk.PluginManifest, 0, len(r.manifests))
	for _, m := range r.manifests {
		out = append(out, m)
	}
	return out
}

// Get returns the manifest for the given plugin ID, or nil.
func (r *PluginRegistry) Get(id string) *sdk.PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.manifests[id]
}

// CLIContributions returns all CLI commands across all registered plugins.
func (r *PluginRegistry) CLIContributions() []sdk.UICLICommand {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []sdk.UICLICommand
	for _, m := range r.manifests {
		out = append(out, m.UI.CLICommands...)
	}
	return out
}

// FindByAction returns (pluginID, command) for the given action_id, or empty.
func (r *PluginRegistry) FindByAction(actionID string) (pluginID string, cmd *sdk.UICLICommand) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.manifests {
		for i := range m.UI.CLICommands {
			if m.UI.CLICommands[i].ActionID == actionID {
				return m.ID, &m.UI.CLICommands[i]
			}
		}
	}
	return "", nil
}
