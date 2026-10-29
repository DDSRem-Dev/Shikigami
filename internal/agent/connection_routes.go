package agent

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

type remoteCandidate struct {
	base      *connect.Client
	close     func()
	agreement *pb.ProtocolAgreement
	daemonID  string
	route     string
}

type routeFactory func(string) (*connect.Client, func(), error)

// establishRemote probes only authentication, protocol and stable identity.
// It never receives a business operation and therefore cannot replay one.
func establishRemote(ctx context.Context, server *apb.ServerConfig, factory routeFactory) (*remoteCandidate, []string, error) {
	var routes []string
	if server.TailscaleAddress != "" {
		routes = append(routes, "tsnet")
	}
	if server.DirectTlsAddress != "" {
		routes = append(routes, "direct-tls")
	}
	var last error
	var attempts []string
	for index, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, attempts, err
		}
		attempts = append(attempts, route)
		base, closePool, err := factory(route)
		if err != nil {
			if closePool != nil {
				closePool()
			}
			last = err
		} else {
			probe, cancel, _ := remoteProbeContext(ctx, len(routes)-index)
			agreement, daemonID, probeErr := negotiateRemote(probe, base, server.DaemonId)
			err = classifyProbeError(ctx, probe, probeErr)
			cancel()
			if err == nil {
				return &remoteCandidate{base: base, close: closePool, agreement: agreement, daemonID: daemonID, route: route}, attempts, nil
			}
			closePool()
			last = err
		}
		if !connection.IsRetryableNetworkError(last) {
			return nil, attempts, last
		}
	}
	if last == nil {
		last = rpcError(connect.CodeFailedPrecondition, "NO_CONFIGURED_ROUTE")
	}
	return nil, attempts, last
}

// Reserve a fair share of a short caller deadline for each configured fallback.
// Parent cancellation/deadline always wins; no candidate extends that budget.
const remoteProbeBudget = 5 * time.Second

func remoteProbeContext(parent context.Context, candidates int) (context.Context, context.CancelFunc, bool) {
	budget := remoteProbeBudget
	if deadline, ok := parent.Deadline(); ok {
		budget = min(budget, time.Until(deadline)/time.Duration(max(candidates, 1)))
	}
	probe, cancel := context.WithTimeout(parent, budget)
	return probe, cancel, budget < remoteProbeBudget
}

type remoteProbeTimeout struct{ callerLimited bool }

func (remoteProbeTimeout) Error() string   { return "remote authentication probe timed out" }
func (remoteProbeTimeout) Timeout() bool   { return true }
func (remoteProbeTimeout) Temporary() bool { return true }

func classifyProbeError(parent, probe context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	var remote *connect.Error
	if errors.As(err, &remote) && remote.IsRemote() {
		return err
	}
	// A local transport can report its deadline before the context timer's
	// callback is scheduled. Preserve the causal local error in that case;
	// remote application deadlines above are never route/fallback evidence.
	if errors.Is(err, context.DeadlineExceeded) || err != nil && probe.Err() == context.DeadlineExceeded && connect.CodeOf(err) == connect.CodeDeadlineExceeded {
		return remoteProbeTimeout{}
	}
	return err
}

func negotiateRemote(ctx context.Context, base *connect.Client, pinnedID string) (*pb.ProtocolAgreement, string, error) {
	remote := rpc.NewShikiDaemonClient(base)
	agreement, err := remote.NegotiateProtocol(FreshRemoteContext(ctx), remoteProtocolOffer())
	if err != nil {
		return nil, "", version.WithComponent(err, "daemon")
	}
	if agreement.Major != 1 || agreement.Minor != 0 {
		return nil, "", rpcError(connect.CodeFailedPrecondition, "PROTOCOL_INCOMPATIBLE")
	}
	daemonID, err := negotiatedDaemonID(ctx, remote, agreement)
	if err != nil {
		return nil, "", err
	}
	if !validID(daemonID, "daemon_") || pinnedID != "" && daemonID != pinnedID {
		return nil, "", rpcError(connect.CodePermissionDenied, "DAEMON_ID_MISMATCH")
	}
	return agreement, daemonID, nil
}

func (r *Resources) connectionPath(ctx context.Context, route, address string) string {
	if route == "direct-tls" {
		return route
	}
	if r.node == nil {
		return "unknown"
	}
	probe, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	path := r.node.ConnectionPath(probe, address)
	if path == "tailscale-direct" || path == "tailscale-relay" {
		return path
	}
	return "unknown"
}

func remoteProtocolOffer() *pb.ProtocolOffer {
	return &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1", "terminal.screen.v1", "terminal.logs.v1", "session.cleanup.v1", "project.directories.v1", "session-config-snapshot.v1", "sync.revision.v1", "port.tcp-half-close.v1", "port.authorization.v1", "port.auto-forward.v1", "plugin.manage.v1", "stats.v1"}}
}

// Identity verification never depends on an unrelated business permission for
// new peers. Older peers retain the authenticated GetSystemInfo fallback; a
// denied fallback is an explicit failure, never permission to omit pinning.
func negotiatedDaemonID(ctx context.Context, remote rpc.ShikiDaemonClient, agreement *pb.ProtocolAgreement) (string, error) {
	if agreement.GetDaemonId() != "" {
		return agreement.DaemonId, nil
	}
	info, err := remote.GetSystemInfo(FreshRemoteContext(ctx), &pb.Empty{})
	if err != nil {
		return "", err
	}
	agreement.GrantedPermissions = nil // legacy peers did not authenticate this metadata contract
	return info.GetDaemonId(), nil
}
func (r *Resources) newTrustedRoute(server *apb.ServerConfig, token, route string) (*connect.Client, func(), error) {
	// Each route is fixed to its explicitly configured authority. Selection and
	// authenticated probing happen in establishRemote, before exposing a client.
	if route == "direct-tls" {
		var roots *x509.CertPool
		if server.TlsCaFile != "" {
			var err error
			roots, err = connection.LoadRootCAs(server.TlsCaFile)
			if err != nil {
				return nil, nil, rpcError(connect.CodeFailedPrecondition, "TRUST_CONFIG_INVALID")
			}
		}
		endpoint, err := connection.NewTLSClient("https://"+server.DirectTlsAddress, connection.TLSOptions{ServerName: server.TlsServerName, RootCAs: roots})
		if err != nil {
			return nil, nil, err
		}
		bearer, err := connection.BearerToken(token)
		if err != nil {
			endpoint.Close()
			return nil, nil, err
		}
		return connection.NewRPCClient(endpoint, bearer), endpoint.Close, nil
	}
	if r.node == nil || server.TailscaleAddress == "" {
		return nil, nil, rpcError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	}
	pool, err := r.node.NewRPCPool(server.TailscaleAddress, token)
	if err != nil {
		return nil, nil, err
	}
	return pool.Client, pool.Close, nil
}
