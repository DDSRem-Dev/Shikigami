package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func managedLink(path, target string) error {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return managedRootLink(parent, filepath.Base(path), target)
}

func managedRootLink(root *os.Root, name, target string) error {
	if existing, err := root.Readlink(name); err == nil {
		if existing == target {
			return nil
		}
		return errors.New("existing link belongs to another installation")
	}
	if _, err := root.Lstat(name); err == nil {
		return errors.New("refusing to replace an existing unmanaged file or plugin directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.Symlink(target, name)
}
func installUserLinks(ctx context.Context, plan Plan, prefix string) (err error) {
	lease, err := preparePluginLinks(ctx, plan, prefix, pluginLinksCurrent)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
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
	return unlinkRootManaged(parent, filepath.Base(path), target)
}

func unlinkRootManaged(root *os.Root, name, target string) error {
	existing, err := root.Readlink(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing != target {
		return errors.New("refusing to remove an unmanaged link")
	}
	return root.Remove(name)
}
func removeUserLinks(ctx context.Context, plan Plan, prefix string) (err error) {
	lease, err := preparePluginLinks(ctx, plan, prefix, pluginLinksRemove)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if plan.Role == "client" {
		for _, name := range []string{"shiki", "shiki-agent", "shiki-install"} {
			if err := unlinkManaged(filepath.Join(plan.BinDir, name), filepath.Join(prefix, "current", name)); err != nil {
				return err
			}
		}
	}
	return nil
}
