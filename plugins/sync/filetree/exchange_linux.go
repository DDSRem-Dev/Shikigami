//go:build linux

package filetree

import "golang.org/x/sys/unix"

func exchange(fd int, a, b string) error { return unix.Renameat2(fd, a, fd, b, unix.RENAME_EXCHANGE) }
