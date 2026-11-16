//go:build linux

package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// InspectQuickstartSupport authenticates the supplied source and checks the
// existing protected receipts/files without APT, service changes or unlocking a
// keyring. Its temporary verification copy is removed before returning.
func InspectQuickstartSupport(ctx context.Context, o ExtensionOptions) (QuickstartSupport, error) {
	var result QuickstartSupport
	if err := trustedExtensionKey(o.PublicKey, false); err != nil {
		return result, err
	}
	work, err := os.MkdirTemp(filepath.Dir(o.PublicKey), ".quickstart-verify-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(work)
	bundle, verified, err := verifiedExtensionBundle(ctx, o, work)
	if err != nil {
		return result, err
	}
	receipt, err := readExtensionReceipt()
	if err != nil {
		return result, err
	}
	account, err := user.Lookup(o.User)
	if err != nil {
		return result, err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid == 0 {
		return result, errors.New("ordinary support account required")
	}
	if err = checkBootUnlockNamespace(receipt, uid); err != nil {
		return result, err
	}
	if receipt == nil {
		return result, nil
	}
	if receipt.BootUnlock != nil {
		result.KeyringMode = receipt.BootUnlock.Mode
		if receipt.BootUnlock.CredentialSHA256 != "" {
			if err = trustedInstalledFile(keyringCredentialPath(uid), 0600); err != nil {
				return result, err
			}
			result.ReuseCredential = true
		} else {
			// An interrupted first enrollment can leave an encrypted store. Ask
			// for its original password again; never provision over that store.
			if _, err = os.Lstat(keyringCredentialPath(uid)); err == nil {
				return result, errors.New("encrypted credential has no committed digest; preserve it for administrator review")
			} else if !errors.Is(err, os.ErrNotExist) {
				return result, err
			}
			result.ProvisionKeyring, err = quickstartRetainedKeyring(keyringStore(account, result.KeyringMode), result.KeyringMode, uint32(uid))
			if err != nil {
				return result, err
			}
		}
	}
	current := true
	for _, path := range []string{RegionHelperPath, BrowserRuntimePath} {
		name := filepath.Base(path)
		entry := File{}
		for _, file := range bundle.Manifest.Files {
			if file.Name == name {
				entry = file
				break
			}
		}
		if receipt.SupportFiles[name] != entry || !quickstartSameRelease(receipt.SignedReleases[name], verified) {
			current = false
			continue
		}
		if err = verifyInstalledSupport(path, receipt); err != nil {
			return result, err
		}
	}
	if current && result.ReuseCredential && receipt.BootUnlock.Enabled {
		policy, err := readBootKeyringPolicy(uid)
		if err != nil {
			return result, err
		}
		if policy == nil || policy.Mode != result.KeyringMode {
			return result, errors.New("enrolled keyring policy is missing or differs from its receipt")
		}
		if err = inspectChromeManagedPolicies("", false); err != nil {
			return result, err
		}
		// Require the mandatory owned policy itself, not merely absence of a
		// conflict in other files.
		if err = trustedInstalledFile(chromePolicyPath, 0644); err != nil {
			return result, err
		}
		data, err := readRegular(filepath.Dir(chromePolicyPath), filepath.Base(chromePolicyPath), 64<<10)
		if err != nil || !exactChromeManagedPolicy(data) {
			return result, errors.New("mandatory Chrome policy is unavailable or changed")
		}
		filesCurrent, err := quickstartSupportFiles(account, uid, result.KeyringMode)
		if err != nil {
			return result, err
		}
		if filesCurrent {
			result.Current = true
			return result, nil
		}
	}
	// Reject a support upgrade while Chrome owns the publication lock BEFORE
	// package installation or credential enrollment. Do not stop it implicitly.
	file, err := os.OpenFile(BrowserRuntimeInstallLock, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer file.Close()
	if err = trustedInstalledFile(BrowserRuntimeInstallLock, 0644); err != nil {
		return result, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return result, errors.New("support update requires explicitly stopping remote Chrome first; installed support was preserved")
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return result, nil
}

func quickstartSupportFiles(account *user.User, uid int, mode string) (bool, error) {
	// An otherwise matching receipt from alpha.4 can hide public directories
	// created with umask 077. Keep setup pending until approved repair occurs.
	directories := append(chromePolicyDirectories(), filepath.Dir(BrowserRuntimePath), "/etc/shikigami")
	if current, err := publicSupportDirectoryState(directories); err != nil || !current {
		return false, err
	}
	unit, err := renderKeyringUnlockUnit(account, uid, false, false)
	if err != nil {
		return false, err
	}
	for _, expected := range []struct {
		path string
		mode os.FileMode
		data []byte
	}{
		{"/etc/sudoers.d/shikigami-region-" + strconv.Itoa(uid), 0440, []byte(regionSudoers(uid))},
		{"/etc/systemd/system/" + RegionGuardUnit, 0644, []byte(regionGuardService())},
		{"/etc/systemd/system/" + keyringUnit(uid, false), 0644, []byte(unit)},
	} {
		if err := trustedInstalledFile(expected.path, expected.mode); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		data, err := readRegular(filepath.Dir(expected.path), filepath.Base(expected.path), 64<<10)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(data, expected.data) {
			return false, errors.New("installed system-support service/authorization was edited; preserve it for administrator review")
		}
	}
	if err := trustedInstalledFile(RegionPolicyPath, 0600); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	data, err := readRegular(filepath.Dir(RegionPolicyPath), filepath.Base(RegionPolicyPath), 64<<10)
	if err != nil {
		return false, err
	}
	var policy struct {
		UID int `json:"daemon_uid"`
	}
	if err = json.Unmarshal(data, &policy); err != nil || policy.UID != uid {
		return false, errors.New("installed region policy is invalid or belongs to another user")
	}
	return true, nil
}
