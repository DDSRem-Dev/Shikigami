//go:build darwin

package main

import "golang.org/x/sys/unix"

func onboardingTermios(fd int) (*unix.Termios, error) { return unix.IoctlGetTermios(fd, unix.TIOCGETA) }
