// Package ui owns frontend presentation. All remote resources are read through
// internal/client's authenticated Agent IPC; it owns no background services.
package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

type Options struct {
	Input, Output       *os.File
	ServerID, ProjectID string
	NoColor             bool
	Verbose             bool
}
type Action struct {
	ServerID                         string
	Session                          *pb.Session
	ReadOnly, TakeControl, Reconnect bool
}
type panel struct {
	rows    []string
	err     error
	loading bool
}
type Model struct {
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	queryCancel                             context.CancelFunc
	client                                  *client.Client
	opts                                    Options
	servers                                 []*apb.ServerRecord
	sessions                                []*pb.Session
	conflicts                               []*ppb.Conflict
	panels                                  [5]panel
	serverID, projectID                     string
	serverIndex, tab, cursor, width, height int
	generation                              uint64
	pending                                 int
	dirty                                   bool
	stream                                  arpc.AgentWatchEventsClientStream
	streamOpen                              bool
	instance                                string
	sequence                                uint64
	eventError                              error
	status                                  string
	action                                  Action
	dialog                                  *sessionDialog
	screenSupported                         bool
	screen                                  *pb.SessionScreen
	screenError                             error
	screenCancel                            context.CancelFunc
	agreement                               *pb.ProtocolAgreement
}
type panelMsg struct {
	generation      uint64
	index           int
	rows            []string
	sessions        []*pb.Session
	conflicts       []*ppb.Conflict
	err             error
	screenSupported bool
	agreement       *pb.ProtocolAgreement
}
type serversMsg struct {
	servers []*apb.ServerRecord
	err     error
}
type subscribedMsg struct {
	stream arpc.AgentWatchEventsClientStream
	event  *apb.AgentEvent
	err    error
}
type eventMsg struct {
	event *apb.AgentEvent
	err   error
}
type tickMsg struct{}

func New(ctx context.Context, c *client.Client, opts Options) *Model {
	ctx, cancel := context.WithCancel(ctx)
	return &Model{ctx: ctx, cancel: cancel, client: c, opts: opts, serverID: opts.ServerID, projectID: opts.ProjectID, width: 80, height: 24}
}
func (m *Model) Close() {
	m.cancel()
	if m.queryCancel != nil {
		m.queryCancel()
	}
	if m.screenCancel != nil {
		m.screenCancel()
	}
	if m.streamOpen {
		_ = m.stream.Close()
	}
}

// Run returns only after terminal state is restored and our native input reader
// has been canceled, joined and closed. The caller may then attach, and launch a
// fresh dashboard later.
func Run(ctx context.Context, c *client.Client, opts Options) (action Action, result error) {
	m := New(ctx, c, opts)
	defer m.Close()
	input, err := newDashboardInput(opts.Input)
	if err != nil {
		return Action{}, err
	}
	defer func() { result = errors.Join(result, input.Close()) }()
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(opts.Output), tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return Action{}, err
	}
	model, ok := final.(*Model)
	if !ok {
		return Action{}, errors.New("unexpected dashboard model")
	}
	return model.action, nil
}
func (m *Model) Init() tea.Cmd { return tea.Batch(m.subscribe(), m.loadServers(), tick()) }
func tick() tea.Cmd            { return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }
func (m *Model) subscribe() tea.Cmd {
	return func() tea.Msg {
		stream, err := m.client.Agent().WatchEvents(m.ctx, &pb.Empty{})
		if err != nil {
			return subscribedMsg{err: err}
		}
		event, err := stream.Receive()
		if err != nil {
			stream.Close()
			return subscribedMsg{err: err}
		}
		return subscribedMsg{stream: stream, event: event}
	}
}
func (m *Model) receive() tea.Cmd {
	stream := m.stream
	return func() tea.Msg { event, err := stream.Receive(); return eventMsg{event: event, err: err} }
}
func (m *Model) loadServers() tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.Agent().ListServers(m.ctx, &pb.Empty{})
		return serversMsg{servers: list.GetServers(), err: err}
	}
}

func (m *Model) refresh() tea.Cmd {
	if !m.streamOpen {
		return nil
	} // subscribe before loading snapshots
	if m.pending > 0 {
		m.dirty = true
		return nil
	}
	if m.queryCancel != nil {
		m.queryCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.queryCancel = cancel
	m.generation++
	generation := m.generation
	m.pending = len(m.panels)
	m.dirty = false
	commands := make([]tea.Cmd, 0, len(m.panels))
	for i := range m.panels {
		m.panels[i].loading = true
		commands = append(commands, m.loadPanel(ctx, generation, i, m.serverID, m.projectID))
	}
	return tea.Batch(commands...)
}
func (m *Model) switchServer(direction int) tea.Cmd {
	if len(m.servers) == 0 {
		return nil
	}
	m.serverIndex = (m.serverIndex + direction + len(m.servers)) % len(m.servers)
	m.serverID = m.servers[m.serverIndex].GetConfig().GetId()
	m.selectedAgreement()
	m.clearServerQueries()
	return m.refresh()
}

// clearServerQueries invalidates replies even when refresh cannot run yet
// (for example, while the event stream is disconnected).
func (m *Model) clearServerQueries() {
	if m.queryCancel != nil {
		m.queryCancel()
	}
	m.clearScreen()
	m.generation++
	m.projectID = ""
	m.cursor, m.pending = 0, 0
	m.dirty = false
	m.sessions, m.conflicts = nil, nil
	m.panels = [5]panel{}
	m.dialog = nil
	m.screenSupported = false
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case screenMsg:
		if message.generation != m.generation || message.server != m.serverID || m.cursor >= len(m.sessions) || m.sessions[m.cursor].Id != message.session {
			return m, nil
		}
		m.screen = message.screen
		m.screenError = message.err
	case projectsMsg:
		return m, m.acceptProjects(message)
	case mutationMsg:
		return m, m.acceptMutation(message)
	case tea.WindowSizeMsg:
		m.width = max(1, message.Width)
		m.height = max(1, message.Height)
	case serversMsg:
		if message.err != nil {
			m.status = "Servers: " + message.err.Error()
			break
		}
		m.servers = message.servers
		found := false
		for i, server := range m.servers {
			if server.GetConfig().GetId() == m.serverID {
				m.serverIndex = i
				found = true
			}
		}
		if !found && len(m.servers) > 0 {
			m.serverIndex = 0
			m.serverID = m.servers[0].GetConfig().GetId()
			m.clearServerQueries()
		}
		if len(m.servers) == 0 && m.serverID != "" {
			m.serverID = ""
			m.clearServerQueries()
		}
		m.selectedAgreement()
		return m, m.refresh()
	case subscribedMsg:
		if message.err != nil {
			m.eventError = message.err
			return m, m.reconnect(message.err)
		}
		m.stream = message.stream
		m.streamOpen = true
		m.eventError = nil
		m.instance = message.event.GetInstanceId()
		m.sequence = message.event.GetSequence()
		return m, tea.Batch(m.receive(), m.refresh())
	case eventMsg:
		if message.err != nil {
			_ = m.stream.Close()
			m.eventError = message.err
			m.streamOpen = false
			return m, m.reconnect(message.err)
		}
		if message.event.GetInstanceId() != m.instance {
			m.action.Reconnect = true
			return m, tea.Quit
		}
		// Events are invalidation hints. Rescan for all hints, sequence gaps and
		// overflow recovery; current generation prevents stale server responses.
		m.sequence = message.event.GetSequence()
		return m, tea.Batch(m.receive(), m.refresh(), m.loadServers())
	case panelMsg:
		if message.generation != m.generation {
			return m, nil
		}
		m.panels[message.index] = panel{rows: message.rows, err: message.err}
		if message.index == 0 {
			m.sessions = message.sessions
			m.agreement = message.agreement
			m.screenSupported = message.screenSupported
			m.cursor = min(m.cursor, max(0, len(m.sessions)-1))
		}
		if message.index == 1 {
			m.conflicts = message.conflicts
		}
		m.pending--
		var next tea.Cmd
		if m.pending == 0 && m.dirty {
			next = m.refresh()
		}
		if message.index == 0 {
			return m, tea.Batch(next, m.loadScreen())
		}
		return m, next
	case tickMsg:
		if !m.streamOpen {
			return m, tea.Batch(m.subscribe(), tick())
		}
		return m, tea.Batch(m.refresh(), m.loadServers(), tick())
	case tea.KeyMsg:
		if m.dialog != nil {
			return m, m.dialogKey(message)
		}
		switch message.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.tab = (m.tab + 1) % len(m.panels)
			m.cursor = 0
		case "shift+tab":
			m.tab = (m.tab + len(m.panels) - 1) % len(m.panels)
			m.cursor = 0
		case "left", "h":
			return m, m.switchServer(-1)
		case "right", "l":
			return m, m.switchServer(1)
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
			if m.tab == 0 {
				return m, m.loadScreen()
			}
		case "down", "j":
			m.cursor = min(max(0, len(m.panels[m.tab].rows)-1), m.cursor+1)
			if m.tab == 0 {
				return m, m.loadScreen()
			}
		case "f5", "ctrl+r":
			return m, m.refresh()
		case "n", "N":
			if m.tab == 0 && m.canCreate() {
				return m, m.beginCreate()
			}
		case "d", "D":
			if m.tab == 0 && m.allowed("session.delete") {
				return m, m.beginEdit("delete")
			}
		case "r", "R":
			if m.tab == 0 && m.allowed("session.update") {
				return m, m.beginEdit("rename")
			}
		case "e", "E":
			if m.tab == 0 && m.allowed("session.update") {
				return m, m.beginEdit("edit")
			}
		case "enter", "o", "t":
			if !m.canObserve() || message.String() == "t" && !m.allowed("session.attach", "session.control", "session.write") {
				return m, nil
			}
			if m.tab == 0 && len(m.sessions) > 0 && m.cursor < len(m.sessions) {
				m.action = Action{ServerID: m.serverID, Session: m.sessions[m.cursor], ReadOnly: message.String() == "o" || !m.allowed("session.write"), TakeControl: message.String() == "t"}
				return m, tea.Quit
			}
		}
	}
	return m, nil
}
func (m *Model) reconnect(err error) tea.Cmd {
	if errors.Is(err, io.EOF) || connect.CodeOf(err) == connect.CodeUnavailable || connect.CodeOf(err) == connect.CodeFailedPrecondition {
		m.action.Reconnect = true
		return tea.Quit
	}
	if connect.CodeOf(err) == connect.CodeResourceExhausted {
		return m.subscribe()
	}
	m.status = fmt.Sprintf("Event stream: %v", err)
	return nil
}
