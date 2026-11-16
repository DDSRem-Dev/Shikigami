//go:build linux

package client

import "golang.org/x/sys/unix"

func consoleTerminalState(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}
