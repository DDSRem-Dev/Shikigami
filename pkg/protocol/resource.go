package protocol

import (
	"regexp"
	"strings"
)

var resourceID = regexp.MustCompile(`^(proj|sess|snap)_[a-zA-Z0-9_-]{1,96}$`)

// ValidResourceID checks the wire shape only, never ownership or existence.
func ValidResourceID(id, prefix string) bool {
	return strings.HasPrefix(id, prefix+"_") && resourceID.MatchString(id)
}
