package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// clean excludes terminal controls from remote names, paths and diagnostics.
func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x7f {
			return ' '
		}
		return r
	}, value)
}

func connectionPath(value string) string {
	switch value {
	case "tailscale-direct", "tailscale-relay", "direct-tls":
		return value
	default:
		return "unknown"
	}
}
func (m *Model) View() string {
	width := max(1, m.width-2)
	clip := func(value string) string { return ansi.Truncate(clean(value), width, "…") }
	var rows []string
	title := "Shikigami · " + m.serverID
	if len(m.servers) > m.serverIndex {
		server := m.servers[m.serverIndex]
		title = "Shikigami · " + server.GetConfig().GetName() + " · " + server.ConnectionState + " · " + connectionPath(server.ConnectionPath) + " · " + server.ReasonCode
	}
	if m.serverID == "" {
		title = "Shikigami · no server selected"
	}
	if m.projectID != "" {
		title += " · " + m.projectID
	}
	title = clip(title)
	if !m.opts.NoColor && os.Getenv("NO_COLOR") == "" && m.dialog == nil {
		title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "81"}).Render(title)
	}
	rows = append(rows, title)
	if m.serverIndex < len(m.servers) {
		if recovery := client.ConnectionRecovery(m.servers[m.serverIndex]); recovery != "" {
			rows = append(rows, clip(recovery))
		}
	}
	if m.dialog != nil {
		return m.dialogView(rows, clip)
	}
	tabs := []string{"Sessions", "Sync & conflicts", "Ports", "Plugins", "Node"}
	for i := range tabs {
		if i == m.tab {
			tabs[i] = "[" + tabs[i] + "]"
		}
	}
	rows = append(rows, clip(strings.Join(tabs, "  ")), "")
	panel := m.panels[m.tab]
	if panel.err != nil {
		rows = append(rows, clip("Unavailable: "+panel.err.Error()))
	}
	if panel.loading {
		rows = append(rows, "Refreshing…")
	}
	if !m.streamOpen {
		rows = append(rows, clip("Connecting to agent event stream…"))
	}
	if m.eventError != nil {
		rows = append(rows, clip("Events: "+m.eventError.Error()))
	}
	if m.status != "" {
		rows = append(rows, clip(m.status))
	}
	if m.agreement.GetDaemonId() == "" {
		rows = append(rows, clip("Permissions unavailable; privileged actions hidden."))
	} else if m.allowed("session.attach") && !m.allowed("session.write") {
		rows = append(rows, "Read-only credential: terminal input and takeover are unavailable.")
	}
	details := []string{}
	if m.tab == 0 && m.cursor < len(m.sessions) {
		session := m.sessions[m.cursor]
		metadata, _ := json.Marshal(session.Metadata)
		details = []string{
			fmt.Sprintf("%s · project %s · %s", session.Id, session.ProjectId, session.WorkingDir),
			fmt.Sprintf("Created %s · last active %s", time.Unix(session.CreatedAt, 0).Format(time.RFC3339), time.Unix(session.LastActive, 0).Format(time.RFC3339)),
			"Description: " + session.Description,
			"Tags: " + strings.Join(session.Tags, ", ") + " · metadata: " + string(metadata),
		}
		if notice := client.CleanupNotice(session, time.Now()); notice != "" {
			details = append([]string{notice}, details...)
		}
		if m.screen != nil && m.screen.SessionId == session.Id {
			label := fmt.Sprintf("Current screen · %d×%d · %s", m.screen.Cols, m.screen.Rows, time.Unix(m.screen.CapturedAt, 0).Format("15:04:05"))
			if m.screen.Truncated {
				label += " · truncated"
			}
			details = append(details, label)
			lines := strings.Split(strings.TrimRight(m.screen.Text, "\n \t"), "\n")
			for _, line := range lines[max(0, len(lines)-4):] {
				details = append(details, "  "+line)
			}
		} else if m.screenError != nil {
			details = append(details, "Current screen: "+m.screenError.Error())
		}
	}
	available := max(0, m.height-len(rows)-3-len(details))
	start := max(0, m.cursor-available+1)
	for i := start; i < len(panel.rows) && i < start+available; i++ {
		prefix := "  "
		if i == m.cursor {
			prefix = "› "
		}
		if m.tab == 0 && i < len(m.sessions) && m.sessions[i].CleanupStatus == "warned" {
			prefix += "[cleanup warning] "
		}
		rows = append(rows, clip(prefix+panel.rows[i]))
	}
	for _, detail := range details {
		rows = append(rows, clip(detail))
	}
	primary := "↑↓ select · ←→ server · Tab panel"
	if m.canObserve() {
		if m.allowed("session.write") {
			primary += " · Enter attach"
		} else {
			primary += " · Enter observe"
		}
		primary += " · o observe"
	}
	if m.allowed("session.attach", "session.control", "session.write") {
		primary += " · t take control"
	}
	primary += " · q quit"
	secondary := []string{}
	if m.canCreate() {
		secondary = append(secondary, "n new")
	}
	if m.allowed("session.delete") {
		secondary = append(secondary, "d delete")
	}
	if m.allowed("session.update") {
		secondary = append(secondary, "r rename", "e edit")
	}
	secondary = append(secondary, "F5 refresh")
	rows = append(rows, "", clip(primary), clip(strings.Join(secondary, " · ")))
	if len(rows) > m.height {
		rows = rows[:m.height]
	}
	return strings.Join(rows, "\n")
}

func (m *Model) dialogView(rows []string, clip func(string) string) string {
	d := m.dialog
	rows = append(rows, clip("Session "+d.mode+" · server "+d.server+" · project "+d.project))
	if d.session != nil {
		rows = append(rows, clip(d.session.Name+" · "+d.session.Id))
	}
	if d.mode == "delete" {
		rows = append(rows, "Project files are retained. The selected tmux session will end.")
	}
	if d.choosing {
		rows = append(rows, "Choose a project:")
		available := max(1, m.height-len(rows)-4)
		start := max(0, d.index-available+1)
		for i := start; i < len(d.projects) && i < start+available; i++ {
			prefix := "  "
			if i == d.index {
				prefix = "› "
			}
			rows = append(rows, clip(prefix+d.projects[i].Name+" · "+d.projects[i].Id))
		}
	} else {
		available := max(1, m.height-len(rows)-4)
		start := max(0, d.index-available+1)
		for i := start; i < len(d.fields) && i < start+available; i++ {
			field := d.fields[i]
			prefix := "  "
			if i == d.index {
				prefix = "› "
			}
			rows = append(rows, clip(prefix+field.label+": "+field.value))
		}
	}
	if d.busy {
		rows = append(rows, "Waiting for the agent…")
	}
	if d.err != nil {
		rows = append(rows, clip("Error: "+d.err.Error()))
	}
	rows = append(rows, "", clip("Tab/↑↓ field · Ctrl+U clear · Enter next/save · Esc cancel"))
	for i := range rows {
		rows[i] = clip(rows[i])
	}
	if len(rows) > m.height {
		rows = rows[:m.height]
	}
	return strings.Join(rows, "\n")
}
