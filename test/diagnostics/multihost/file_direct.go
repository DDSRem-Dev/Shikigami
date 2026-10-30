package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// directFiles is a diagnostic-only client for conditional file primitives.
// Normal frontend RPC remains on the agent proxy, whose raw-file denylist is
// intentional. Trust comes from this run's sole authenticated server record;
// callers cannot supply a target, CA, credential filename, or bearer value.
func (h *helper) directFiles(ctx context.Context, frontendName, serverID, expectedDaemonID string) (rpc.ShikiDaemonClient, func(), error) {
	f, err := h.front(ctx, frontendName)
	if err != nil {
		return nil, nil, err
	}
	servers, err := f.Agent().ListServers(ctx, &pb.Empty{})
	if err != nil {
		return nil, nil, err
	}
	if len(servers.Servers) != 1 || servers.Servers[0].GetConfig().GetId() != serverID {
		return nil, nil, invalid()
	}
	return h.openDirectFiles(ctx, servers.Servers[0].Config, expectedDaemonID)
}

func (h *helper) openDirectFiles(ctx context.Context, server *apb.ServerConfig, expectedDaemonID string) (rpc.ShikiDaemonClient, func(), error) {
	if server == nil || !strings.HasPrefix(expectedDaemonID, "daemon_") || server.DaemonId != expectedDaemonID || !loopback(server.DirectTlsAddress) || server.TlsServerName == "" || server.TlsCaFile == "" {
		return nil, nil, invalid()
	}
	ca, err := h.readDirectFile(server.TlsCaFile, 64<<10, false)
	if err != nil {
		return nil, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, nil, invalid()
	}
	token, err := h.readDirectFile(filepath.Join(h.root, "device.token"), 8<<10, true)
	if err != nil {
		return nil, nil, err
	}
	defer clear(token)
	bearer, err := connection.BearerToken(string(bytes.TrimSpace(token)))
	if err != nil {
		return nil, nil, err
	}
	endpoint, err := connection.NewTLSClient("https://"+server.DirectTlsAddress, connection.TLSOptions{ServerName: server.TlsServerName, RootCAs: roots})
	if err != nil {
		return nil, nil, err
	}
	client := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, directFileMethods, bearer))
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	agreement, err := client.NegotiateProtocol(probe, &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.FileCapability}, RequiredCapabilities: []string{sdk.FileCapability}})
	cancel()
	if err != nil {
		endpoint.Close()
		return nil, nil, err
	}
	if agreement == nil || agreement.Major != 1 || agreement.Minor != 0 || !slices.Contains(agreement.Capabilities, sdk.FileCapability) {
		endpoint.Close()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "file protocol unavailable")
	}
	if agreement.DaemonId != expectedDaemonID {
		endpoint.Close()
		return nil, nil, connect.NewError(connect.CodePermissionDenied, "daemon identity mismatch")
	}
	return client, endpoint.Close, nil
}

// Even internal reuse of this client cannot turn it into an alternate session,
// project, token, or plugin management channel.
func directFileMethods(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		switch spec.Procedure {
		case rpc.ShikiDaemonNegotiateProtocolProcedure, rpc.ShikiDaemonStatFileProcedure, rpc.ShikiDaemonWriteFileProcedure:
			return next(ctx, spec)
		default:
			return nil, connect.NewError(connect.CodePermissionDenied, "diagnostic file method required")
		}
	}
}

func (h *helper) readDirectFile(path string, limit int64, private bool) ([]byte, error) {
	relative, err := h.path(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(h.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || private && info.Mode().Perm()&0077 != 0 {
		return nil, invalid()
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		clear(data)
		return nil, invalid()
	}
	return data, nil
}
