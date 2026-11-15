//go:build !linux

package runtime

import (
	"errors"
	"net"
	"os"
)

func owned(os.FileInfo) bool      { return false }
func singleLink(os.FileInfo) bool { return false }
func syncDirectory(string) error  { return errors.New("managed browser runtime requires Linux") }
func verifyPeer(net.Conn) error   { return errors.New("managed browser runtime requires Linux") }
