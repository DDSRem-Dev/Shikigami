package version

import (
	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"testing"
)

func TestNegotiateVersionAndCapabilities(t *testing.T) {
	local := &pb.ProtocolOffer{Major: 1, MinMinor: 2, MaxMinor: 5, Capabilities: []string{"terminal", "events"}, RequiredCapabilities: []string{"terminal"}}
	remote := &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 4, Capabilities: []string{"events", "terminal", "future"}, RequiredCapabilities: []string{"events"}}
	result, err := Negotiate(local, remote, "build-not-a-protocol")
	if err != nil || result.Minor != 4 || result.Major != 1 || len(result.Capabilities) != 2 || result.Capabilities[0] != "events" {
		t.Fatalf("negotiation: %v %v", result, err)
	}
	for name, offer := range map[string]*pb.ProtocolOffer{
		"missing":                    nil,
		"major":                      {Major: 2, MinMinor: 2, MaxMinor: 4, Capabilities: []string{"terminal"}},
		"disjoint":                   {Major: 1, MinMinor: 6, MaxMinor: 7, Capabilities: []string{"terminal"}},
		"invalid range":              {Major: 1, MinMinor: 4, MaxMinor: 3, Capabilities: []string{"terminal"}},
		"missing local requirement":  {Major: 1, MinMinor: 2, MaxMinor: 4, Capabilities: []string{"events"}},
		"missing remote requirement": {Major: 1, MinMinor: 2, MaxMinor: 4, Capabilities: []string{"terminal", "future"}, RequiredCapabilities: []string{"future"}},
		"unadvertised requirement":   {Major: 1, MinMinor: 2, MaxMinor: 4, Capabilities: []string{"terminal"}, RequiredCapabilities: []string{"events"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Negotiate(local, offer, ""); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("got %v", err)
			}
		})
	}
}
