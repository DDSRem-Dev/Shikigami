package ui

import (
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestReadOnlyPermissionsHideAndRejectPrivilegedDashboardActions(t *testing.T) {
	m := New(t.Context(), nil, Options{ServerID: "server", ProjectID: "proj_test", NoColor: true})
	defer m.Close()
	m.width = 300
	m.height = 30
	m.sessions = []*pb.Session{{Id: "sess_test", Name: "main", ProjectId: "proj_test"}}
	m.agreement = &pb.ProtocolAgreement{DaemonId: "daemon_test", GrantedPermissions: []string{"session.list", "session.attach"}}
	view := m.View()
	for _, label := range []string{"t take control", "n new", "d delete", "r rename", "e edit"} {
		if strings.Contains(view, label) {
			t.Fatal("read-only credential shows forbidden action", label, view)
		}
	}
	for _, key := range []rune{'t', 'n', 'd', 'r', 'e'} {
		_, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if command != nil || m.dialog != nil || m.action.Session != nil {
			t.Fatal("hidden privileged key still executes", key)
		}
	}
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || m.action.Session == nil || !m.action.ReadOnly || m.action.TakeControl {
		t.Fatal("read-only Enter did not explicitly observe")
	}
	m.action = Action{}
	m.agreement = &pb.ProtocolAgreement{GrantedPermissions: []string{"*", "session.control", "session.write"}}
	if m.allowed("session.control", "session.write") {
		t.Fatal("legacy/unbound permission hints trusted")
	}
	_, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || m.action.Session == nil || !m.action.ReadOnly || m.action.TakeControl {
		t.Fatal("unknown legacy permissions must allow only a read-only request")
	}
}
func TestChangedGrantsPreventSubmittingPreviouslyOpenedForm(t *testing.T) {
	m := New(t.Context(), nil, Options{ServerID: "server", ProjectID: "proj_test"})
	defer m.Close()
	m.agreement = testAgreement()
	m.beginCreate()
	m.dialog.fields[0].value = "new"
	m.dialog.index = 4
	m.agreement = &pb.ProtocolAgreement{DaemonId: "daemon_test", GrantedPermissions: []string{"session.attach"}}
	if command := m.submitDialog(); command != nil || m.dialog.err == nil {
		t.Fatal("stale create form ignored updated credential grants")
	}
}
