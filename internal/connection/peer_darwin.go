package connection

import (
	"errors"
	"golang.org/x/sys/unix"
	"net"
)

func socketPeerUID(conn net.Conn) (uint32, error) {
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a Unix connection")
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var socketErr error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		socketErr = e
		if e == nil {
			uid = cred.Uid
		}
	})
	if err != nil {
		return 0, err
	}
	return uid, socketErr
}
