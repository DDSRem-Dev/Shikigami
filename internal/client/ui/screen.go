package ui

import (
	"context"
	"errors"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type screenMsg struct {
	generation      uint64
	server, session string
	screen          *pb.SessionScreen
	err             error
}

func (m *Model) clearScreen() {
	if m.screenCancel != nil {
		m.screenCancel()
	}
	m.screen = nil
	m.screenError = nil
}
func (m *Model) loadScreen() tea.Cmd {
	m.clearScreen()
	if !m.screenSupported {
		m.screenError = errors.New("server does not support terminal.screen.v1")
		return nil
	}
	if !m.canObserve() {
		m.screenError = errors.New("session.attach permission not granted")
		return nil
	}
	if m.cursor >= len(m.sessions) {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.screenCancel = cancel
	server, session, generation, c := m.serverID, m.sessions[m.cursor].Id, m.generation, m.client
	return func() tea.Msg {
		screen, err := c.Remote(server).GetSessionScreen(ctx, &pb.GetSessionRequest{Id: session})
		return screenMsg{generation: generation, server: server, session: session, screen: screen, err: err}
	}
}
