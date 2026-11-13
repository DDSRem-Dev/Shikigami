package install

import "golang.org/x/sys/unix"

func movePluginRoot(parent int, from, to string, exchange bool) error {
	flags := uint32(unix.RENAME_EXCL)
	if exchange {
		flags = unix.RENAME_SWAP
	}
	return unix.RenameatxNp(parent, from, parent, to, flags)
}
