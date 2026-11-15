//go:build linux

package runtime

import (
	"errors"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
func singleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
func verifyPeer(connection net.Conn) error {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("browser runtime requires Unix socket")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return err
	}
	var credential *unix.Ucred
	var failure error
	if err := raw.Control(func(fd uintptr) {
		credential, failure = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if credential == nil || credential.Uid != uint32(os.Getuid()) {
		return errors.New("browser runtime peer UID mismatch")
	}
	return nil
}
