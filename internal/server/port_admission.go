package server

import (
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// These gates protect the host before generated unary/server-stream handlers
// decode the request. Worker limits alone cannot bound a silent HTTP2 body.
// ForwardPort is admitted by its worker before reading CONNECT, independently.
type portAdmission struct {
	metadata atomic.Int32
	watches  atomic.Int32
}

func (a *portAdmission) budget(procedure string) (*atomic.Int32, int32) {
	switch procedure {
	case rpc.ShikiDaemonListPortsProcedure, rpc.ShikiDaemonCheckPortProcedure:
		return &a.metadata, limits.PortMetadataCalls
	case rpc.ShikiDaemonWatchPortsProcedure:
		return &a.watches, limits.PortWatches
	default:
		return nil, 0
	}
}

func (a *portAdmission) interceptor() connect.ServerInterceptor {
	return admissionInterceptor(a.budget, limits.PortFirstFrameTimeout, "PORT")
}
