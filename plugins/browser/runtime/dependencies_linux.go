//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"time"
)

func dependencyVersions(parent context.Context) ([]string, error) {
	for _, path := range []string{"/usr/share/fcitx5/inputmethod/pinyin.conf", "/usr/share/fcitx5/inputmethod/mozc.conf"} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, errors.New("server IME Chinese Pinyin/Japanese Mozc engine is not installed")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return nil, errors.New("server IME engine description is not root-owned and immutable")
		}
	}
	if err := verifyExecutable("/usr/bin/dpkg-query"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	data, err := command(ctx, "/usr/bin/dpkg-query", "--show", "--showformat=${binary:Package} ${Version}\n", "tigervnc-standalone-server", "tigervnc-common", "fcitx5", "fcitx5-chinese-addons", "fcitx5-mozc", "fcitx5-frontend-gtk3", "fonts-noto-cjk", "openbox", "gnome-keyring", "libsecret-1-0")
	if err != nil {
		return nil, errors.New("browser runtime dependencies are incomplete; inspect the explicit administrator dependency plan")
	}
	var versions []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if len(line) > 512 {
			return nil, errors.New("dependency version exceeds bound")
		}
		versions = append(versions, "Installed dependency: "+line)
	}
	return versions, nil
}
