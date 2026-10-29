//go:build darwin || linux

package ui

import (
	"strings"
	"testing"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestConnectionAttemptsDashboardUsesLatestDiagnosticWithoutPathInference(t *testing.T) {
	for _, routes := range [][]string{{"tsnet", "direct-tls"}, nil, {"private-route"}} {
		c, p, _ := dashboardClient(t)
		p.serverRecord.Store(&apb.ServerRecord{Config: &apb.ServerConfig{Id: "server", Name: "test-server"}, ConnectionPath: "unknown", ConnectionAttempts: routes})
		m := New(t.Context(), c, Options{})
		panel := m.loadPanel(t.Context(), 1, 4, "server", "")().(panelMsg)
		m.Close()
		if panel.err != nil {
			t.Fatal(panel.err)
		}
		rows := strings.Join(panel.rows, "\n")
		want := "Latest route selection: not reported"
		if len(routes) == 2 {
			want = "Latest route selection: tsnet → direct-tls"
		}
		if !strings.Contains(rows, want) || !strings.Contains(rows, "Network path: unknown") || strings.Contains(rows, "private-route") {
			t.Fatal(rows)
		}
	}
}
