//go:build !linux

package server

import (
	"errors"
	"os"
)

func lockState(string) (*os.File, error) {
	return nil, errors.New("managed browser server requires Linux")
}
func linuxBootID() (string, error) { return "", errors.New("managed browser server requires Linux") }
