package ui

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type formField struct{ label, value string }
type sessionDialog struct {
	id, mode, server, project, requestID string
	session                              *pb.Session
	fields                               []formField
	index                                int
	projects                             []*pb.Project
	choosing, busy                       bool
	err                                  error
}
type projectsMsg struct {
	id       string
	projects []*pb.Project
	err      error
}
type mutationMsg struct {
	id      string
	session *pb.Session
	err     error
}

func (m *Model) beginCreate() tea.Cmd {
	if !m.canCreate() {
		m.status = "Session creation is unavailable for the current permissions/project selection."
		return nil
	}
	if m.serverID == "" {
		m.status = "Configure a server before creating a session."
		return nil
	}
	d := &sessionDialog{id: rand.Text(), mode: "create", server: m.serverID, project: m.projectID}
	m.dialog = d
	if d.project != "" {
		d.creationFields()
		return nil
	}
	d.choosing = true
	d.busy = true
	id, server, ctx, c := d.id, d.server, m.ctx, m.client
	return func() tea.Msg {
		list, err := c.Remote(server).ListProjects(ctx, &pb.ListProjectsRequest{})
		return projectsMsg{id: id, projects: list.GetProjects(), err: err}
	}
}
func (d *sessionDialog) creationFields() {
	d.choosing = false
	d.busy = false
	d.index = 0
	d.fields = []formField{{"Session name", ""}, {"Working directory (relative to project)", "."}, {"Description", ""}, {"Tags (comma separated)", ""}, {"Start Claude (true or false)", "true"}}
}
func (m *Model) acceptProjects(message projectsMsg) tea.Cmd {
	d := m.dialog
	if d == nil || d.id != message.id {
		return nil
	}
	d.busy = false
	d.projects = message.projects
	d.err = message.err
	if message.err == nil && len(message.projects) == 0 {
		d.err = errors.New("no projects; create one with shiki projects create")
	}
	return nil
}
func (m *Model) beginEdit(mode string) tea.Cmd {
	permission := "session.update"
	if mode == "delete" {
		permission = "session.delete"
	}
	if !m.allowed(permission) {
		m.status = "This session action is not granted by the current credential."
		return nil
	}
	if len(m.sessions) == 0 || m.cursor >= len(m.sessions) {
		m.status = "Select a session first."
		return nil
	}
	value := proto.Clone(m.sessions[m.cursor]).(*pb.Session)
	d := &sessionDialog{id: rand.Text(), mode: mode, server: m.serverID, project: value.ProjectId, session: value}
	switch mode {
	case "delete":
		d.fields = []formField{{"Type the session name to delete its management record and tmux process", ""}}
	case "rename":
		d.fields = []formField{{"New session name", value.Name}}
	case "edit":
		metadata, _ := json.Marshal(value.Metadata)
		if value.Metadata == nil {
			metadata = []byte("{}")
		}
		d.fields = []formField{{"Description", value.Description}, {"Tags (comma separated)", strings.Join(value.Tags, ",")}, {"Metadata (JSON string values)", string(metadata)}}
	}
	m.dialog = d
	return nil
}
func (m *Model) dialogKey(key tea.KeyMsg) tea.Cmd {
	d := m.dialog
	if key.String() == "ctrl+c" {
		return tea.Quit
	}
	if d.busy {
		return nil
	}
	if key.String() == "esc" {
		m.dialog = nil
		return nil
	}
	if d.choosing {
		switch key.String() {
		case "up", "k":
			d.index = max(0, d.index-1)
		case "down", "j":
			d.index = min(max(0, len(d.projects)-1), d.index+1)
		case "enter":
			if len(d.projects) > 0 {
				d.project = d.projects[d.index].Id
				d.creationFields()
			}
		}
		return nil
	}
	if len(d.fields) == 0 {
		return nil
	}
	switch key.String() {
	case "tab", "down":
		d.index = (d.index + 1) % len(d.fields)
	case "shift+tab", "up":
		d.index = (d.index + len(d.fields) - 1) % len(d.fields)
	case "enter":
		if d.index < len(d.fields)-1 {
			d.index++
			return nil
		}
		return m.submitDialog()
	case "backspace", "ctrl+h":
		runes := []rune(d.fields[d.index].value)
		if len(runes) > 0 {
			d.fields[d.index].value = string(runes[:len(runes)-1])
			d.requestID = ""
		}
	case "ctrl+u":
		d.fields[d.index].value = ""
		d.requestID = ""
	default:
		if key.Type == tea.KeyRunes {
			text := strings.Map(func(r rune) rune {
				if r < ' ' || r == 127 {
					return -1
				}
				return r
			}, string(key.Runes))
			if len(d.fields[d.index].value)+len(text) <= 4096 {
				d.fields[d.index].value += text
				d.requestID = ""
			}
		}
	}
	return nil
}
func splitTags(text string) []string {
	var tags []string
	for _, tag := range strings.Split(text, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
func (m *Model) submitDialog() tea.Cmd {
	d := m.dialog
	permission := "session.update"
	if d.mode == "create" {
		permission = "session.create"
	}
	if d.mode == "delete" {
		permission = "session.delete"
	}
	if !m.allowed(permission) {
		d.err = errors.New("current credential no longer grants this action")
		return nil
	}
	var create *pb.CreateSessionRequest
	var update *pb.UpdateSessionRequest
	switch d.mode {
	case "create":
		if strings.TrimSpace(d.fields[0].value) == "" {
			d.err = errors.New("session name is required")
			return nil
		}
		startText := strings.TrimSpace(d.fields[4].value)
		if startText != "true" && startText != "false" {
			d.err = errors.New("Start Claude must be true or false")
			return nil
		}
		start := startText == "true"
		if d.requestID == "" {
			d.requestID = rand.Text()
		}
		create = &pb.CreateSessionRequest{ProjectId: d.project, Name: d.fields[0].value, WorkingDir: d.fields[1].value, Description: d.fields[2].value, Tags: splitTags(d.fields[3].value), AutoStartClaude: &start, RequestId: d.requestID}
	case "delete":
		if d.fields[0].value != d.session.Name {
			d.err = errors.New("the entered name does not match; no deletion was sent")
			return nil
		}
	case "rename":
		update = &pb.UpdateSessionRequest{Id: d.session.Id, Name: d.fields[0].value, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}}}
	case "edit":
		metadata := map[string]string{}
		if err := json.Unmarshal([]byte(d.fields[2].value), &metadata); err != nil {
			d.err = errors.New("metadata must be a JSON object with string values")
			return nil
		}
		update = &pb.UpdateSessionRequest{Id: d.session.Id, Description: d.fields[0].value, Tags: splitTags(d.fields[1].value), Metadata: metadata, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description", "tags", "metadata"}}}
	}
	d.busy = true
	d.err = nil
	id, mode, server, sessionID := d.id, d.mode, d.server, d.session.GetId()
	c, parent := m.client, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		remote := c.Remote(server)
		var session *pb.Session
		var err error
		switch mode {
		case "create":
			session, err = remote.CreateSession(ctx, create)
		case "delete":
			_, err = remote.DeleteSession(ctx, &pb.DeleteSessionRequest{Id: sessionID})
		default:
			session, err = remote.UpdateSession(ctx, update)
		}
		return mutationMsg{id: id, session: session, err: err}
	}
}
func (m *Model) acceptMutation(message mutationMsg) tea.Cmd {
	d := m.dialog
	if d == nil || d.id != message.id {
		return nil
	}
	d.busy = false
	if message.err != nil {
		d.err = message.err
		return nil
	}
	m.status = fmt.Sprintf("Session %s completed.", d.mode)
	m.dialog = nil
	if m.queryCancel != nil {
		m.queryCancel()
	}
	m.pending = 0
	return m.refresh()
}
