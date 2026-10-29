package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

const readinessInstanceHeader = "shiki-daemon-readiness-instance-id"

// This read-only service is mounted exclusively on the owner-checked admin
// listener. It does not expose application operations or node login material.
type daemonReadiness struct {
	runtime  *Runtime
	instance string
}

func newDaemonReadiness(r *Runtime) *daemonReadiness {
	return &daemonReadiness{runtime: r, instance: "readiness_" + rand.Text()}
}
func (d *daemonReadiness) authorize(ctx context.Context, negotiate bool) error {
	if uid, ok := connection.PeerUID(ctx); !ok || uid != uint32(os.Geteuid()) {
		return connect.NewError(connect.CodePermissionDenied, "IPC_UID_REQUIRED")
	}
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok || len(info.RequestHeader().Values("Authorization")) != 0 {
		return connect.NewError(connect.CodePermissionDenied, "LOCAL_READINESS_REQUIRED")
	}
	if !negotiate {
		values := info.RequestHeader().Values(readinessInstanceHeader)
		if len(values) != 1 || values[0] != d.instance {
			return connect.NewError(connect.CodeFailedPrecondition, "DAEMON_READINESS_RESTARTED")
		}
	}
	if d.runtime.ctx.Err() != nil {
		return connect.NewError(connect.CodeUnavailable, "DAEMON_STOPPING")
	}
	return nil
}
func (d *daemonReadiness) Negotiate(ctx context.Context, offer *pb.ProtocolOffer) (*apb.ReadinessNegotiation, error) {
	if err := d.authorize(ctx, true); err != nil {
		return nil, err
	}
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1, Capabilities: []string{"daemon.readiness.v1"}}, offer, d.runtime.Service.Build)
	if err != nil {
		return nil, err
	}
	agreement.DaemonId, err = d.runtime.Service.Auth.DaemonID()
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "DAEMON_IDENTITY_UNAVAILABLE")
	}
	return &apb.ReadinessNegotiation{Protocol: agreement, InstanceId: d.instance}, nil
}
func (d *daemonReadiness) GetStatus(parent context.Context, _ *pb.Empty) (*apb.DaemonReadinessStatus, error) {
	if err := d.authorize(parent, false); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	r := d.runtime
	system, err := r.Service.GetSystemInfo(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	result := &apb.DaemonReadinessStatus{InstanceId: d.instance, State: "local_application_ready", System: system, Health: r.health(), RemoteListenerReady: r.Info().RemoteReady, ClaudeLoginState: "unknown", ExternalPeerAcceptance: "pending"}
	degrade := func(reason string) {
		result.State = "degraded"
		if result.ReasonCode == "" {
			result.ReasonCode = reason
		}
		result.Health.Status = pb.HealthStatus_DEGRADED
	}
	if !result.RemoteListenerReady {
		degrade("REMOTE_LISTENER_PENDING")
	}
	if r.state == nil {
		degrade("STATE_UNAVAILABLE")
	} else if _, err := r.state.View(); err != nil {
		degrade("STATE_UNAVAILABLE")
	}
	plugins, err := r.Service.ListPlugins(ctx, &pb.ListPluginsRequest{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "PLUGIN_STATUS_UNAVAILABLE")
	}
	observed := make(map[string]*pb.PluginStatus, len(plugins.Plugins))
	instances := make(map[string]string, len(plugins.Plugins))
	for _, status := range plugins.Plugins {
		if _, duplicate := observed[status.PluginId]; duplicate {
			degrade("PLUGIN_STATUS_CHANGED")
		}
		observed[status.PluginId] = status
		if !status.Enabled {
			continue
		}
		if r.Service.Supervisor == nil {
			degrade("ENABLED_PLUGIN_NOT_READY")
			continue
		}
		instances[status.PluginId] = r.Service.Supervisor.ReadyInstanceID(status.PluginId)
		if instances[status.PluginId] == "" {
			degrade("ENABLED_PLUGIN_NOT_READY")
			continue
		}
		if status.State != "ready" {
			degrade("ENABLED_PLUGIN_NOT_READY")
			continue
		}
		call, stop := context.WithTimeout(ctx, time.Second)
		err := r.Service.Supervisor.Call(call, status.PluginId, "core.scope.v1", func(ctx context.Context, worker sdk.Lifecycle) error {
			health, err := worker.Health(ctx)
			if err != nil {
				return err
			}
			if health == nil || health.Status != pb.HealthStatus_HEALTHY {
				return connect.NewError(connect.CodeFailedPrecondition, "PLUGIN_UNHEALTHY")
			}
			return nil
		})
		stop()
		if err != nil {
			degrade("ENABLED_PLUGIN_UNHEALTHY")
		}
	}
	result.ClaudeVersion, err = r.Service.GetClaudeVersion(ctx, &pb.Empty{})
	if err != nil {
		degrade("CLAUDE_VERSION_UNAVAILABLE")
	} else {
		result.ClaudeLoginState = claudeLoginState(ctx, result.ClaudeVersion.Path)
		if result.ClaudeLoginState == "reported_logged_out" {
			degrade("CLAUDE_LOGIN_REQUIRED")
		}
		if result.ClaudeLoginState == "unknown" {
			degrade("CLAUDE_LOGIN_UNVERIFIED")
		}
	}
	if r.ctx.Err() != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "DAEMON_STOPPING")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := r.Service.ListPlugins(ctx, &pb.ListPluginsRequest{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "PLUGIN_STATUS_UNAVAILABLE")
	}
	result.Plugins = current.Plugins
	if len(current.Plugins) != len(observed) {
		degrade("PLUGIN_STATUS_CHANGED")
	}
	seen := map[string]bool{}
	for _, status := range current.Plugins {
		if seen[status.PluginId] {
			degrade("PLUGIN_STATUS_CHANGED")
		}
		seen[status.PluginId] = true
		if status.Enabled && status.State != "ready" {
			degrade("ENABLED_PLUGIN_NOT_READY")
		}
		previous, exists := observed[status.PluginId]
		if !exists || previous.Revision != status.Revision || previous.Enabled != status.Enabled {
			degrade("PLUGIN_STATUS_CHANGED")
		}
		if status.Enabled && (r.Service.Supervisor == nil || instances[status.PluginId] == "" || r.Service.Supervisor.ReadyInstanceID(status.PluginId) != instances[status.PluginId]) {
			degrade("PLUGIN_STATUS_CHANGED")
		}
	}
	for id := range observed {
		if !seen[id] {
			degrade("PLUGIN_STATUS_CHANGED")
		}
	}
	// Refresh host observations after dependency probes; a listener/plugin
	// transition during the query must not leave an optimistic outer state.
	result.Health = r.health()
	if r.state == nil {
		degrade("STATE_UNAVAILABLE")
	} else if _, err := r.state.View(); err != nil {
		degrade("STATE_UNAVAILABLE")
	}
	result.RemoteListenerReady = r.Info().RemoteReady
	if !result.RemoteListenerReady {
		degrade("REMOTE_LISTENER_PENDING")
	}
	if result.Health.Status != pb.HealthStatus_HEALTHY {
		degrade("HOST_HEALTH_DEGRADED")
	}
	if result.State != "local_application_ready" {
		result.Health.Status = pb.HealthStatus_DEGRADED
		result.Health.Message = "local readiness conditions not satisfied"
	}
	if result.Health.Details == nil {
		result.Health.Details = map[string]string{}
	}
	result.Health.Details["claude"] = result.ClaudeLoginState
	result.System, err = r.Service.GetSystemInfo(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Only the strict boolean survives this bounded CLI query. Account metadata,
// unknown output fields, raw stderr and credentials are never returned/logged.
type loginStatusOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (o *loginStatusOutput) Bytes() []byte { return o.buffer.Bytes() }

func (o *loginStatusOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := max(4096-o.buffer.Len(), 0)
	if len(data) > remaining {
		o.overflow = true
		data = data[:remaining]
	}
	o.buffer.Write(data)
	return n, nil
}
func claudeLoginState(parent context.Context, executable string) string {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "auth", "status", "--json")
	cmd.WaitDelay = time.Second
	var output loginStatusOutput
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	defer func() { clear(output.Bytes()) }()
	err := cmd.Run()
	if ctx.Err() != nil || output.overflow {
		return "unknown"
	}
	var fields map[string]json.RawMessage
	if err := strictjson.Decode(output.Bytes(), &fields); err != nil {
		return "unknown"
	}
	defer func() {
		for _, value := range fields {
			clear(value)
		}
	}()
	switch {
	case err == nil && bytes.Equal(bytes.TrimSpace(fields["loggedIn"]), []byte("true")):
		return "reported_logged_in"
	case bytes.Equal(bytes.TrimSpace(fields["loggedIn"]), []byte("false")):
		var exit *exec.ExitError
		if err == nil || (errors.As(err, &exit) && exit.ExitCode() == 1) {
			return "reported_logged_out"
		}
		return "unknown"
	default:
		return "unknown"
	}
}
