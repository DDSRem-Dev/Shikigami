package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// The guided plan explicitly permits only removing group/other write from the
// selected account's home. Do not recurse, change ownership or follow a link.
func quickstartSecureHome(home string, uid uint32) error {
	f, err := os.OpenFile(home, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || owner.Uid != uid {
		return errors.New("runtime home must be a real directory owned by the selected ordinary user; ownership was not changed")
	}
	if info.Mode().Perm()&0022 == 0 {
		return nil
	}
	if err = f.Chmod(info.Mode() &^ 0022); err != nil {
		return fmt.Errorf("secure runtime home %s without changing its contents: %w", home, err)
	}
	return nil
}
