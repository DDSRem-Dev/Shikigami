package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
)

func (a *app) doctorCommand() *cobra.Command {
	return &cobra.Command{Use: "doctor", Args: cobra.NoArgs, Short: "Inspect agent readiness and authenticated server connectivity", RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), false, func(c *client.Client) error {
			status, e := c.Agent().GetStatus(cmd.Context(), &pb.Empty{})
			if e != nil {
				return e
			}
			servers, e := c.Agent().ListServers(cmd.Context(), &pb.Empty{})
			type report struct {
				Build         buildinfo.Information   `json:"build"`
				Agent         *apb.AgentStatus        `json:"agent"`
				Servers       *apb.ServerList         `json:"servers"`
				Remote        *pb.SystemInfo          `json:"remote,omitempty"`
				RemoteError   string                  `json:"remote_error,omitempty"`
				Node          *apb.NodeStatus         `json:"node,omitempty"`
				NodeError     string                  `json:"node_error,omitempty"`
				Health        *pb.HealthStatus        `json:"health,omitempty"`
				Claude        *pb.ClaudeVersion       `json:"claude,omitempty"`
				Resources     *pb.StatsSnapshot       `json:"resources,omitempty"`
				LocalPlugins  *pb.ListPluginsResponse `json:"local_plugins,omitempty"`
				RemotePlugins *pb.ListPluginsResponse `json:"remote_plugins,omitempty"`
				Errors        map[string]string       `json:"errors,omitempty"`
				Cleanup       map[string]string       `json:"cleanup,omitempty"`
				Recovery      map[string]string       `json:"recovery,omitempty"`
			}
			result := report{Build: buildinfo.Current(), Agent: status, Servers: servers, Errors: map[string]string{}}
			if e != nil {
				// Degraded resources must not prevent independent node diagnostics.
				result.Errors["servers"] = e.Error()
				servers = nil
				result.Servers = nil
			}
			result.LocalPlugins, e = c.Agent().ListPlugins(cmd.Context(), &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
			if e != nil {
				result.Errors["local_plugins"] = e.Error()
			}
			node, nodeErr := client.GetNodeStatus(cmd.Context(), c.Config.Agent.Socket, a.timeout)
			if nodeErr != nil {
				result.NodeError = nodeErr.Error()
			} else {
				result.Node = node
				if result.Node != nil {
					result.Node.LoginUrl = ""
				}
			}
			if len(servers.GetServers()) > 0 {
				selected, e := c.ResolveServer(cmd.Context(), a.server)
				if e != nil {
					result.RemoteError = e.Error()
				} else {
					result.Remote, e = c.Remote(selected.Config.Id).GetSystemInfo(cmd.Context(), &pb.Empty{})
					if e != nil {
						result.RemoteError = e.Error()
					}
					if refreshed, err := c.ResolveServer(cmd.Context(), selected.Config.Id); err == nil {
						selected = refreshed
					}
					remote := c.Remote(selected.Config.Id)
					if diagnosticGranted(selected.RemoteProtocol, "system.info") {
						result.Health, e = remote.HealthCheck(cmd.Context(), &pb.Empty{})
						if e != nil {
							result.Errors["health"] = e.Error()
						}
						result.Claude, e = remote.GetClaudeVersion(cmd.Context(), &pb.Empty{})
						if e != nil {
							result.Errors["claude"] = e.Error()
						}
					} else {
						result.Errors["health"] = "system.info permission not granted"
						result.Errors["claude"] = "system.info permission not granted"
					}
					if diagnosticGranted(selected.RemoteProtocol, "session.list") {
						sessions, err := remote.ListSessions(cmd.Context(), &pb.ListSessionsRequest{})
						if err != nil {
							result.Errors["cleanup"] = err.Error()
						} else {
							result.Cleanup = map[string]string{}
							for _, session := range sessions.Sessions {
								if notice := client.CleanupNotice(session, time.Now()); notice != "" {
									result.Cleanup[session.Id] = notice
								}
							}
						}
					} else {
						result.Errors["cleanup"] = "session.list permission not granted"
					}
					capabilities := selected.RemoteProtocol.GetCapabilities()
					if selected.RemoteProtocol == nil {
						capabilities = result.Remote.GetCapabilities()
					}
					if slices.Contains(capabilities, "stats.v1") {
						if diagnosticGranted(selected.RemoteProtocol, "system.stats") {
							result.Resources, e = remote.GetStats(cmd.Context(), &pb.StatsRequest{})
							if e != nil {
								result.Errors["resources"] = e.Error()
							}
						} else {
							result.Errors["resources"] = "system.stats permission not granted"
						}
					}
					if slices.Contains(capabilities, "plugin.manage.v1") {
						if diagnosticGranted(selected.RemoteProtocol, "plugin.manage") {
							result.RemotePlugins, e = c.Agent().ListPlugins(cmd.Context(), &apb.PluginTarget{Side: apb.PluginTarget_REMOTE, ServerId: selected.Config.Id})
							if e != nil {
								result.Errors["remote_plugins"] = e.Error()
							}
						} else {
							result.Errors["remote_plugins"] = "plugin.manage permission not granted"
						}
					}
				}
			}
			// Probes above can establish or replace the route; report the resulting
			// runtime path rather than the pre-probe snapshot.
			if refreshed, err := c.Agent().ListServers(cmd.Context(), &pb.Empty{}); err == nil {
				result.Servers = refreshed
				delete(result.Errors, "servers")
			}
			result.Servers = client.SafeConnectionAttemptList(result.Servers)
			result.Recovery = map[string]string{}
			for _, server := range result.Servers.GetServers() {
				if recovery := client.ConnectionRecovery(server); recovery != "" {
					result.Recovery[server.GetConfig().GetId()] = recovery
				}
			}
			if a.json {
				return a.printJSON(result)
			}
			fmt.Fprintf(a.output, "Agent: %s (%s)\n", status.State, status.InstanceId)
			if err := result.Errors["servers"]; err != "" {
				fmt.Fprintf(a.output, "Registered servers: unavailable (%s)\n", safeText(err))
			} else {
				fmt.Fprintf(a.output, "Registered servers: %d\n", len(result.Servers.GetServers()))
			}
			for _, server := range result.Servers.GetServers() {
				path := server.ConnectionPath
				if path != "tailscale-direct" && path != "tailscale-relay" && path != "direct-tls" {
					path = "unknown"
				}
				fmt.Fprintf(a.output, "Server %s: %s · path %s · %s\n", safeText(server.GetConfig().GetName()), safeText(server.ConnectionState), path, safeText(server.ReasonCode))
				fmt.Fprintln(a.output, client.ConnectionAttemptsNotice(server))
				if recovery := result.Recovery[server.GetConfig().GetId()]; recovery != "" {
					fmt.Fprintln(a.output, recovery)
				}
			}
			if result.Node != nil {
				fmt.Fprintf(a.output, "Embedded node: %s · %s\n", safeText(result.Node.State), safeText(result.Node.ReasonCode))
			}
			if result.NodeError != "" {
				fmt.Fprintln(a.output, "Node control:", safeText(result.NodeError))
			}
			if result.Remote != nil {
				fmt.Fprintf(a.output, "Remote: %s/%s · daemon %s · %s\n", result.Remote.Os, result.Remote.Arch, result.Remote.DaemonVersion, result.Remote.DaemonId)
			}
			if result.RemoteError != "" {
				fmt.Fprintln(a.output, "Remote system information:", safeText(result.RemoteError))
			}
			if result.Health != nil {
				fmt.Fprintf(a.output, "Remote health: %s · %s\n", result.Health.Status.String(), safeText(result.Health.Message))
			}
			if result.Claude != nil {
				fmt.Fprintf(a.output, "Remote Claude: %s · %s\n", safeText(result.Claude.Version), safeText(result.Claude.Path))
			}
			for _, item := range []struct {
				name    string
				plugins *pb.ListPluginsResponse
			}{{"Local", result.LocalPlugins}, {"Remote", result.RemotePlugins}} {
				for _, plugin := range item.plugins.GetPlugins() {
					fmt.Fprintf(a.output, "%s plugin %s: %s · %s\n", item.name, safeText(plugin.PluginId), safeText(plugin.State), safeText(plugin.ReasonCode))
				}
			}
			if result.Resources != nil {
				if err := a.printStats(result.Resources, false); err != nil {
					return err
				}
			}
			for _, name := range []string{"local_plugins", "health", "claude", "resources", "remote_plugins", "cleanup"} {
				if reason := result.Errors[name]; reason != "" {
					fmt.Fprintf(a.output, "%s: %s\n", name, safeText(reason))
				}
			}
			for id, notice := range result.Cleanup {
				fmt.Fprintf(a.output, "Session %s: %s\n", safeText(id), notice)
			}
			fmt.Fprintf(a.output, "Active terminals: %d · sync tasks: %d · forwards: %d\n", status.ActiveTerminals, status.ActiveSyncTasks, status.ActiveForwards)
			return nil
		})
	}}
}

func diagnosticGranted(agreement *pb.ProtocolAgreement, permission string) bool {
	// Unknown legacy metadata defers to the server; known effective grants never
	// turn an unrelated denied diagnostic into a blocker for another permission.
	return agreement.GetDaemonId() == "" || slices.Contains(agreement.GrantedPermissions, permission)
}
