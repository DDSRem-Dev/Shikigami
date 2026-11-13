package pluginmarket

import (
	"errors"
	"strings"

	"golang.org/x/mod/semver"
)

// ValidateVersion accepts complete SemVer versions, retaining their exact text.
// A leading v is accepted for compatibility with release-style version names.
func ValidateVersion(value string) error {
	if value == "" || len(value) > MaxVersionLength {
		return errors.New("invalid plugin semantic version")
	}
	version := value
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	core := strings.SplitN(strings.SplitN(strings.TrimPrefix(version, "v"), "+", 2)[0], "-", 2)[0]
	if len(strings.Split(core, ".")) != 3 || !semver.IsValid(version) {
		return errors.New("invalid plugin semantic version")
	}
	return nil
}

// CompareVersions compares already validated versions using SemVer precedence.
func CompareVersions(a, b string) int {
	if !strings.HasPrefix(a, "v") {
		a = "v" + a
	}
	if !strings.HasPrefix(b, "v") {
		b = "v" + b
	}
	return semver.Compare(a, b)
}
