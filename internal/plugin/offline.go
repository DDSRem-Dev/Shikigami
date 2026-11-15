package plugin

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
)

// DisableOffline records durable disabled intent after the owning host has
// stopped. Installation helpers must invoke it as the configured ordinary UID;
// the same private store lock/validation used by Supervisor prevents concurrent
// mutation by a live host. Browser cleanup remains pending until a cleanup-only
// worker confirms stop, so reinstall cannot silently revive a retained profile.
// An absent control directory is not initialized by an uninstall operation.
func DisableOffline(controlDir, id string) error {
	if !filepath.IsAbs(controlDir) || filepath.Clean(controlDir) != controlDir || filepath.Base(controlDir) != "plugin-control" || !pluginIDPattern.MatchString(id) {
		return errors.New("invalid offline plugin control request")
	}
	if _, err := os.Lstat(filepath.Dir(controlDir)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	// The daemon holds the resource store's state.lock for its entire life.
	// Take it before plugin-control, in the same order as daemon startup, and
	// refuse immediately if any manually started host still owns the state.
	host, err := storage.Open(filepath.Dir(controlDir), storage.Options{DisableRecovery: true})
	if err != nil {
		return err
	}
	defer host.Close()
	if _, err := os.Lstat(controlDir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	store, err := storage.Open(controlDir, storage.Options{DisableRecovery: true, Validate: validatePluginState})
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Update(func(state *storage.Snapshot) error {
		if _, exists := state.Buckets["plugins"][id]; !exists {
			return nil
		}
		var want desiredPlugin
		if err := state.Get("plugins", id, &want); err != nil {
			return err
		}
		want.Enabled = false
		want.DisablePending = id == "browser"
		want.Revision = "rev_" + rand.Text()
		return state.Put("plugins", id, &want)
	})
}
