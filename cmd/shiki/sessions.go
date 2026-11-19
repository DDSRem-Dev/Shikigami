package main

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (a *app) sessionCommands() *cobra.Command {
	root := &cobra.Command{Use: "sessions", Short: "Manage independent remote tmux sessions"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			result, e := c.Remote(server).ListSessions(cmd.Context(), &pb.ListSessionsRequest{})
			if e != nil {
				return e
			}
			if project != "" {
				filtered := result.Sessions[:0]
				for _, s := range result.Sessions {
					if s.ProjectId == project {
						filtered = append(filtered, s)
					}
				}
				result.Sessions = filtered
			}
			return a.print(result)
		})
	}}
	root.RunE = list.RunE
	root.Args = cobra.NoArgs
	root.AddCommand(list)
	var workingDir, description string
	var tags []string
	var noClaude bool
	create := &cobra.Command{Use: "create <name>", Short: "Create a session in the explicitly selected --project", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return fmt.Errorf("resolve session creation server/project; CreateSession request has not been sent: %w", e)
			}
			if project == "" {
				return errors.New("sessions create requires --project")
			}
			start := !noClaude
			requestID := a.id()
			session, e := c.Remote(server).CreateSession(cmd.Context(), &pb.CreateSessionRequest{ProjectId: project, Name: args[0], WorkingDir: workingDir, Description: description, Tags: tags, AutoStartClaude: &start, RequestId: requestID})
			if e != nil {
				return sessionCreateError(e, requestID)
			}
			return a.print(session)
		})
	}}
	create.Flags().StringVar(&workingDir, "working-dir", "", "Remote project directory or subdirectory")
	create.Flags().StringVar(&description, "description", "", "Session description")
	create.Flags().StringSliceVar(&tags, "tags", nil, "Comma-separated tags")
	create.Flags().BoolVar(&noClaude, "no-claude", false, "Create an empty shell without starting Claude")
	root.AddCommand(create)
	root.AddCommand(&cobra.Command{Use: "info <name-or-id>", Aliases: []string{"get"}, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			session, e := c.ResolveSession(cmd.Context(), server, project, args[0])
			if e != nil {
				return e
			}
			return a.print(session)
		})
	}})
	var editDescription string
	var editName string
	var editTags []string
	var metadata map[string]string
	edit := &cobra.Command{Use: "edit <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			session, e := c.ResolveSession(cmd.Context(), server, project, args[0])
			if e != nil {
				return e
			}
			paths := []string{}
			for _, name := range []string{"name", "description", "tags", "metadata"} {
				if cmd.Flags().Changed(name) {
					paths = append(paths, name)
				}
			}
			if len(paths) == 0 {
				return errors.New("edit requires --name, --description, --tags, or --metadata")
			}
			result, e := c.Remote(server).UpdateSession(cmd.Context(), &pb.UpdateSessionRequest{Id: session.Id, Name: editName, Description: editDescription, Tags: editTags, Metadata: metadata, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	edit.Flags().StringVar(&editDescription, "description", "", "Replace the description")
	edit.Flags().StringVar(&editName, "name", "", "Rename within the project, preserving the session ID and process")
	edit.Flags().StringSliceVar(&editTags, "tags", nil, "Replace all tags")
	edit.Flags().StringToStringVar(&metadata, "metadata", nil, "Replace metadata using key=value pairs")
	root.AddCommand(edit)
	var yes bool
	remove := &cobra.Command{Use: "delete <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			session, e := c.ResolveSession(cmd.Context(), server, project, args[0])
			if e != nil {
				return e
			}
			if e = a.confirm(cmd.Context(), "End session "+safeText(session.Name)+" and delete its record (project files remain)", yes); e != nil {
				return e
			}
			result, e := c.Remote(server).DeleteSession(cmd.Context(), &pb.DeleteSessionRequest{Id: session.Id})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm session termination without prompting")
	root.AddCommand(remove)
	for _, restart := range []bool{false, true} {
		name := "stop-claude"
		description := "End the current pane process and leave a shell, retaining session and project files"
		if restart {
			name = "restart-claude"
			description = "Replace the current pane process with Claude, retaining session and project files"
		}
		var confirmed bool
		command := &cobra.Command{Use: name + " <name-or-id>", Short: description, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if !a.interactive() && !confirmed {
				return errors.New(name + " requires --yes in non-interactive mode")
			}
			return a.withClient(cmd.Context(), true, func(c *client.Client) error {
				server, project, e := a.selected(cmd.Context(), c)
				if e != nil {
					return e
				}
				session, e := c.ResolveSession(cmd.Context(), server, project, args[0])
				if e != nil {
					return e
				}
				impact := "End the current pane process and leave a shell"
				if restart {
					impact = "Replace the current pane process with a new Claude process"
				}
				label := impact + " for session " + safeText(session.Name) + " (" + safeText(session.Id) + ") on server " + safeText(server) + " / project " + safeText(session.ProjectId) + " (session record and project files remain)"
				if e = a.confirm(cmd.Context(), label, confirmed); e != nil {
					return e
				}
				var result *pb.Session
				if restart {
					result, e = c.Remote(server).RestartClaude(cmd.Context(), &pb.SessionActionRequest{SessionId: session.Id})
				} else {
					result, e = c.Remote(server).StopClaude(cmd.Context(), &pb.SessionActionRequest{SessionId: session.Id})
				}
				if e != nil {
					return e
				}
				return a.print(result)
			})
		}}
		command.Flags().BoolVar(&confirmed, "yes", false, "Confirm ending or replacing the current pane process without prompting")
		root.AddCommand(command)
	}
	a.addSnapshotCommands(root)
	return root
}

func sessionCreateError(err error, requestID string) error {
	code := connect.CodeOf(err)
	if code == connect.CodeCanceled || code == connect.CodeDeadlineExceeded || code == connect.CodeUnavailable || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("CreateSession result may be unknown; first run shiki sessions list with the same --server and --project to check. Absence from the list does not prove failure. While the result remains unknown, keep all original arguments and use --request-id %q to retrieve the original outcome; this does not rerun a recorded failure. Only after confirming the original request failed, explicitly create with a new request ID: %w", requestID, err)
	}
	return fmt.Errorf("CreateSession failed: %w", err)
}
