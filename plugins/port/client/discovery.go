package client

import (
	"context"
	"net/netip"
	"sort"

	"connectrpc.com/connect/v2"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

// Discovery never binds listeners: only the host can persist and authorize a
// returned candidate, then supply its assigned ID/revision to StartForward.
func (w *Worker) DiscoverForwards(ctx context.Context, scope *ppb.Scope) (*ppb.ForwardCandidates, error) {
	if scope == nil || scope.ServerId == "" || scope.SessionId != "" || scope.ProjectId != "" || scope.MappingId != "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "DISCOVERY_SERVER_SCOPE_REQUIRED")
	}
	w.mu.Lock()
	cfg, core, closed := w.config, w.core, w.closed
	w.mu.Unlock()
	if closed || core == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "PORT_WORKER_STOPPED")
	}
	out := &ppb.ForwardCandidates{}
	if !cfg.AutoForward {
		return out, nil
	}
	list, e := core.RemoteListPorts(ctx, &ppb.RemoteListPortsRequest{Scope: scope, Request: &pb.ListPortsRequest{}})
	if e != nil {
		return nil, e
	}
	for _, p := range list.GetPorts() {
		if p.Protocol != "tcp" || p.Pid <= 0 || p.SessionId == "" || len(p.SourceIdentity) != 64 || p.Port < 1 || p.Port > 65535 || schema.Protected(uint16(p.Port)) || !cfg.AutoAllowed[uint16(p.Port)] {
			continue
		}
		address := p.BindAddress
		switch address {
		case "0.0.0.0":
			address = "127.0.0.1"
		case "::":
			address = "::1"
		}
		ip, e := netip.ParseAddr(address)
		if e != nil || !ip.IsLoopback() || ip.Zone() != "" {
			continue
		}
		address = ip.Unmap().String()
		check, e := core.RemoteCheckPort(ctx, &ppb.RemoteCheckPortRequest{Scope: &ppb.Scope{ServerId: scope.ServerId, SessionId: p.SessionId}, Request: &pb.CheckPortRequest{RemoteAddress: address, Port: p.Port, SessionId: p.SessionId, Automatic: true, SourceIdentity: p.SourceIdentity}})
		if e != nil {
			if connect.CodeOf(e) == connect.CodePermissionDenied {
				continue
			}
			return nil, e
		}
		if check.GetPort().GetSessionId() != p.SessionId || check.GetPort().GetSourceIdentity() != p.SourceIdentity {
			return nil, connect.NewError(connect.CodeDataLoss, "INVALID_PORT_AUTHORIZATION")
		}
		out.Specs = append(out.Specs, &ppb.ForwardSpec{ServerId: scope.ServerId, SessionId: p.SessionId, RemoteAddress: address, RemotePort: uint32(p.Port), LocalAddress: cfg.BindAddress, LocalPort: uint32(p.Port), Enabled: true, Automatic: true, SourceIdentity: p.SourceIdentity})
		if len(out.Specs) > 256 {
			return nil, connect.NewError(connect.CodeResourceExhausted, "AUTOMATIC_FORWARD_LIMIT")
		}
	}
	sort.Slice(out.Specs, func(i, j int) bool {
		a, b := out.Specs[i], out.Specs[j]
		if a.RemoteAddress != b.RemoteAddress {
			return a.RemoteAddress < b.RemoteAddress
		}
		return a.RemotePort < b.RemotePort
	})
	return out, nil
}
