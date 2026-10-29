package install

import (
	"errors"
	"os"
	"path/filepath"
)

func managedLink(path, target string) error {
	if existing, err := os.Readlink(path); err == nil {
		if existing == target {
			return nil
		}
		return errors.New("existing link belongs to another installation")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("refusing to replace an existing unmanaged file or plugin directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Symlink(target, filepath.Base(path))
}
func installUserLinks(plan Plan, prefix string) error {
	if err := managedLink(plan.PluginsDir, filepath.Join(prefix, "current")); err != nil {
		return err
	}
	if plan.Role == "client" {
		if err := ownedServiceDirectory(plan.BinDir, os.Getuid()); err != nil {
			return err
		}
		for _, name := range []string{"shiki", "shiki-agent", "shiki-install"} {
			if err := managedLink(filepath.Join(plan.BinDir, name), filepath.Join(prefix, "current", name)); err != nil {
				return err
			}
		}
	}
	return nil
}
func unlinkManaged(path, target string) error {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	existing, err := parent.Readlink(filepath.Base(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing != target {
		return errors.New("refusing to remove an unmanaged link")
	}
	return parent.Remove(filepath.Base(path))
}
func removeUserLinks(plan Plan, prefix string) error {
	if err := unlinkManaged(plan.PluginsDir, filepath.Join(prefix, "current")); err != nil {
		return err
	}
	if plan.Role == "client" {
		for _, name := range []string{"shiki", "shiki-agent", "shiki-install"} {
			if err := unlinkManaged(filepath.Join(plan.BinDir, name), filepath.Join(prefix, "current", name)); err != nil {
				return err
			}
		}
	}
	return nil
}
