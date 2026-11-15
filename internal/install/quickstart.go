package install

import (
	"errors"
	"os"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
)

// QuickstartSupport describes verified installed support, not application or
// browser readiness. Runtime readiness remains the host/plugin's responsibility.
type QuickstartSupport struct {
	Current          bool
	KeyringMode      string
	ReuseCredential  bool
	ProvisionKeyring bool
}

func quickstartSameRelease(a, b releaseauth.Verified) bool {
	// Archive is the verifier's temporary private COPY path, not provenance.
	// Every signed identity/digest field must match; temporary paths must not.
	a.Archive, b.Archive = "", ""
	return a == b
}

func quickstartRetainedKeyring(path, mode string, uid uint32) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return mode == "dedicated", nil
	}
	if err != nil {
		return false, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || owner.Uid != uid || info.Mode().Perm()&0077 != 0 || owner.Nlink != 1 {
		return false, errors.New("retained keyring store is unsafe; preserved for administrator review")
	}
	return false, nil
}
