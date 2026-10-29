package install

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

// ProbeReport travels only through the ordinary-user helper's private pipe.
// Node may contain a login URL; public Result must copy only its safe fields.
type ProbeReport struct {
	Node      *apb.NodeStatus            `json:"node"`
	State     string                     `json:"state"`
	Reason    string                     `json:"reason"`
	Readiness *apb.DaemonReadinessStatus `json:"readiness,omitempty"`
	Agent     *apb.AgentStatus           `json:"agent,omitempty"`
	Plugins   []*pb.PluginStatus         `json:"plugins,omitempty"`
}

func localInstance(header, instance string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			// Calls are unary; the caller owns the bounded context through Receive.
			call, info := connect.NewClientContext(ctx)
			info.RequestHeader().Set(header, instance)
			return next(call, spec)
		}
	}
}

func probeUser(parent context.Context, request UserOperation) (*ProbeReport, error) {
	ctx, cancel := context.WithTimeout(parent, 40*time.Second)
	defer cancel()
	socket := filepath.Join(request.Plan.StateDir, "run", "admin.sock")
	allowDisabled := false
	if request.Plan.Role == "client" {
		cfg, err := config.LoadClient(request.Plan.Config)
		if err != nil {
			return nil, err
		}
		if cfg.Agent.StateDir != request.Plan.StateDir {
			return nil, errors.New("client state directory differs from installation receipt")
		}
		socket, allowDisabled = cfg.Agent.Socket, !cfg.Tailscale.Enabled
	} else {
		cfg, err := config.LoadDaemon(request.Plan.Config)
		if err != nil {
			return nil, err
		}
		if cfg.StateDir != request.Plan.StateDir || cfg.RunUser != request.Plan.User {
			return nil, errors.New("daemon identity path differs from installation receipt")
		}
		allowDisabled = !cfg.Tailscale.Enabled && cfg.Server.DirectTLS.Enabled
	}
	startup, stopStartup := context.WithTimeout(ctx, 20*time.Second)
	defer stopStartup()
	var endpoint *connection.Endpoint
	defer func() {
		if endpoint != nil {
			endpoint.Close()
		}
	}()
	var negotiation *apb.NodeNegotiation
	var err error
	for {
		if endpoint == nil {
			endpoint, err = connection.NewUnixClient(socket, uint32(os.Getuid()), "shiki-installer-control")
			// Service startup may not have created the private runtime path yet.
			// Ownership, permissions and socket-type failures still fail closed.
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if endpoint != nil {
			raw := arpc.NewNodeControlClient(connection.NewRPCClient(endpoint))
			call, stop := context.WithTimeout(startup, 2*time.Second)
			negotiation, err = raw.Negotiate(call, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}})
			stop()
			if err == nil {
				break
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if startup.Err() != nil {
			return nil, errors.Join(errors.New("local NodeControl did not become ready within 20 seconds"), startup.Err())
		}
		select {
		case <-startup.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	stopStartup()
	if negotiation.InstanceId == "" || negotiation.Protocol.GetMajor() != 1 || negotiation.Protocol.GetMinor() != 0 || negotiation.Protocol.GetBuildVersion() != buildinfo.Version {
		return nil, errors.New("local node identity or installed build differs")
	}
	api := arpc.NewNodeControlClient(connection.NewRPCClient(endpoint, localInstance("shiki-node-control-instance-id", negotiation.InstanceId)))
	call, stop := context.WithTimeout(ctx, 2*time.Second)
	status, err := api.GetStatus(call, &pb.Empty{})
	stop()
	if err != nil {
		return nil, err
	}
	status, err = guideLogin(ctx, api, status, request, slices.Contains(negotiation.Protocol.GetCapabilities(), "node.login.v1"))
	if err != nil {
		return nil, err
	}
	report := &ProbeReport{Node: status, State: "node_" + status.State, Reason: status.ReasonCode}
	if report.Reason == "" {
		report.Reason = "NODE_LOGIN_PENDING"
	}
	if !nodeAllowsApplication(status, allowDisabled) {
		return report, nil
	}
	report.State, report.Reason = "application_pending", "APPLICATION_STATUS_UNAVAILABLE"
	if request.Plan.Role == "client" {
		// Installer compatibility is intentionally stricter than an ordinary
		// frontend: bind the actual Agent handshake to the selected build.
		call, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		rawAgent := arpc.NewAgentClient(connection.NewRPCClient(endpoint))
		hello, err := rawAgent.Handshake(call, &apb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1, RequiredCapabilities: []string{"agent.control.v1"}, Capabilities: []string{"agent.control.v1"}}, ClientBuild: buildinfo.Version, DeviceName: "shiki-install"})
		if err != nil {
			return report, nil
		}
		identified := func(next connect.ClientFunc) connect.ClientFunc {
			return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
				call, info := connect.NewClientContext(ctx)
				info.RequestHeader().Set("shiki-agent-instance-id", hello.InstanceId)
				info.RequestHeader().Set("shiki-frontend-id", hello.FrontendId)
				return next(call, spec)
			}
		}
		agent := arpc.NewAgentClient(connection.NewRPCClient(endpoint, identified))
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_, _ = agent.ReleaseFrontend(cleanup, &pb.Empty{})
		}()
		if hello.InstanceId == "" || hello.FrontendId == "" || hello.Protocol.GetMajor() != 1 || hello.Protocol.GetMinor() != 0 || hello.Protocol.GetBuildVersion() != buildinfo.Version || !slices.Contains(hello.Protocol.GetCapabilities(), "agent.control.v1") {
			return nil, errors.New("agent identity or selected build differs")
		}
		report.Agent, err = agent.GetStatus(call, &pb.Empty{})
		if err != nil {
			return report, nil
		}
		if report.Agent.InstanceId != hello.InstanceId {
			return nil, errors.New("agent instance changed during readiness")
		}
		plugins, err := agent.ListPlugins(call, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
		if err != nil {
			report.Reason = "PLUGIN_STATUS_UNAVAILABLE"
			return report, nil
		}
		report.Plugins = plugins.Plugins
		if report.Agent.State != "ready" {
			report.Reason = "AGENT_NOT_READY"
			return report, nil
		}
		for _, plugin := range plugins.Plugins {
			if plugin.Enabled && plugin.State != "ready" {
				report.Reason = "ENABLED_PLUGIN_NOT_READY"
				return report, nil
			}
		}
	} else {
		call, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		rawReadiness := arpc.NewDaemonReadinessClient(connection.NewRPCClient(endpoint))
		hello, err := rawReadiness.Negotiate(call, &pb.ProtocolOffer{Major: 1, RequiredCapabilities: []string{"daemon.readiness.v1"}, Capabilities: []string{"daemon.readiness.v1"}})
		if err != nil {
			report.Reason = "DAEMON_READINESS_UNAVAILABLE"
			return report, nil
		}
		if hello.InstanceId == "" || hello.Protocol.GetMajor() != 1 || hello.Protocol.GetMinor() != 0 || hello.Protocol.GetBuildVersion() != buildinfo.Version || hello.Protocol.GetDaemonId() == "" || !slices.Contains(hello.Protocol.GetCapabilities(), "daemon.readiness.v1") {
			return nil, errors.New("daemon readiness negotiation identity or build differs")
		}
		readiness := arpc.NewDaemonReadinessClient(connection.NewRPCClient(endpoint, localInstance("shiki-daemon-readiness-instance-id", hello.InstanceId)))
		report.Readiness, err = readiness.GetStatus(call, &pb.Empty{})
		if err != nil {
			report.Reason = "DAEMON_READINESS_UNAVAILABLE"
			return report, nil
		}
		if report.Readiness.InstanceId != hello.InstanceId || report.Readiness.System.GetDaemonId() != hello.Protocol.DaemonId || report.Readiness.System.GetDaemonVersion() != buildinfo.Version {
			return nil, errors.New("daemon readiness identity changed")
		}
		if report.Readiness.State != "local_application_ready" || !report.Readiness.RemoteListenerReady || report.Readiness.Health == nil || report.Readiness.Health.Status != pb.HealthStatus_HEALTHY {
			report.Reason = report.Readiness.ReasonCode
			if report.Reason == "" {
				report.Reason = "DAEMON_NOT_READY"
			}
			return report, nil
		}
	}
	// A host restart or node revocation between the two independent local
	// negotiations cannot inherit an earlier node's successful observation.
	call, stop = context.WithTimeout(ctx, 2*time.Second)
	finalNode, err := api.GetStatus(call, &pb.Empty{})
	stop()
	if err != nil {
		return nil, err
	}
	report.Node = finalNode
	if !nodeAllowsApplication(finalNode, allowDisabled) {
		report.State, report.Reason = "node_"+finalNode.State, finalNode.ReasonCode
		if report.Reason == "" {
			report.Reason = "NODE_LOGIN_PENDING"
		}
		return report, nil
	}
	report.State, report.Reason = "local_application_ready", "LOCAL_CONDITIONS_VERIFIED"
	return report, nil
}

func nodeAllowsApplication(status *apb.NodeStatus, allowDisabled bool) bool {
	if status.State == "online" {
		return true
	}
	if !allowDisabled {
		return false
	}
	// Both local NodeControl implementations preserve the documented offline
	// state when tsnet is explicitly disabled. Other offline reasons must not
	// bypass node readiness, even when installation configuration disables it.
	return status.State == "offline" && status.ReasonCode == "NODE_DISABLED"
}

func ExistingConfiguration(plan Plan) (bool, error) {
	var err error
	if plan.Role == "client" {
		_, err = config.LoadClient(plan.Config)
	} else {
		_, err = config.LoadDaemon(plan.Config)
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func guideLogin(parent context.Context, api arpc.NodeControlClient, status *apb.NodeStatus, request UserOperation, canLogin bool) (*apb.NodeStatus, error) {
	if status.State == "online" || (status.State == "offline" && status.ReasonCode == "NODE_DISABLED") {
		return status, nil
	}
	active := status.State == "authenticating" || (status.LoginId != "" && status.LoginExpiresAt > time.Now().Unix() && status.State != "denied" && status.State != "expired")
	if !active && canLogin && (len(request.Key) != 0 || request.Interactive) {
		login := &apb.BeginLoginRequest{RequestId: "install-" + rand.Text(), Method: &apb.BeginLoginRequest_Interactive{Interactive: true}}
		if len(request.Key) != 0 {
			login.Method = &apb.BeginLoginRequest_AuthKey{AuthKey: request.Key}
		}
		call, stop := context.WithTimeout(parent, 3*time.Second)
		next, err := api.BeginLogin(call, login)
		stop()
		if err != nil {
			if connect.CodeOf(err) != connect.CodeAlreadyExists {
				return nil, err
			}
			// Another owner frontend may have begun the same node's login.
			call, stop := context.WithTimeout(parent, 2*time.Second)
			status, err = api.GetStatus(call, &pb.Empty{})
			stop()
			if err != nil {
				return nil, err
			}
		} else {
			status = next
		}
		active = true
	}
	if !active {
		return status, nil
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	for {
		validURL := status.LoginUrl != "" && status.LoginId != "" && status.LoginExpiresAt > time.Now().Unix()
		if validURL || status.State == "online" || status.State == "denied" || status.State == "expired" || status.ReasonCode == "LOGIN_START_FAILED" || status.ReasonCode == "LOGIN_INTERRUPTED" || status.ReasonCode == "LOGIN_CANCELED" || status.ReasonCode == "LOGIN_EXPIRED" {
			return status, nil
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return nil, parent.Err()
			}
			return status, nil
		case <-time.After(100 * time.Millisecond):
		}
		call, stop := context.WithTimeout(ctx, 2*time.Second)
		next, err := api.GetStatus(call, &pb.Empty{})
		stop()
		if err != nil {
			if ctx.Err() != nil && parent.Err() == nil {
				return status, nil
			}
			return nil, err
		}
		status = next
	}
}
