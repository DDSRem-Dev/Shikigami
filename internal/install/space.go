package install

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// This is bounded metadata headroom, not a reservation or a promise that
// concurrent writers cannot consume space after the observation.
const installMetadataBytes uint64 = 64 << 10

type spaceNeed struct {
	path     string
	bytes    uint64
	root     *os.Root
	relative string
}
type filesystemSpace struct {
	device           uint64
	available, block uint64
}

func addSpace(a, b uint64) (uint64, error) {
	if b > math.MaxUint64-a {
		return 0, errors.New("installation space requirement overflow")
	}
	return a + b, nil
}
func multiplySpace(a, b uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, errors.New("installation filesystem size overflow")
	}
	return a * b, nil
}

// Observe the nearest existing directory of the actual destination, not HOME
// or the source bundle filesystem. Missing descendants inherit this filesystem.
// Existing non-directories and final symlinks are rejected, not climbed past.
func availableSpace(path string) (filesystemSpace, error) {
	if !filepath.IsAbs(path) {
		return filesystemSpace{}, errors.New("space destination must be absolute")
	}
	for {
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if errors.Is(err, os.ErrNotExist) && filepath.Dir(path) != path {
			path = filepath.Dir(path)
			continue
		}
		if err != nil {
			return filesystemSpace{}, fmt.Errorf("inspect installation filesystem: %w", err)
		}
		defer file.Close()
		return spaceFromFile(file)
	}
}

func spaceFromFile(file *os.File) (filesystemSpace, error) {
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &fs); err != nil {
		return filesystemSpace{}, fmt.Errorf("query installation free space: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return filesystemSpace{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || fs.Bsize <= 0 {
		return filesystemSpace{}, errors.New("invalid installation filesystem statistics")
	}
	free, err := multiplySpace(uint64(fs.Bavail), uint64(fs.Bsize))
	if err != nil {
		return filesystemSpace{}, err
	}
	return filesystemSpace{device: uint64(stat.Dev), available: free, block: uint64(fs.Bsize)}, nil
}

func (need spaceNeed) observe() (filesystemSpace, error) {
	if need.root == nil {
		return availableSpace(need.path)
	}
	relative := need.relative
	for {
		file, err := need.root.OpenFile(relative, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if errors.Is(err, os.ErrNotExist) && relative != "." {
			relative = filepath.Dir(relative)
			continue
		}
		if err != nil {
			return filesystemSpace{}, fmt.Errorf("inspect pinned installation filesystem: %w", err)
		}
		defer file.Close()
		return spaceFromFile(file)
	}
}

func checkSpace(ctx context.Context, needs []spaceNeed) error {
	type total struct {
		path              string
		needed, available uint64
	}
	totals := map[uint64]total{}
	for _, need := range needs {
		if err := ctx.Err(); err != nil {
			return err
		}
		fs, err := need.observe()
		if err != nil {
			return err
		}
		required, err := addSpace(need.bytes, fs.block-1)
		if err != nil {
			return err
		}
		required = required / fs.block * fs.block
		sum, ok := totals[fs.device]
		if !ok {
			sum = total{path: need.path, needed: installMetadataBytes, available: fs.available}
		}
		sum.available = min(sum.available, fs.available)
		if sum.needed, err = addSpace(sum.needed, required); err != nil {
			return err
		}
		totals[fs.device] = sum
	}
	for _, sum := range totals {
		if sum.needed > sum.available {
			return fmt.Errorf("insufficient installation space at %s: need at least %d bytes, available to ordinary users %d bytes: %w", sum.path, sum.needed, sum.available, syscall.ENOSPC)
		}
	}
	return ctx.Err()
}

func (a *Artifacts) stageNeeds(bundle *Bundle) ([]spaceNeed, error) {
	destination := releaseName(bundle.Manifest)
	if _, err := a.root.Lstat(destination); err == nil {
		_, err = Verify(filepath.Join(a.Prefix, destination), bundle.Manifest.Version, bundle.TrustedChecksums)
		return nil, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	needs := []spaceNeed{{path: a.Prefix, bytes: 128 << 10, root: a.root, relative: "."}}
	for _, entry := range bundle.Manifest.Files {
		if entry.Size < 0 {
			return nil, errors.New("invalid release payload size")
		}
		needs = append(needs, spaceNeed{path: a.Prefix, bytes: uint64(entry.Size), root: a.root, relative: "."})
	}
	return needs, nil
}

type installPreflight interface {
	Preflight(context.Context, Plan, *Artifacts, []spaceNeed) error
}

func (n *Native) Preflight(ctx context.Context, plan Plan, artifacts *Artifacts, stage []spaceNeed) error {
	prefix := artifacts.Prefix
	if err := validateBackupDestination(plan, prefix); err != nil {
		return err
	}
	// The live estimate is discarded. Normal shutdown can flush or add state;
	// Backup takes a new locked inventory and rechecks before writing its tar.
	inventory, err := n.collectBackup(ctx, plan, false)
	if err != nil {
		return err
	}
	defer inventory.Close()
	needs := append([]spaceNeed(nil), stage...)
	needs = append(needs, spaceNeed{path: filepath.Join(prefix, "backups"), bytes: inventory.bytes, root: artifacts.root, relative: "backups"})
	for _, path := range []string{filepath.Dir(plan.Config), plan.StateDir, plan.TailscaleDir, filepath.Dir(plan.PluginsDir), plan.BinDir, filepath.Dir(plan.ServiceFile)} {
		if path != "" && path != "." {
			needs = append(needs, spaceNeed{path: path, bytes: installMetadataBytes})
		}
	}
	return checkSpace(ctx, needs)
}
