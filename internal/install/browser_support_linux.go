//go:build linux

package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
)

// Preserve this empty inode across all installations and uninstallations. Managed
// startup and the independent runtime hold shared locks; publishing needs an
// exclusive lock and may not race an already queued launch specification.
func lockBrowserRuntimeInstallation() (func(), error) {
	file, err := openBrowserRuntimeInstallationLock()
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("browser runtime publication is busy; explicitly stop managed Chrome before changing the runtime: %w", err)
	}
	return func() { _ = file.Close() }, nil
}

func openBrowserRuntimeInstallationLock() (*os.File, error) {
	if err := publicSystemDirectory(filepath.Dir(BrowserRuntimeInstallLock)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(BrowserRuntimeInstallLock, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil {
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || owner.Uid != 0 || info.Size() != 0 || info.Mode().Perm()&0022 != 0 {
			err = errors.New("unsafe fixed runtime installation lock")
		}
	}
	if err == nil {
		err = file.Chown(0, 0)
	}
	if err == nil {
		err = file.Chmod(0644)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func installBrowserRuntimeSupport(ctx context.Context, bundle *Bundle, plan *ExtensionPlan, verified releaseauth.Verified) error {
	// An identical executable is not renamed: preserve the running process's
	// installed inode and launch identity without disturbing Chrome.
	for _, entry := range bundle.Manifest.Files {
		if entry.Name == "shiki-browser-runtime" && verifyFile(filepath.Dir(BrowserRuntimePath), entry) == nil {
			lock, err := openBrowserRuntimeInstallationLock()
			if err != nil {
				return err
			}
			if err = lock.Close(); err != nil {
				return err
			}
			if err := copyExtensionBinary(bundle, entry.Name, BrowserRuntimePath); err != nil {
				return err
			}
			return recordExtensionReceipt(plan, bundle, verified, ExtensionOptions{Component: "browser"})
		}
	}
	unlock, err := lockBrowserRuntimeInstallation()
	if err != nil {
		return err
	}
	defer unlock()
	account, err := user.Lookup(plan.User)
	if err != nil {
		return err
	}
	ordinary := Plan{Role: "daemon", User: plan.User, Home: account.HomeDir, Path: "/usr/bin:/bin"}
	running, err := installedBrowserUnits(ctx, ordinary, false)
	if err != nil {
		return err
	}
	pending, err := pendingBrowserUnitJobs(ctx, ordinary)
	if err != nil {
		return err
	}
	if running || pending {
		return errors.New("changed runtime payload requires explicit StopBrowser and no pending managed unit jobs; no running Chrome was restarted or replaced")
	}
	if err = copyExtensionBinary(bundle, "shiki-browser-runtime", BrowserRuntimePath); err != nil {
		return err
	}
	return recordExtensionReceipt(plan, bundle, verified, ExtensionOptions{Component: "browser"})
}

func pendingBrowserUnitJobs(ctx context.Context, plan Plan) (bool, error) {
	_, uid, _, err := accountFor(plan)
	if err != nil {
		return false, err
	}
	if _, err = os.Stat(fmt.Sprintf("/run/user/%d/bus", uid)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := command(ctx, plan, true, "/usr/bin/systemctl", "--user", "list-jobs", "--no-legend", "--no-pager", "--plain")
	captured := &cappedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = captured, io.Discard
	if err = cmd.Run(); err != nil {
		return false, fmt.Errorf("cannot verify pending browser start jobs: %w", err)
	}
	data, cut := captured.Bytes()
	if cut {
		return false, errors.New("systemd user job listing exceeds bounded verification")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "No jobs running." {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return false, errors.New("unrecognized systemd user job listing")
		}
		if browserUnitName.MatchString(fields[1]) {
			return true, nil
		}
	}
	return false, nil
}

func stageRuntimeInstallLock(root *os.Root) error {
	file, err := root.OpenFile(strings.TrimPrefix(BrowserRuntimeInstallLock, "/"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err == nil {
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || owner.Uid != uint32(os.Geteuid()) || info.Size() != 0 || info.Mode().Perm()&0022 != 0 {
			err = errors.New("unsafe inert runtime installation lock")
		}
	}
	if err == nil {
		err = file.Chmod(0644)
	}
	return errors.Join(err, file.Close())
}
