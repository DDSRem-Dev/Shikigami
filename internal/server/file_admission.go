package server

import (
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// A Service owns one OS identity, so all its tokens, HTTP/2 connections and
// Handler instances share these gates. Metadata cannot queue behind transfers
// or long-lived watches. A full gate rejects immediately, without a waiter.
type fileAdmission struct {
	metadata  atomic.Int32
	transfers atomic.Int32
	watches   atomic.Int32
}

func (a *fileAdmission) budget(procedure string) (*atomic.Int32, int32) {
	switch procedure {
	case rpc.ShikiDaemonListFilesProcedure, rpc.ShikiDaemonStatFileProcedure, rpc.ShikiDaemonDeleteFileProcedure, erpc.BrowserServiceListBrowserFilesProcedure:
		return &a.metadata, limits.FileMetadataCalls
	case rpc.ShikiDaemonReadFileProcedure, rpc.ShikiDaemonWriteFileProcedure, erpc.BrowserServiceReadBrowserFileProcedure, erpc.BrowserServiceWriteBrowserFileProcedure:
		return &a.transfers, limits.FileTransfers
	case rpc.ShikiDaemonWatchFilesProcedure, erpc.BrowserServiceBrowserStreamProcedure:
		return &a.watches, limits.FileWatches
	default:
		return nil, 0
	}
}

func (a *fileAdmission) interceptor() connect.ServerInterceptor {
	return admissionInterceptor(a.budget, limits.FileFirstFrameTimeout, "FILE")
}
