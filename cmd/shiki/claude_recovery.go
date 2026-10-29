package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// recoverClaude runs outside raw mode. It returns an explicit request to attach
// again; neither a crash nor a failed mutation is silently retried.
func (a *app) recoverClaude(ctx context.Context, c *client.Client, server string, session *pb.Session, crash *client.ClaudeExitError) (*pb.Session, bool, error) {
	current, err := c.Remote(server).GetSession(ctx, &pb.GetSessionRequest{Id: session.Id})
	if err != nil {
		return session, false, err
	}
	if current.ProjectId != session.ProjectId || current.Id != session.Id {
		return session, false, fmt.Errorf("session scope changed while checking Claude state")
	}
	session = current
	if session.ClaudeStatus == "running" || session.ClaudeStatus == "starting" {
		fmt.Fprintf(a.errorOutput, "Claude is now %s; reconnecting to the current process.\n", safeText(session.ClaudeStatus))
		return session, true, nil
	}
	record, err := c.ResolveServer(ctx, server)
	if err != nil {
		return session, false, err
	}
	agreement := record.RemoteProtocol
	if crash != nil && crash.ReasonCode == "EXIT_REASON_UNKNOWN" {
		fmt.Fprintf(a.errorOutput, "Claude process ended; exit reason is unknown · server %s · project %s · session %s.\n", safeText(server), safeText(session.ProjectId), safeText(session.Name))
	} else {
		fmt.Fprintf(a.errorOutput, "Claude stopped unexpectedly · server %s · project %s · session %s.\n", safeText(server), safeText(session.ProjectId), safeText(session.Name))
	}
	if crash != nil && crash.ExitCode != "" {
		fmt.Fprintf(a.errorOutput, "Exit code: %s\n", safeText(crash.ExitCode))
	} else {
		fmt.Fprintln(a.errorOutput, "Exit code: unknown")
	}
	if crash != nil && crash.ExitSignal != "" {
		fmt.Fprintf(a.errorOutput, "Termination signal: %s\n", safeText(crash.ExitSignal))
	}
	fmt.Fprintln(a.errorOutput, "The tmux session and project files remain. A restart starts a new process; conversation recovery is not guaranteed.")
	for {
		var choices []smartSessionChoice
		if smartGrant(agreement, "session.control") {
			choices = append(choices, smartSessionChoice{"restart", "Restart Claude Code (new process)"})
		}
		if smartGrant(agreement, "session.attach") && slices.Contains(agreement.GetCapabilities(), "terminal.logs.v1") {
			choices = append(choices, smartSessionChoice{"logs", "View retained tmux output (up to 2,000 lines; not permanent history)"})
		}
		if smartGrant(agreement, "session.attach") && slices.Contains(agreement.GetCapabilities(), "terminal.screen.v1") {
			choices = append(choices, smartSessionChoice{"screen", "View current terminal screen"})
		}
		choices = append(choices, smartSessionChoice{"manual", "Show manual tmux attachment command on the server"}, smartSessionChoice{"exit", "Exit (keep the session)"})
		labels := make([]string, len(choices))
		for i := range choices {
			labels[i] = choices[i].label
		}
		selected, err := a.choose(ctx, "Recovery", labels)
		if err != nil {
			return session, false, err
		}
		switch choices[selected].action {
		case "logs":
			logs, err := c.Remote(server).GetSessionLogs(ctx, &pb.SessionLogsRequest{SessionId: session.Id, MaxLines: 2000})
			if err != nil {
				fmt.Fprintf(a.errorOutput, "Retained output unavailable: %s\n", safeText(err.Error()))
				continue
			}
			if logs.SessionId != session.Id {
				return session, false, fmt.Errorf("retained output belongs to a different session")
			}
			fmt.Fprintf(a.errorOutput, "Retained tmux output · %s · captured %d · truncated: %t · %s\n", safeText(logs.State), logs.CapturedAt, logs.Truncated, safeText(logs.ReasonCode))
			fmt.Fprintln(a.errorOutput, "Only the currently retained scrollback and screen are available; this is not durable history or a terminal byte replay.")
			if logs.ExitCode != nil {
				fmt.Fprintf(a.errorOutput, "Observed exit code: %d\n", *logs.ExitCode)
			}
			if logs.State == "available" {
				fmt.Fprintln(a.errorOutput, safeMultiline(logs.Text))
			}
		case "restart":
			if err = a.confirm(ctx, "Start a new Claude process in session "+safeText(session.Name), false); err != nil {
				return session, false, err
			}
			current, err = c.Remote(server).GetSession(ctx, &pb.GetSessionRequest{Id: session.Id})
			if err != nil {
				return session, false, err
			}
			if current.Id != session.Id || current.ProjectId != session.ProjectId {
				return session, false, fmt.Errorf("session scope changed before restart")
			}
			if current.ClaudeStatus == "running" || current.ClaudeStatus == "starting" {
				fmt.Fprintf(a.errorOutput, "Claude is now %s; no restart was sent.\n", safeText(current.ClaudeStatus))
				return current, true, nil
			}
			next, err := c.Remote(server).RestartClaude(ctx, &pb.SessionActionRequest{SessionId: session.Id})
			if err != nil {
				return session, false, err
			}
			fmt.Fprintf(a.errorOutput, "Claude state: %s. Shiki did not restore conversation history.\n", safeText(next.ClaudeStatus))
			return next, true, nil
		case "screen":
			screen, err := c.Remote(server).GetSessionScreen(ctx, &pb.GetSessionRequest{Id: session.Id})
			if err != nil {
				fmt.Fprintf(a.errorOutput, "Screen unavailable: %s\n", safeText(err.Error()))
				continue
			}
			fmt.Fprintf(a.errorOutput, "Current screen (captured %d; truncated: %t; not full logs):\n%s\n", screen.CapturedAt, screen.Truncated, safeMultiline(screen.Text))
		case "manual":
			name := session.TmuxSession
			if name == "" {
				name = "shiki-" + session.Id
			}
			quoted := "'" + strings.ReplaceAll(name, "'", "'\\''") + "'"
			fmt.Fprintf(a.errorOutput, "On the selected server, as the daemon's configured OS user, run:\ntmux -L \"shiki-$(id -u)\" attach-session -t %s\n", safeText(quoted))
			return session, false, nil
		case "exit":
			return session, false, nil
		}
	}
}

func safeMultiline(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 || r == 127 {
			return '�'
		}
		return r
	}, value)
}
