package install

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/pluginmarket"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"golang.org/x/sys/unix"
)

const pluginOwnerFile = ".install-owner.json"

type pluginLinkAction int

const (
	pluginLinksCurrent pluginLinkAction = iota
	pluginLinksLegacy
	pluginLinksRemove
)

type pluginLinkOwner struct {
	Format     int    `json:"format"`
	Prefix     string `json:"prefix"`
	PluginsDir string `json:"plugins_dir"`
}

type pluginLinkEntry struct {
	info os.FileInfo
	link bool
}

type pluginLinkLease struct {
	parent    *os.Root
	store     *pluginmarket.InstallStore
	directory *os.Root
}

func (l *pluginLinkLease) Close() error {
	var result error
	if l.directory != nil {
		result = l.directory.Close()
	}
	if l.store != nil {
		result = errors.Join(result, l.store.Close())
	}
	if l.parent != nil {
		result = errors.Join(result, l.parent.Close())
	}
	return result
}

func legacyPluginLinks(version string) bool {
	// These immutable public helpers require plugins_dir -> prefix/current.
	return version == "v0.0.1-alpha.1" || version == "v0.0.1-alpha.2"
}

func pluginLinkPaths(path string) (string, string) {
	base := filepath.Base(path)
	return "." + base + ".shiki-store", "." + base + ".shiki-owner.json"
}

func inspectPluginLink(parent *os.Root, name, target string) (pluginLinkEntry, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return pluginLinkEntry{}, nil
	}
	if err != nil {
		return pluginLinkEntry{}, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return pluginLinkEntry{}, errors.New("plugin layout entry must belong to the configured ordinary user")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := parent.Readlink(name)
		if err != nil {
			return pluginLinkEntry{}, err
		}
		if link != target {
			return pluginLinkEntry{}, errors.New("refusing a plugin link belonging to another installation")
		}
		return pluginLinkEntry{info: info, link: true}, nil
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return pluginLinkEntry{}, errors.New("plugin store must be a real private directory")
	}
	return pluginLinkEntry{info: info}, nil
}

func readPluginLinkOwner(parent *os.Root, name string, expected pluginLinkOwner) (os.FileInfo, error) {
	file, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64<<10 {
		return nil, errors.New("unsafe plugin layout ownership marker")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	var actual pluginLinkOwner
	if len(data) > 64<<10 || strictjson.Decode(data, &actual) != nil || actual != expected {
		return nil, errors.New("plugin directory belongs to another installation or has invalid ownership metadata")
	}
	return info, nil
}

func createPluginLinkOwner(parent *os.Root, name string, owner pluginLinkOwner) (os.FileInfo, error) {
	data, err := json.Marshal(owner)
	if err != nil {
		return nil, err
	}
	temporary := name + ".new-" + rand.Text()
	file, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	defer parent.Remove(temporary)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err = errors.Join(writeErr, file.Close()); err != nil {
		return nil, err
	}
	// Link, unlike Rename, cannot replace another installation's ownership claim.
	if err = parent.Link(temporary, name); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err = syncDir(parent); err != nil {
		return nil, err
	}
	return readPluginLinkOwner(parent, name, owner)
}

func pinPluginDirectory(parent *os.Root, name string, entry pluginLinkEntry, marker os.FileInfo) (*os.Root, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(entry.info, info) {
		root.Close()
		return nil, errors.New("plugin directory changed while pinning it")
	}
	file, err := root.OpenFile(pluginOwnerFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		root.Close()
		return nil, err
	}
	owned, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(marker, owned) {
		root.Close()
		return nil, errors.New("refusing an unmanaged plugin directory")
	}
	return root, nil
}

func initializePluginDirectory(parent *os.Root, name, markerName string, marker os.FileInfo) error {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	if file, err := root.OpenFile(pluginOwnerFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0); err == nil {
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil || closeErr != nil || !os.SameFile(marker, info) {
			return errors.New("refusing an unmanaged parked plugin directory")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The outside ownership claim is durable before this initial directory is
	// created. A crash here may leave only the original, private install lock.
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := file.ReadDir(2)
	file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	for _, entry := range entries {
		if entry.Name() != ".install.lock" {
			return errors.New("refusing an unmarked nonempty parked plugin directory")
		}
	}
	parentFD, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer parentFD.Close()
	directoryFD, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directoryFD.Close()
	if err = unix.Linkat(int(parentFD.Fd()), markerName, int(directoryFD.Fd()), pluginOwnerFile, 0); err != nil {
		return err
	}
	return syncDir(root)
}

func preparePluginLinks(ctx context.Context, plan Plan, prefix string, action pluginLinkAction) (_ *pluginLinkLease, err error) {
	if os.Geteuid() == 0 {
		return nil, errors.New("plugin layout changes must run as the configured ordinary user")
	}
	if !filepath.IsAbs(plan.PluginsDir) || filepath.Clean(plan.PluginsDir) == "/" || !filepath.IsAbs(prefix) || filepath.Clean(prefix) == "/" {
		return nil, errors.New("plugin layout paths must be absolute and non-root")
	}
	plan.PluginsDir = filepath.Clean(plan.PluginsDir)
	prefix = filepath.Clean(prefix)
	parentPath := filepath.Dir(plan.PluginsDir)
	if action == pluginLinksRemove {
		if _, err := os.Lstat(parentPath); errors.Is(err, os.ErrNotExist) {
			return &pluginLinkLease{}, nil
		} else if err != nil {
			return nil, err
		}
	}
	if err = privateOwnedDir(parentPath, os.Geteuid(), os.Getegid()); err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	lease := &pluginLinkLease{parent: parent}
	defer func() {
		if err != nil {
			lease.Close()
		}
	}()
	active := filepath.Base(plan.PluginsDir)
	parked, markerName := pluginLinkPaths(plan.PluginsDir)
	target := filepath.Join(prefix, "current")
	current, err := inspectPluginLink(parent, active, target)
	if err != nil {
		return nil, err
	}
	spare, err := inspectPluginLink(parent, parked, target)
	if err != nil {
		return nil, err
	}
	owner := pluginLinkOwner{Format: 1, Prefix: filepath.Clean(prefix), PluginsDir: filepath.Clean(plan.PluginsDir)}
	marker, err := readPluginLinkOwner(parent, markerName, owner)
	if errors.Is(err, os.ErrNotExist) {
		if current.info != nil && !current.link || spare.info != nil {
			return nil, errors.New("refusing to adopt an existing unmanaged plugin directory")
		}
		if action != pluginLinksCurrent {
			if action == pluginLinksLegacy && current.info == nil {
				err = parent.Symlink(target, active)
			} else if action == pluginLinksRemove && current.info != nil {
				err = parent.Remove(active)
			}
			if err == nil {
				err = syncDir(parent)
			}
			return lease, err
		}
		marker, err = createPluginLinkOwner(parent, markerName, owner)
	}
	if err != nil {
		return nil, err
	}
	if current.info != nil && !current.link && spare.info != nil && !spare.link {
		return nil, errors.New("both live and parked plugin stores exist; refusing to replace either")
	}
	// A parked symlink is the harmless other half of an interrupted exchange.
	// If a live store exists, retain its lock before removing even that shadow.
	removeShadow := spare.link && (current.info == nil || current.link || action != pluginLinksLegacy)
	if removeShadow && (current.info == nil || current.link) {
		if err = parent.Remove(parked); err != nil {
			return nil, err
		}
		spare = pluginLinkEntry{}
	}
	storeName, storeEntry := active, current
	if current.info == nil || current.link {
		storeName, storeEntry = parked, spare
	}
	if storeEntry.info == nil && action == pluginLinksCurrent {
		if err = parent.Mkdir(parked, 0700); err != nil {
			return nil, err
		}
		storeName = parked
		storeEntry, err = inspectPluginLink(parent, parked, target)
		if err != nil {
			return nil, err
		}
	}
	if storeEntry.info != nil && !storeEntry.link {
		if storeName == parked {
			if err = initializePluginDirectory(parent, parked, markerName, marker); err != nil {
				return nil, err
			}
		}
		lease.directory, err = pinPluginDirectory(parent, storeName, storeEntry, marker)
		if err != nil {
			return nil, err
		}
		lease.store, err = pluginmarket.OpenInstallStore(ctx, filepath.Join(parentPath, storeName))
		if err != nil {
			return nil, err
		}
		pinned, statErr := lease.directory.Stat(".")
		now, lookupErr := parent.Lstat(storeName)
		if statErr != nil || lookupErr != nil || !os.SameFile(pinned, now) {
			return nil, errors.New("plugin store changed while acquiring its installation lock")
		}
	}
	if removeShadow && spare.link {
		if err = parent.Remove(parked); err != nil {
			return nil, err
		}
		spare = pluginLinkEntry{}
	}
	parentFD, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer parentFD.Close()
	switch action {
	case pluginLinksCurrent:
		if err = managedRootLink(lease.directory, ".builtin", target); err != nil {
			return nil, err
		}
		if storeName == parked {
			err = movePluginRoot(int(parentFD.Fd()), parked, active, current.link)
			if err == nil && current.link {
				err = parent.Remove(parked)
			}
		}
	case pluginLinksLegacy:
		if current.info != nil && !current.link {
			if spare.info == nil {
				if err = parent.Symlink(target, parked); err != nil {
					return nil, err
				}
			}
			err = movePluginRoot(int(parentFD.Fd()), active, parked, true)
		} else if current.info == nil {
			err = parent.Symlink(target, active)
		}
	case pluginLinksRemove:
		if current.link {
			err = parent.Remove(active)
		} else if current.info != nil {
			err = unlinkRootManaged(lease.directory, ".builtin", target)
		}
	default:
		err = errors.New("unknown plugin layout action")
	}
	if err == nil && lease.directory != nil {
		err = syncDir(lease.directory)
	}
	if err == nil {
		err = syncDir(parent)
	}
	return lease, err
}
