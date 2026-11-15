//go:build linux

package server

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

func lockState(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("browser profile state is already owned")
	}
	return file, nil
}
func linuxBootID() (string, error) {
	value, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	boot := strings.TrimSpace(string(value))
	if len(boot) != 36 {
		return "", errors.New("invalid Linux boot identity")
	}
	return boot, nil
}
