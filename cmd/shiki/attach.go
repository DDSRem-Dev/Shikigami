package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/spf13/cobra"
)

func (a *app) attachCommand() *cobra.Command {
	var readOnly, takeControl, reconnect bool
	var detachKey string
	command := &cobra.Command{Use: "attach [session-name-or-id]", Aliases: []string{"@"}, Args: cobra.MaximumNArgs(1), Short: "Attach to a remote terminal; Ctrl+Q detaches", RunE: func(cmd *cobra.Command, args []string) error {
		if !a.interactive() {
			return errors.New("attach requires terminal stdin and stdout; use sessions commands for non-interactive management")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				name, e = a.chooseSession(cmd.Context(), c, server, project)
				if e != nil {
					return e
				}
			}
			session, e := c.ResolveSession(cmd.Context(), server, project, name)
			if e != nil {
				return e
			}
			if detachKey == "" {
				preferences, e := c.Agent().GetPreferences(cmd.Context(), &pb.Empty{})
				if e != nil {
					return e
				}
				detachKey = preferences.DetachKey
			}
			key, e := client.ParseDetachKey(detachKey)
			if e != nil {
				return e
			}
			return a.runTerminal(cmd.Context(), c, server, session, client.TerminalOptions{Input: a.input, Output: a.output.(*os.File), ReadOnly: readOnly, TakeControl: takeControl, DetachKey: key}, reconnect)
		})
	}}
	command.Flags().BoolVar(&readOnly, "read-only", false, "Observe without input or resize")
	command.Flags().BoolVar(&takeControl, "take-control", false, "Explicitly request control for this new attachment")
	command.Flags().BoolVar(&reconnect, "reconnect", true, "Reconnect up to five times without replaying input")
	command.Flags().StringVar(&detachKey, "detach-key", "", "ctrl-q, ctrl-], or none (defaults to agent preference)")
	return command
}
func (a *app) runTerminal(ctx context.Context, initial *client.Client, server string, session *pb.Session, options client.TerminalOptions, reconnect bool) error {
	if a.json {
		return errors.New("--json cannot be combined with an interactive terminal")
	}
	if session.ClaudeStatus == "crashed" {
		next, attach, err := a.recoverClaude(ctx, initial, server, session, nil)
		if err != nil || !attach {
			return err
		}
		session = next
	}
	options.BeforeRaw = func(state client.TerminalState) {
		label := "controller"
		if state.Mode == "observer" {
			label = "read-only observer"
		}
		fmt.Fprintf(a.errorOutput, "Attached to %s (%s). Detach: %s.\n", safeText(session.Name), label, detachLabel(options.DetachKey))
		if a.verbose {
			fmt.Fprintln(a.errorOutput, "Verbose plugin logs will be shown after detach; use shiki dashboard --verbose for the live Plugins panel.")
		}
	}
	current := initial
	var err error
	for attempt := 0; ; attempt++ {
		if current != nil {
			err = current.Terminal(ctx, server, session.Id, options)
			a.printVerboseLogs(ctx, current)
			var crash *client.ClaudeExitError
			if errors.As(err, &crash) {
				next, attach, recoveryErr := a.recoverClaude(ctx, current, server, session, crash)
				if recoveryErr != nil || !attach {
					return recoveryErr
				}
				session = next
				attempt = -1
				continue
			}
		}
		if err == nil {
			fmt.Fprintln(a.errorOutput, "Detached; the remote session continues.")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !reconnect || attempt >= 5 || !terminalRetryable(err) {
			return err
		}
		if current != nil {
			_ = current.Close()
			current = nil
		}
		delay := min(time.Second*time.Duration(1<<attempt), 30*time.Second)
		delay = time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
		fmt.Fprintf(a.errorOutput, "Connection interrupted. Reconnecting (%d/5); Ctrl+C cancels.\n", attempt+1)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		next, e := client.Open(ctx, a.configPath, client.Options{AutoStart: true, Timeout: a.timeout})
		if e != nil {
			err = e
			continue
		}
		defer next.Close()
		current = next
	}
}
func terminalRetryable(err error) bool { return client.TerminalRetryable(err) }

func detachLabel(key byte) string {
	switch key {
	case 17:
		return "Ctrl+Q"
	case 29:
		return "Ctrl+]"
	default:
		return "disabled (close the terminal or signal the CLI)"
	}
}
func (a *app) choose(ctx context.Context, label string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("no choices are available")
	}
	for index, value := range options {
		fmt.Fprintf(a.errorOutput, "  %d. %s\n", index+1, safeText(value))
	}
	answer, e := a.prompt(ctx, label, "")
	if e != nil {
		return 0, e
	}
	number, e := strconv.Atoi(answer)
	if e != nil || number < 1 || number > len(options) {
		return 0, errors.New("invalid menu selection")
	}
	return number - 1, nil
}
func (a *app) chooseSession(ctx context.Context, c *client.Client, server, project string) (string, error) {
	list, e := c.Remote(server).ListSessions(ctx, &pb.ListSessionsRequest{})
	if e != nil {
		return "", e
	}
	var sessions []*pb.Session
	var choices []string
	for _, session := range list.Sessions {
		if project == "" || session.ProjectId == project {
			sessions = append(sessions, session)
			choices = append(choices, fmt.Sprintf("%s · project %s · Claude %s · %d attachment(s)", session.Name, session.ProjectId, session.ClaudeStatus, session.ClientCount))
		}
	}
	if len(sessions) == 0 {
		return "", errors.New("no sessions; create one with shiki sessions create <name> --project <id>")
	}
	if len(sessions) == 1 {
		return sessions[0].Id, nil
	}
	selected, e := a.choose(ctx, "Session", choices)
	if e != nil {
		return "", e
	}
	return sessions[selected].Id, nil
}
func (a *app) smart(ctx context.Context) error {
	if !a.interactive() {
		return errors.New("shiki without a command requires a terminal; use servers, projects, or sessions commands")
	}
	if a.json {
		return errors.New("--json requires an explicit management command")
	}
	return a.withClient(ctx, true, func(c *client.Client) error {
		servers, e := c.Agent().ListServers(ctx, &pb.Empty{})
		if e != nil {
			return e
		}
		if len(servers.Servers) == 0 {
			fmt.Fprintln(a.errorOutput, "Set up your first server.")
			name, e := a.prompt(ctx, "Server name", "")
			if e != nil {
				return e
			}
			transport, e := a.prompt(ctx, "Connection (tailnet or tls)", "tailnet")
			if e != nil {
				return e
			}
			flags := new(serverFlags)
			switch transport {
			case "tailnet":
				flags.tailnet, e = a.prompt(ctx, "Server address (Tailscale IP or hostname; default port 9527)", "")
			case "tls":
				flags.tlsAddress, e = a.prompt(ctx, "Direct TLS server address (host:port)", "")
				if e == nil {
					flags.ca, e = a.prompt(ctx, "Trusted CA file (empty uses system roots)", "")
				}
			default:
				return errors.New("connection must be tailnet or tls")
			}
			if e != nil {
				return e
			}
			setup := &cobra.Command{}
			setup.SetContext(ctx)
			if e = a.saveServer(setup, c, []string{name}, flags, false, true); e != nil {
				return e
			}
		}
		cwd, e := os.Getwd()
		if e != nil {
			return e
		}
		mappings, e := c.Agent().ListMappings(ctx, &apb.ListMappingsRequest{})
		if e != nil {
			return e
		}
		mapping, relative, e := client.MappingForDirectory(cwd, mappings.Mappings)
		if e != nil {
			return e
		}
		server, projectID, preferred := "", "", ""
		if mapping != nil {
			server, projectID, preferred = mapping.ServerId, mapping.ProjectId, mapping.DefaultSession
			best := 0
			for hint, session := range mapping.SessionHints {
				if relative == hint || strings.HasPrefix(relative, hint+string(filepath.Separator)) {
					if len(hint) > best {
						best = len(hint)
						preferred = session
					}
				}
			}
		}
		if a.server != "" || server == "" {
			record, e := c.ResolveServer(ctx, a.server)
			if e != nil && a.server == "" && len(servers.Servers) > 1 {
				choices := make([]string, len(servers.Servers))
				for i, item := range servers.Servers {
					choices[i] = item.Config.Name
				}
				index, chooseErr := a.choose(ctx, "Server", choices)
				if chooseErr != nil {
					return chooseErr
				}
				record, e = servers.Servers[index], nil
			}
			if e != nil {
				return e
			}
			if server != "" && server != record.Config.Id {
				projectID, preferred, mapping = "", "", nil
			}
			server = record.Config.Id
		}
		if a.project != "" {
			project, e := c.ResolveProject(ctx, server, a.project)
			if e != nil {
				return e
			}
			if projectID != "" && projectID != project.Id {
				mapping = nil
			}
			projectID, preferred = project.Id, ""
		}
		if projectID == "" {
			list, e := c.Remote(server).ListProjects(ctx, &pb.ListProjectsRequest{})
			if e != nil {
				return e
			}
			verified, e := c.ResolveServer(ctx, server)
			if e != nil {
				return e
			}
			canCreate := smartGrant(verified.RemoteProtocol, "project.create")
			choices := []string{}
			if canCreate {
				choices = append(choices, "Create a new remote project")
			}
			for _, p := range list.Projects {
				choices = append(choices, p.Name+" · "+p.Path)
			}
			if len(choices) == 0 {
				return errors.New("no projects are available and project.create permission is not granted")
			}
			selected, e := a.choose(ctx, "Project", choices)
			if e != nil {
				return e
			}
			if selected == 0 && canCreate {
				name, e := a.prompt(ctx, "Project name", filepath.Base(cwd))
				if e != nil {
					return e
				}
				path, e := a.prompt(ctx, "Remote project path", "")
				if e != nil {
					return e
				}
				created, e := c.Remote(server).CreateProject(ctx, &pb.CreateProjectRequest{Name: name, Path: path, RequestId: a.smartRequestID("project:" + server + ":" + name)})
				if e != nil {
					return e
				}
				projectID = created.Id
			} else {
				if canCreate {
					selected--
				}
				projectID = list.Projects[selected].Id
			}
			fmt.Fprintf(a.errorOutput, "Associate local directory %s with server %s · project %s.\n", safeText(cwd), safeText(server), safeText(projectID))
			if e = a.confirm(ctx, "Associate this local directory with the selected project (sync remains paused)", false); e != nil {
				return e
			}
			local, e := filepath.EvalSymlinks(cwd)
			if e != nil {
				return e
			}
			mapping, e = c.Agent().PutMapping(ctx, &apb.PutMappingRequest{Mapping: &ppb.Mapping{ServerId: server, ProjectId: projectID, LocalRoot: local}, RequestId: a.smartRequestID("mapping:" + server + ":" + projectID + ":" + local)})
			if e != nil {
				return e
			}
			mapping, e = a.setupSmartSync(ctx, c, mapping, verified.RemoteProtocol)
			if e != nil {
				return e
			}
		}
		sessions, e := c.Remote(server).ListProjectSessions(ctx, &pb.ListProjectSessionsRequest{ProjectId: projectID})
		if e != nil {
			return e
		}
		verified, e := c.ResolveServer(ctx, server)
		if e != nil {
			return e
		}
		agreement := verified.RemoteProtocol
		if preferred != "" {
			if _, resolveErr := c.ResolveSession(ctx, server, projectID, preferred); resolveErr != nil {
				if connect.CodeOf(resolveErr) != connect.CodeNotFound {
					return resolveErr
				}
				if mapping != nil {
					missing := preferred
					if mapping.DefaultSession == missing {
						mapping.DefaultSession = ""
					}
					for hint, session := range mapping.SessionHints {
						if session == missing {
							delete(mapping.SessionHints, hint)
						}
					}
					mapping, e = c.Agent().PutMapping(ctx, &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: mapping.Revision, RequestId: a.smartRequestID("mapping-prune:" + mapping.Id + ":" + mapping.Revision)})
					if e != nil {
						return e
					}
				}
				preferred = ""
			}
		}
		if len(sessions.Sessions) == 0 {
			created, e := a.createSmartSession(ctx, c, server, projectID, agreement)
			if e != nil {
				return e
			}
			preferred = created.Id
		}
		if preferred == "" {
			preferred, e = a.chooseSession(ctx, c, server, projectID)
			if e != nil {
				return e
			}
		}
		selected, e := c.ResolveSession(ctx, server, projectID, preferred)
		if e != nil {
			return e
		}
		readOnly := !smartGrant(agreement, "session.write")
		if selected.ClientCount > 0 {
			fmt.Fprintf(a.errorOutput, "Session %s · server %s · project %s has %d active attachment(s).\n", safeText(selected.Name), safeText(server), safeText(projectID), selected.ClientCount)
			choices := activeSessionChoices(selected, agreement)
			labels := make([]string, len(choices))
			for i := range choices {
				labels[i] = choices[i].label
			}
			index, err := a.choose(ctx, "Action", labels)
			if err != nil {
				return err
			}
			switch choices[index].action {
			case "observe":
				readOnly = true
			case "create":
				selected, e = a.createSmartSession(ctx, c, server, projectID, agreement)
				if e != nil {
					return e
				}
			case "manage":
				_ = c.Close()
				dashboardApp := *a
				dashboardApp.server, dashboardApp.project = server, projectID
				command := dashboardApp.dashboardCommand()
				command.SetContext(ctx)
				return command.RunE(command, nil)
			case "exit":
				return nil
			}
		}
		if agreement.GetDaemonId() != "" && !smartGrant(agreement, "session.attach") {
			return errors.New("session is available, but this credential does not grant session.attach")
		}
		if mapping != nil && mapping.DefaultSession == "" {
			mapping.DefaultSession = selected.Id
			mapping, e = c.Agent().PutMapping(ctx, &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: mapping.Revision, RequestId: a.smartRequestID("mapping-default:" + mapping.Id + ":" + mapping.Revision)})
			if e != nil {
				return e
			}
		}
		preferences, e := c.Agent().GetPreferences(ctx, &pb.Empty{})
		if e != nil {
			return e
		}
		key, e := client.ParseDetachKey(preferences.DetachKey)
		if e != nil {
			return e
		}
		return a.runTerminal(ctx, c, server, selected, client.TerminalOptions{Input: a.input, Output: a.output.(*os.File), ReadOnly: readOnly, DetachKey: key}, true)
	})
}
