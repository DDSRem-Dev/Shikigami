package ui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) loadPanel(ctx context.Context, generation uint64, index int, server, project string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		result := panelMsg{generation: generation, index: index}
		switch index {
		case 0:
			if server == "" {
				result.rows = []string{"No servers configured. Use shiki connect."}
				return result
			}
			list, err := c.Remote(server).ListSessions(ctx, &pb.ListSessionsRequest{})
			record, recordErr := c.ResolveServer(ctx, server)
			if recordErr == nil {
				result.agreement = record.RemoteProtocol
			}
			if err != nil {
				result.err = err
			}
			for _, session := range list.GetSessions() {
				if project != "" && session.ProjectId != project {
					continue
				}
				result.sessions = append(result.sessions, session)
				result.rows = append(result.rows, fmt.Sprintf("%s · project %s · Claude %s · %d attachments", session.Name, session.ProjectId, session.ClaudeStatus, session.ClientCount))
			}
			if err == nil && len(result.sessions) == 0 {
				result.rows = append(result.rows, "No sessions. Create one with shiki sessions create --project <id>.")
			}
			result.screenSupported = slices.Contains(result.agreement.GetCapabilities(), "terminal.screen.v1")
			if !slices.Contains(result.agreement.GetCapabilities(), "stats.v1") {
				result.rows = append(result.rows, "Resources: unavailable (server does not support stats.v1)")
				return result
			}
			if !granted(result.agreement, "system.stats") {
				result.rows = append(result.rows, "Resources: unavailable (system.stats permission not granted)")
				return result
			}
			stats, err := c.Remote(server).GetStats(ctx, &pb.StatsRequest{})
			if err != nil {
				result.rows = append(result.rows, "Resources: "+err.Error())
				return result
			}
			for i, session := range result.sessions {
				for _, stat := range stats.Sessions {
					if stat.SessionId == session.Id {
						result.rows[i] += fmt.Sprintf("  ·  %s CPU / %s RSS", cpu(stat.CpuPercent), memory(stat.MemoryBytes))
					}
				}
			}
			result.rows = append(result.rows, fmt.Sprintf("System: %s CPU · %s memory · %s minimum disk free", cpu(stats.CpuPercent), memory(stats.MemoryUsedBytes), memory(stats.DiskFreeBytes)), "CPU: one core = 100%; RSS includes the verified process tree.")
		case 1:
			list, err := c.Agent().ListMappings(ctx, &apb.ListMappingsRequest{ServerId: server})
			if err != nil {
				result.err = err
				return result
			}
			for _, mapping := range list.Mappings {
				if project != "" && mapping.ProjectId != project {
					continue
				}
				result.rows = append(result.rows, fmt.Sprintf("%s · %s · sync enabled: %t", mapping.Id, mapping.LocalRoot, mapping.SyncEnabled))
				scope := &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}
				state, err := c.Agent().GetSyncState(ctx, scope)
				if err != nil {
					result.rows = append(result.rows, "  State: "+err.Error())
				} else {
					result.rows = append(result.rows, fmt.Sprintf("  %s · pending %d · conflicts %d · %s", state.State, state.PendingFiles, state.ConflictCount, state.ReasonCode))
				}
				conflicts, err := c.Agent().ListConflicts(ctx, scope)
				if err != nil {
					result.rows = append(result.rows, "  Conflicts: "+err.Error())
					continue
				}
				for _, conflict := range conflicts.Conflicts {
					result.conflicts = append(result.conflicts, conflict)
					kind := "conflict"
					if conflict.Base == nil {
						kind = "initial sync choice required"
					}
					result.rows = append(result.rows, fmt.Sprintf("  %s: %s · id %s · generation %s", kind, conflict.Path, conflict.Id, conflict.Generation))
				}
			}
			if len(list.Mappings) == 0 {
				result.rows = []string{"No directory mappings."}
			}
			result.rows = append(result.rows, "Review with shiki sync show-diff; resolve with an explicit generation and choice.")
		case 2:
			list, err := c.Agent().ListForwards(ctx, &pb.Empty{})
			if err != nil {
				result.err = err
				return result
			}
			for _, forward := range list.Forwards {
				if server != "" && forward.GetSpec().GetServerId() != server {
					continue
				}
				bind := "not listening"
				if forward.State == "listening" && forward.BoundPort > 0 {
					bind = fmt.Sprintf("%s:%d", forward.GetSpec().GetLocalAddress(), forward.BoundPort)
				}
				result.rows = append(result.rows, fmt.Sprintf("%s → %s:%d · %s · connections %d · bytes %d · %s", bind, forward.GetSpec().GetRemoteAddress(), forward.GetSpec().GetRemotePort(), forward.State, forward.Connections, forward.BytesTransferred, forward.ReasonCode))
			}
			if len(result.rows) == 0 {
				result.rows = []string{"No forwards for this server."}
			}
		case 3:
			for _, target := range []*apb.PluginTarget{{Side: apb.PluginTarget_LOCAL}, {Side: apb.PluginTarget_REMOTE, ServerId: server}} {
				label := "Local"
				if target.Side == apb.PluginTarget_REMOTE {
					label = "Remote"
					if server == "" {
						continue
					}
				}
				list, err := c.Agent().ListPlugins(ctx, target)
				if err != nil {
					result.rows = append(result.rows, label+": "+err.Error())
					continue
				}
				for _, plugin := range list.Plugins {
					result.rows = append(result.rows, fmt.Sprintf("%s %s · enabled %t · %s · %s", label, plugin.PluginId, plugin.Enabled, plugin.State, plugin.ReasonCode))
					if m.opts.Verbose {
						logs, err := c.PluginLogTail(ctx, target, plugin.PluginId, 10)
						if err != nil {
							result.rows = append(result.rows, "  Logs: "+err.Error())
						}
						for _, record := range logs {
							result.rows = append(result.rows, fmt.Sprintf("  %d [%s] %s", record.Timestamp, record.Level, record.Message))
						}
					}
				}
			}
			result.rows = append(result.rows, "Manage with shiki plugins enable|disable|restart --side local|remote.")
		case 4:
			path := "unknown"
			attempts := client.ConnectionAttemptsNotice(nil)
			if server != "" {
				if record, err := c.ResolveServer(ctx, server); err == nil {
					path = connectionPath(record.ConnectionPath)
					attempts = client.ConnectionAttemptsNotice(record)
				}
			}
			result.rows = append(result.rows, "Network path: "+path, attempts, "RTT: unavailable (not measured)")
			status, err := c.Agent().GetStatus(ctx, &pb.Empty{})
			if err != nil {
				result.rows = append(result.rows, "Agent: "+err.Error())
			} else {
				result.rows = append(result.rows, fmt.Sprintf("Agent %s · %s", status.State, status.ReasonCode), fmt.Sprintf("Terminals %d · sync tasks %d · forwards %d", status.ActiveTerminals, status.ActiveSyncTasks, status.ActiveForwards))
			}
			state, err := client.GetNodeStatus(ctx, c.Config.Agent.Socket, 5*time.Second)
			if err != nil {
				result.rows = append(result.rows, "Node: "+err.Error())
				return result
			}
			result.rows = append(result.rows, fmt.Sprintf("Embedded node %s · %s", state.State, state.ReasonCode))
			switch state.ReasonCode {
			case "NODE_DISABLED":
				result.rows = append(result.rows, "Embedded node is disabled. To enable it, update the agent configuration and restart the agent.")
			case "NODE_STATE_UNAVAILABLE":
				result.rows = append(result.rows, "Node identity state is unavailable. Inspect agent logs and repair the state path or permissions, preserving the existing identity, then restart the agent.")
			default:
				if state.State == "needs_login" || state.State == "expired" || state.State == "denied" {
					result.rows = append(result.rows, "Log in through shiki node login.")
				}
			}
			result.rows = append(result.rows, "Dashboard exit keeps the agent running.")
		}
		return result
	}
}
func cpu(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
func memory(value *int64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f MiB", float64(*value)/(1024*1024))
}
