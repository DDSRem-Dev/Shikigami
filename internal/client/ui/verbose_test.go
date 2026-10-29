//go:build darwin || linux

package ui

import (
	"strings"
	"testing"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestVerbosePanelReadsFiniteScopedHostLogs(t *testing.T) {
	c, p, _ := dashboardClient(t)
	p.logRequests = make(chan *apb.PluginLogsRequest, 4)
	m := New(t.Context(), c, Options{Verbose: true})
	defer m.Close()
	panel := m.loadPanel(t.Context(), 1, 3, "server", "")().(panelMsg)
	if panel.err != nil {
		t.Fatal(panel.err)
	}
	rows := strings.Join(panel.rows, "\n")
	if strings.Count(rows, "123 [warn] measured-plugin-record") != 2 {
		t.Fatal("panel failed to render actual host logs", rows)
	}
	for _, side := range []apb.PluginTarget_Side{apb.PluginTarget_LOCAL, apb.PluginTarget_REMOTE} {
		r := <-p.logRequests
		if r.Target.Side != side || r.Request.PluginId != "sync" || r.Request.Follow || r.Request.Tail != 10 {
			t.Fatal("wrong diagnostic request", r)
		}
		if side == apb.PluginTarget_REMOTE && r.Target.ServerId != "server" {
			t.Fatal("remote logs lost server scope")
		}
	}
	m.opts.Verbose = false
	normal := m.loadPanel(t.Context(), 1, 3, "server", "")().(panelMsg)
	if strings.Contains(strings.Join(normal.rows, "\n"), "measured-plugin-record") || len(p.logRequests) != 0 {
		t.Fatal("ordinary panel unexpectedly fetched logs")
	}
}
