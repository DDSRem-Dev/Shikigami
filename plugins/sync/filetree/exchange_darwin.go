//go:build darwin

package filetree

import "golang.org/x/sys/unix"

func exchange(fd int, a, b string) error { return unix.RenameatxNp(fd, a, fd, b, unix.RENAME_SWAP) }
