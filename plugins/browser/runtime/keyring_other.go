//go:build !linux

package runtime

import "errors"

func RunKeyringService(bool) error {
	return errors.New("administrator browser keyring services require Linux systemd encrypted credentials")
}
