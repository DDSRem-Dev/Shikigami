// Package version negotiates application protocol versions independently from
// Connect's wire version and software build versions.
package version

import (
	"sort"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func Negotiate(local, remote *pb.ProtocolOffer, build string) (*pb.ProtocolAgreement, error) {
	fail := func(missing ...string) (*pb.ProtocolAgreement, error) {
		return nil, incompatible(local, remote, build, missing)
	}
	if local == nil || remote == nil || local.Major == 0 || local.Major != remote.Major || local.MinMinor > local.MaxMinor || remote.MinMinor > remote.MaxMinor {
		return fail()
	}
	minor := min(local.MaxMinor, remote.MaxMinor)
	if minor < max(local.MinMinor, remote.MinMinor) {
		return fail()
	}
	a, b := map[string]bool{}, map[string]bool{}
	for _, c := range local.Capabilities {
		if c != "" {
			a[c] = true
		}
	}
	for _, c := range remote.Capabilities {
		if c != "" {
			b[c] = true
		}
	}
	common := map[string]bool{}
	for c := range a {
		if b[c] {
			common[c] = true
		}
	}
	for _, required := range [][]string{local.RequiredCapabilities, remote.RequiredCapabilities} {
		for _, c := range required {
			if !common[c] {
				return fail(c)
			}
		}
	}
	capabilities := make([]string, 0, len(common))
	for c := range common {
		capabilities = append(capabilities, c)
	}
	sort.Strings(capabilities)
	return &pb.ProtocolAgreement{Major: local.Major, Minor: minor, Capabilities: capabilities, BuildVersion: build}, nil
}
