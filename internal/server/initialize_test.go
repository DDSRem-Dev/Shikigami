package server

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

func TestInitializeDoesNotCreateOrMutatePluginArtifacts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("daemon initialization requires an ordinary runtime user")
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "managed-release-link"}[installed], func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultDaemon(account.Username, filepath.Join(root, "state"))
			cfg.ProjectsDir = filepath.Join(root, "projects")
			cfg.PluginsDir = filepath.Join(root, "plugins")
			release := filepath.Join(root, "release")
			if installed {
				if err := os.Mkdir(release, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(release, cfg.PluginsDir); err != nil {
					t.Fatal(err)
				}
			}
			id, err := Initialize(cfg)
			if err != nil || id == "" {
				t.Fatal(id, err)
			}
			if installed {
				link, err := os.Readlink(cfg.PluginsDir)
				if err != nil || link != release {
					t.Fatal("managed artifact link changed", link, err)
				}
				info, err := os.Stat(release)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatal("release permissions changed", err)
				}
			} else if _, err := os.Lstat(cfg.PluginsDir); !os.IsNotExist(err) {
				t.Fatal("initialization created an unmanaged artifact directory", err)
			}
		})
	}
}
