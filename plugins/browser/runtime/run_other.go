//go:build !linux

package runtime

import "errors"

func Run(string) error {
	return errors.New("managed browser runtime requires Linux systemd user services")
}
func verifyExecutable(string) error { return errors.New("managed browser runtime requires Linux") }
