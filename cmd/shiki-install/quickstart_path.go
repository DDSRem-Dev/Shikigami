package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const quickstartPathBlock = "# >>> Shikigami managed PATH >>>\nexport PATH=\"$HOME/.local/bin:$PATH\"\n# <<< Shikigami managed PATH <<<\n"

func quickstartShellPath(home string) error {
	var names []string
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		names = []string{".zprofile", ".zshrc"}
	case "bash":
		names = []string{".bash_profile"}
		for _, candidate := range []string{".bash_profile", ".bash_login", ".profile"} {
			if _, err := os.Lstat(filepath.Join(home, candidate)); err == nil {
				names = []string{candidate}
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	default:
		return errors.New("unknown shell; add ~/.local/bin to its PATH explicitly")
	}
	var problems []error
	for _, name := range names {
		if err := quickstartAppendPath(filepath.Join(home, name)); err != nil {
			problems = append(problems, fmt.Errorf("%s was preserved: %w", filepath.Join(home, name), err))
		}
	}
	return errors.Join(problems...)
}

func quickstartAppendPath(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	info, err := f.Stat()
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || owner.Uid != uint32(os.Geteuid()) || owner.Nlink != 1 || info.Mode().Perm()&0022 != 0 || info.Size() > 1<<20 {
		return errors.New("shell startup file is not a safe owned regular file; preserve it and add ~/.local/bin to PATH manually")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if bytes.Contains(data, []byte(quickstartPathBlock)) {
		return nil
	}
	if bytes.Contains(data, []byte("Shikigami managed PATH")) {
		return errors.New("existing Shikigami PATH block was edited; preserve it and review manually")
	}
	if _, err = f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	prefix := ""
	if len(data) != 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	if _, err = io.WriteString(f, prefix+quickstartPathBlock); err != nil {
		return err
	}
	return f.Sync()
}
