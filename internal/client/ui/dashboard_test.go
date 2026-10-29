package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEmptyNavigationStaleRepliesAndNarrowNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New(t.Context(), nil, Options{})
	defer m.Close()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyTab}} {
		m.Update(key)
	}
	if m.action.Session != nil {
		t.Fatal("empty list attached")
	}
	m.generation = 5
	m.pending = 1
	m.Update(panelMsg{generation: 4, index: 0, sessions: []*pb.Session{{Id: "stale"}}, rows: []string{"stale"}})
	if len(m.sessions) != 0 || m.pending != 1 {
		t.Fatal("stale reply changed current selection")
	}
	m.Update(panelMsg{generation: 5, index: 0, err: errors.New("denied\x1b[2J")})
	if m.panels[0].err == nil || m.pending != 0 {
		t.Fatal("panel failure not retained")
	}
	m.tab = 0
	for _, width := range []int{1, 5, 40, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
		view := m.View()
		if strings.Contains(view, "\x1b") {
			t.Fatal("NO_COLOR/remote escape handling failed", view)
		}
		if len(strings.Split(view, "\n")) > 8 {
			t.Fatal("narrow view overflowed height")
		}
	}
}

func TestSwitchCancelsOldQueryAndAttachmentCarriesScope(t *testing.T) {
	m := New(t.Context(), nil, Options{})
	defer m.Close()
	ctx, cancel := context.WithCancel(t.Context())
	m.queryCancel = cancel
	m.servers = []*apb.ServerRecord{{Config: &apb.ServerConfig{Id: "server_a"}, RemoteProtocol: testAgreement()}, {Config: &apb.ServerConfig{Id: "server_b"}, RemoteProtocol: testAgreement()}}
	m.serverID = "server_a"
	m.projectID = "proj_old"
	m.switchServer(1)
	if ctx.Err() == nil || m.serverID != "server_b" || m.projectID != "" {
		t.Fatal("server switch did not cancel and clear scope")
	}
	m.sessions = []*pb.Session{{Id: "sess_new", ProjectId: "proj_new"}}
	m.tab = 0
	m.cursor = 0
	model, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if model != m || command == nil || m.action.ServerID != "server_b" || m.action.Session.Id != "sess_new" || !m.action.ReadOnly {
		t.Fatal("attachment action lost selected scope")
	}
}

func TestScreenPreviewRejectsOldServerSelectionAndGeneration(t *testing.T) {
	m := New(t.Context(), nil, Options{ServerID: "server_new"})
	defer m.Close()
	m.generation = 4
	m.sessions = []*pb.Session{{Id: "sess_selected"}}
	for _, message := range []screenMsg{
		{generation: 3, server: "server_new", session: "sess_selected", screen: &pb.SessionScreen{Text: "old generation"}},
		{generation: 4, server: "server_old", session: "sess_selected", screen: &pb.SessionScreen{Text: "old server"}},
		{generation: 4, server: "server_new", session: "sess_other", screen: &pb.SessionScreen{Text: "old session"}},
	} {
		m.Update(message)
		if m.screen != nil {
			t.Fatal("stale screen crossed scope", m.screen)
		}
	}
	m.Update(screenMsg{generation: 4, server: "server_new", session: "sess_selected", screen: &pb.SessionScreen{SessionId: "sess_selected", Text: "current"}})
	if m.screen == nil || m.screen.Text != "current" {
		t.Fatal("current screen discarded")
	}
}

func TestNarrowFormKeepsFocusedFieldVisible(t *testing.T) {
	m := New(t.Context(), nil, Options{ServerID: "server", ProjectID: "project"})
	defer m.Close()
	m.width = 80
	m.height = 7
	m.agreement = testAgreement()
	m.beginCreate()
	m.dialog.index = 4
	if !strings.Contains(m.View(), "Start Claude") {
		t.Fatal("narrow form hid its focused field", m.View())
	}
}

func testAgreement() *pb.ProtocolAgreement {
	return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_test", Capabilities: []string{"terminal.v1", "terminal.screen.v1", "stats.v1"}, GrantedPermissions: []string{"project.list", "session.list", "session.create", "session.update", "session.delete", "session.attach", "session.write", "session.control", "system.stats"}}
}

func TestImplicitServerRemovalClearsDialogScreenAndStaleGeneration(t *testing.T) {
	for _, remaining := range [][]*apb.ServerRecord{nil, {{Config: &apb.ServerConfig{Id: "server_new"}}}} {
		m := New(t.Context(), nil, Options{ServerID: "server_old", ProjectID: "project_old"})
		m.generation = 7
		m.pending = 1
		m.sessions = []*pb.Session{{Id: "session_old"}}
		m.dialog = &sessionDialog{id: "old_dialog", server: "server_old"}
		m.screen = &pb.SessionScreen{SessionId: "session_old", Text: "old screen"}
		query, cancel := context.WithCancel(t.Context())
		m.queryCancel = cancel
		m.Update(serversMsg{servers: remaining})
		if query.Err() == nil || m.dialog != nil || m.screen != nil || m.projectID != "" || m.generation <= 7 {
			t.Fatal("removed server left old scoped UI state")
		}
		m.Update(panelMsg{generation: 7, index: 0, sessions: []*pb.Session{{Id: "late_old_session"}}})
		if len(m.sessions) != 0 || m.pending != 0 {
			t.Fatal("old panel result crossed the implicit server switch")
		}
		m.Close()
	}
}
