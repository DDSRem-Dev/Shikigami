//go:build !linux

package runtime

import (
	"errors"
	"os"
)

func acquireInstallLease() (*os.File, error) {
	return nil, errors.New("managed browser installation leases require Linux")
}
func executableDigest(string) (string, error) {
	return "", errors.New("managed browser executable verification requires Linux")
}
