//go:build darwin

package client

import "golang.org/x/sys/unix"

func consoleTerminalState(fd int) (*unix.Termios, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err == nil {
		// Kernel canonical-input bookkeeping changes when raw mode is restored.
		state.Lflag &^= unix.PENDIN
	}
	return state, err
}
