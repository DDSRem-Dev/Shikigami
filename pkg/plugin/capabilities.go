package plugin

import (
	"errors"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

const CoreCapability = "core.scope.v1"

// BuiltinProtocol describes the distinct boundaries defined in docs/09.
// A dual-side artifact advertises the union; a host requires only its own side.
func BuiltinProtocol(id, side string) (*pb.ProtocolOffer, error) {
	var client, server string
	switch id {
	case "sync":
		client, server = "sync.worker.v1", "sync.revision.v1"
	case "port":
		client, server = "port.worker.v1", "port.tcp-half-close.v1"
	default:
		return nil, errors.New("unknown built-in plugin")
	}
	offer := &pb.ProtocolOffer{Major: 1, Capabilities: []string{CoreCapability}, RequiredCapabilities: []string{CoreCapability}}
	switch side {
	case "client":
		offer.Capabilities = append(offer.Capabilities, client)
		offer.RequiredCapabilities = append(offer.RequiredCapabilities, client)
	case "server":
		offer.Capabilities = append(offer.Capabilities, server)
		offer.RequiredCapabilities = append(offer.RequiredCapabilities, server)
	case "both":
		offer.Capabilities = append(offer.Capabilities, client, server)
	default:
		return nil, errors.New("invalid plugin side")
	}
	if id == "port" {
		offer.Capabilities = append(offer.Capabilities, PortAutoCapability)
		if side != "client" {
			offer.Capabilities = append(offer.Capabilities, PortCheckCapability)
		}
	}
	return offer, nil
}
