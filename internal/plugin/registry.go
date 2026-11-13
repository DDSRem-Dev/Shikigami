package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"sync"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

type registeredArtifact struct {
	manifest *Manifest
	err      error
}

// PluginRegistry is the side-aware view of exactly one configured plugins_dir.
// Hidden transaction/cache directories and protected built-in IDs are never
// interpreted as packages. Invalid packages replace, rather than retain, an old
// valid record so removal or corruption cannot leave a runnable cached entry.
type PluginRegistry struct {
	mu        sync.RWMutex
	dir, side string
	artifacts map[string]registeredArtifact
}

func NewPluginRegistry(dir, side string) *PluginRegistry {
	return &PluginRegistry{dir: dir, side: side, artifacts: map[string]registeredArtifact{}}
}

func (r *PluginRegistry) Reload() error {
	next := make(map[string]registeredArtifact)
	err := validateArtifactRoot(r.dir)
	if err == nil {
		var entries []os.DirEntry
		entries, err = os.ReadDir(r.dir)
		if err == nil {
			for _, entry := range entries {
				id := entry.Name()
				if !pluginIDPattern.MatchString(id) || builtinID(id) {
					continue
				}
				if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
					continue
				}
				// Symlink packages are invalid, never aliases to another
				// installation location.
				manifest, readErr := readThirdPartyArtifact(filepath.Join(r.dir, id), id, r.side)
				record := registeredArtifact{err: readErr}
				if readErr == nil {
					record.manifest = &manifest
				}
				next[id] = record
			}
		}
	}
	r.mu.Lock()
	r.artifacts = next
	r.mu.Unlock()
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (r *PluginRegistry) snapshot() map[string]registeredArtifact {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]registeredArtifact, len(r.artifacts))
	for id, record := range r.artifacts {
		result[id] = record
	}
	return result
}

// List returns detached, validated declarations for this host's side.
func (r *PluginRegistry) List() []*sdk.PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*sdk.PluginManifest, 0, len(r.artifacts))
	for _, record := range r.artifacts {
		if record.manifest != nil {
			result = append(result, clonePluginManifest(record.manifest.PluginManifest))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (r *PluginRegistry) Get(id string) *sdk.PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if record := r.artifacts[id]; record.manifest != nil {
		return clonePluginManifest(record.manifest.PluginManifest)
	}
	return nil
}

func (r *PluginRegistry) CLIContributions() []sdk.UICLICommand {
	var result []sdk.UICLICommand
	for _, manifest := range r.List() {
		result = append(result, manifest.UI.CLICommands...)
	}
	return result
}

func (r *PluginRegistry) FindByAction(actionID string) (pluginID string, cmd *sdk.UICLICommand) {
	for _, manifest := range r.List() {
		for i := range manifest.UI.CLICommands {
			if manifest.UI.CLICommands[i].ActionID == actionID {
				return manifest.ID, &manifest.UI.CLICommands[i]
			}
		}
	}
	return "", nil
}
