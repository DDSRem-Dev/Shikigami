//go:build darwin || linux

package ui

import (
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestDashboardSessionActionsUseExplicitScopesAndConfirmation(t *testing.T) {
	c, _, remote := dashboardClient(t)
	m := New(t.Context(), c, Options{ServerID: "server"})
	defer m.Close()
	m.agreement = testAgreement()
	projects := m.beginCreate()
	if projects == nil {
		t.Fatal("create did not load project choice")
	}
	m.Update(projects())
	m.dialogKey(tea.KeyMsg{Type: tea.KeyDown})
	m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.dialog.project != "proj_two" {
		t.Fatal("project selection lost")
	}
	m.dialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("created")})
	for range 4 {
		m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.dialogKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.dialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("false")})
	command := m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("create not submitted")
	}
	m.Update(command())
	create := (<-remote.mutations).(*pb.CreateSessionRequest)
	if create.ProjectId != "proj_two" || create.Name != "created" || create.WorkingDir != "." || create.GetAutoStartClaude() || create.RequestId == "" {
		t.Fatal("wrong create semantics", create)
	}
	m.sessions = []*pb.Session{{Id: "sess_existing", ProjectId: "proj_one", Name: "original", Description: "old", Tags: []string{"old"}}}
	m.cursor = 0
	m.beginEdit("rename")
	m.dialogKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.dialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("renamed")})
	command = m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	update := (<-remote.mutations).(*pb.UpdateSessionRequest)
	if update.Id != "sess_existing" || update.Name != "renamed" || len(update.UpdateMask.Paths) != 1 || update.UpdateMask.Paths[0] != "name" {
		t.Fatal("rename did not preserve identity", update)
	}
	m.beginEdit("edit")
	m.dialog.fields[0].value = "changed"
	m.dialog.fields[1].value = "web, test"
	m.dialog.fields[2].value = `{"role":"frontend"}`
	m.dialog.index = 2
	command = m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	update = (<-remote.mutations).(*pb.UpdateSessionRequest)
	if update.Id != "sess_existing" || update.Description != "changed" || len(update.Tags) != 2 || update.Metadata["role"] != "frontend" {
		t.Fatal("edit fields lost", update)
	}
	m.beginEdit("delete")
	if command := m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter}); command != nil || m.dialog.err == nil {
		t.Fatal("delete did not require confirmation")
	}
	select {
	case value := <-remote.mutations:
		t.Fatal("unconfirmed mutation sent", value)
	default:
	}
	m.dialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("original")})
	command = m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	deleted := (<-remote.mutations).(*pb.DeleteSessionRequest)
	if deleted.Id != "sess_existing" {
		t.Fatal("delete selected wrong session", deleted)
	}
}

func TestDashboardCanceledAndInvalidFormsDoNotMutate(t *testing.T) {
	m := New(t.Context(), nil, Options{ServerID: "server", ProjectID: "proj_one"})
	defer m.Close()
	m.agreement = testAgreement()
	m.beginCreate()
	m.dialogKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.dialog != nil {
		t.Fatal("cancel retained form")
	}
	m.sessions = []*pb.Session{{Id: "sess_one", ProjectId: "proj_one", Name: "one"}}
	m.beginEdit("edit")
	m.dialog.fields[2].value = `{"secret":42}`
	m.dialog.index = 2
	if command := m.submitDialog(); command != nil || m.dialog.err == nil {
		t.Fatal("invalid metadata reached IPC")
	}
}
