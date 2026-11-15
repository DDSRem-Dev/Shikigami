//go:build linux

package runtime

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const ChromePolicy = "/etc/opt/chrome/policies/managed/shikigami-browser.json"

func requireChromePolicy() error {
	for current := ChromePolicy; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("mandatory Chrome password/signin/sync policy is missing; run the explicit administrator browser setup")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || (current == ChromePolicy && (!info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() > MaxControlBody)) {
			return errors.New("mandatory Chrome policy must be a root-owned immutable ordinary file under root-owned directories")
		}
		if current == "/" {
			break
		}
	}
	file, err := os.Open(ChromePolicy)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxControlBody+1))
	if err != nil {
		return err
	}
	if len(data) > MaxControlBody {
		return errors.New("Chrome policy exceeds bound")
	}
	if err := validateChromePolicy(data); err != nil {
		return err
	}
	return verifyManagedPolicyDirectory(filepath.Dir(ChromePolicy))
}
func validateChromePolicy(data []byte) error {
	var policy struct {
		Passwords *bool `json:"PasswordManagerEnabled"`
		Sync      *bool `json:"SyncDisabled"`
		Signin    *int  `json:"BrowserSignin"`
	}
	if json.Unmarshal(data, &policy) != nil || policy.Passwords == nil || *policy.Passwords || policy.Sync == nil || !*policy.Sync || policy.Signin == nil || *policy.Signin != 0 {
		return errors.New("Chrome requires mandatory PasswordManagerEnabled=false, SyncDisabled=true and BrowserSignin=0")
	}
	return nil
}

func verifyManagedPolicyDirectory(directory string) error {
	folder, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer folder.Close()
	entries, err := folder.ReadDir(65)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 64 {
		return errors.New("managed Chrome policy verification exceeds 64 files")
	}
	var total int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		before, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := before.Sys().(*syscall.Stat_t)
		if !ok || !before.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || before.Mode().Perm()&0022 != 0 || before.Size() > 1<<20 {
			return errors.New("cannot verify an untrusted or oversized managed Chrome policy")
		}
		total += before.Size()
		if total > 8<<20 {
			return errors.New("managed Chrome policy total exceeds verification bound")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		opened, err := file.Stat()
		if err != nil || !os.SameFile(before, opened) {
			file.Close()
			return errors.New("managed Chrome policy changed during verification")
		}
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		file.Close()
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return errors.New("managed Chrome policy grew beyond verification bound")
		}
		if err := validatePolicyConflicts(data); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyConflicts(data []byte) error {
	var values map[string]json.RawMessage
	if json.Unmarshal(data, &values) != nil {
		return errors.New("managed Chrome policy JSON cannot be verified")
	}
	for key, expected := range map[string]string{"PasswordManagerEnabled": "false", "SyncDisabled": "true", "BrowserSignin": "0", "CloudPolicyOverridesPlatformPolicy": "false"} {
		if value, present := values[key]; present && strings.TrimSpace(string(value)) != expected {
			return errors.New("managed Chrome policy conflicts with required password/signin/sync enforcement: " + key)
		}
	}
	return nil
}
