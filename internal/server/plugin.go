package server

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/types/known/structpb"
)

// startPlugins binds the durable supervisor before any recovered worker can
// call CoreAPI. Installed artifacts are verified against their saved manifest;
// missing or incompatible workers leave their data plane unavailable.
func (r *Runtime) startPlugins(options DaemonOptions) error {
	hostID := "daemon_" + rand.Text()
	supervisor, e := plugin.NewSupervisor(r.ctx, plugin.SupervisorOptions{
		StateDir:   filepath.Join(r.cfg.StateDir, "plugin-control"),
		PluginsDir: r.cfg.PluginsDir,
		Side:       "server",
		Validate:   validateServerPluginConfig,
		Config: func(id string, values *structpb.Struct) (plugin.Config, error) {
			var manifest plugin.Manifest
			if plugin.BuiltinAvailable(id, "server") {
				var e error
				manifest, e = plugin.ResolveBuiltinManifest(r.cfg.PluginsDir, id, "server", r.Service.Build)
				if e != nil {
					return plugin.Config{}, e
				}
			}
			core := &daemonPluginCore{service: r.Service, id: id}
			callbacks := []string{"core.projects.read", "core.sessions.read", "core.config.read", "core.log"}
			var region *browserRegionCore
			if id == "browser" {
				callbacks = []string{"core.config.read", "core.log", "region.read"}
				region = &browserRegionCore{service: r.Service}
			} else if id == "region" {
				callbacks = []string{"core.config.read", "core.log"}
			}
			cfg := plugin.Config{Manifest: manifest, RunDir: filepath.Join(r.cfg.StateDir, "run", "p"), StateDir: filepath.Join(r.cfg.StateDir, "plugin-data", id), HostInstanceID: hostID, Values: values, Core: core, CredentialLifetime: options.PluginCredentialLifetime, Policy: plugin.Policy{Side: "server", Capabilities: callbacks, Scopes: []*ppb.Scope{{}}, AuthorizeScope: core.authorizeScope}}
			if region != nil {
				cfg.Region = region
				cfg.StartupTimeout = 120 * time.Second
			}
			return cfg, nil
		},
		Disable: stopDisabledBrowser,
		Ready: func(ctx context.Context, _ string, worker sdk.Lifecycle) error {
			health, e := worker.Health(ctx)
			if e != nil {
				return e
			}
			if health == nil || health.Status != pb.HealthStatus_HEALTHY {
				return connect.NewError(connect.CodeFailedPrecondition, "PLUGIN_NOT_HEALTHY")
			}
			return nil
		},
	})
	if e != nil {
		return e
	}
	r.Service.Supervisor = supervisor
	supervisor.Start()
	return nil
}
func validateServerPluginConfig(id string, values *structpb.Struct) error {
	switch id {
	case "sync":
		return schema.ValidateSyncServer(values)
	case "port":
		_, err := schema.Parse(values, "server")
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, "INVALID_PORT_CONFIG")
		}
		return nil
	default:
		return connect.NewError(connect.CodeNotFound, "unknown plugin")
	}
}
func (s *Service) ListPlugins(context.Context, *pb.ListPluginsRequest) (*pb.ListPluginsResponse, error) {
	if s.Supervisor == nil {
		return &pb.ListPluginsResponse{Plugins: []*pb.PluginStatus{{PluginId: "sync", State: "stopped", ReasonCode: "NOT_CONFIGURED"}, {PluginId: "port", State: "stopped", ReasonCode: "NOT_CONFIGURED"}}}, nil
	}
	return s.Supervisor.List()
}
func (s *Service) ManagePlugin(ctx context.Context, request *pb.ManagePluginRequest) (*pb.PluginStatus, error) {
	if s.Supervisor == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "plugin management is unavailable")
	}
	return s.Supervisor.Manage(ctx, request)
}
func (s *Service) WatchPluginLogs(ctx context.Context, request *pb.PluginLogsRequest, stream rpc.ShikiDaemonWatchPluginLogsServerStream) error {
	if s.Supervisor == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "plugin management is unavailable")
	}
	return s.Supervisor.WatchLogs(ctx, request, stream.Send)
}
func (s *Service) capabilities() []string {
	available := map[string]bool{"terminal.v1": true, "session-config-snapshot.v1": true}
	if s.Sessions != nil {
		available["terminal.screen.v1"] = true
		available["terminal.logs.v1"] = true
		available["session.cleanup.v1"] = true
	}
	if s.Projects != nil {
		available["project.directories.v1"] = true
	}
	if s.Monitor != nil {
		available["stats.v1"] = true
	}
	if s.Supervisor != nil {
		available["plugin.manage.v1"] = true
		if list, e := s.Supervisor.List(); e == nil {
			for _, status := range list.Plugins {
				if !status.Enabled || status.State != "ready" {
					continue
				}
				capability := ""
				if status.PluginId == "sync" {
					capability = sdk.FileCapability
				} else if status.PluginId == "port" {
					capability = sdk.PortCapability
				} else if status.PluginId == "region" {
					capability = sdk.RegionCapability
				} else if status.PluginId == "browser" {
					capability = sdk.BrowserCapability
				}
				for _, offered := range status.Protocol.GetCapabilities() {
					if (offered == capability && capability != "") || (status.PluginId == "port" && (offered == sdk.PortCheckCapability || offered == sdk.PortAutoCapability)) || (status.PluginId == "browser" && offered == sdk.BrowserLaunchModeCapability) {
						available[offered] = true
					}
				}
			}
		}
	}
	result := make([]string, 0, len(available))
	for capability := range available {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

// Normal worker Stop deliberately leaves independent browser runtime services
// alive. Only a durable explicit disable invokes this typed stop operation.
func stopDisabledBrowser(ctx context.Context, id string, lifecycle sdk.Lifecycle) error {
	if id != "browser" {
		return nil
	}
	extensions, ok := lifecycle.(sdk.ExtensionLifecycle)
	var client sdk.BrowserClient
	if ok {
		client = extensions.Browser()
	}
	if client == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "BROWSER_DISABLE_SERVICE_UNAVAILABLE")
	}
	profiles, err := client.ListBrowserProfiles(ctx, &epb.BrowserScope{})
	if err != nil {
		return err
	}
	if profiles == nil {
		return connect.NewError(connect.CodeDataLoss, "BROWSER_DISABLE_PROFILES_UNAVAILABLE")
	}
	var failures error
	for _, profile := range profiles.GetProfiles() {
		_, err := client.StopBrowser(ctx, &epb.BrowserActionRequest{ProfileId: profile.Id, RequestId: "disable_" + rand.Text(), Confirm: true})
		failures = errors.Join(failures, err)
	}
	return failures
}
