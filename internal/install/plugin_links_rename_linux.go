package install

import "golang.org/x/sys/unix"

func movePluginRoot(parent int, from, to string, exchange bool) error {
	flags := uint(unix.RENAME_NOREPLACE)
	if exchange {
		flags = unix.RENAME_EXCHANGE
	}
	return unix.Renameat2(parent, from, parent, to, flags)
}
