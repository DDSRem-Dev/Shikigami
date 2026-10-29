package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/spf13/cobra"
)

func (a *app) projectCommands() *cobra.Command {
	root := &cobra.Command{Use: "projects", Short: "Manage remote project roots and local directory mappings"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, _, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			list, e := c.Remote(server).ListProjects(cmd.Context(), &pb.ListProjectsRequest{})
			if e != nil {
				return e
			}
			return a.print(list)
		})
	}}
	root.RunE = list.RunE
	root.Args = cobra.NoArgs
	root.AddCommand(list)
	var path, local string
	create := &cobra.Command{Use: "create <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, _, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			if path == "" {
				path, e = a.prompt(cmd.Context(), "Remote project path", "")
				if e != nil {
					return e
				}
			}
			record, e := c.Remote(server).CreateProject(cmd.Context(), &pb.CreateProjectRequest{Name: args[0], Path: path, RequestId: a.id()})
			if e != nil {
				return e
			}
			if local != "" {
				if e = a.saveMapping(cmd, c, server, record.Id, local, ""); e != nil {
					return e
				}
			}
			return a.print(record)
		})
	}}
	create.Flags().StringVar(&path, "path", "", "Absolute project directory on the remote server")
	create.Flags().StringVar(&local, "map", "", "Explicitly map an existing local directory (sync remains paused)")
	root.AddCommand(create)
	root.AddCommand(&cobra.Command{Use: "get <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, _, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			record, e := c.ResolveProject(cmd.Context(), server, args[0])
			if e != nil {
				return e
			}
			return a.print(record)
		})
	}})
	var yes bool
	remove := &cobra.Command{Use: "delete <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, _, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			record, e := c.ResolveProject(cmd.Context(), server, args[0])
			if e != nil {
				return e
			}
			if e = a.confirm(cmd.Context(), "Delete project record "+safeText(record.Name)+" (files remain)", yes); e != nil {
				return e
			}
			result, e := c.Remote(server).DeleteProject(cmd.Context(), &pb.DeleteProjectRequest{Id: record.Id})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm deletion without prompting")
	root.AddCommand(remove)
	var mapLocal, defaultSession string
	mapping := &cobra.Command{Use: "map <project-name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, _, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			record, e := c.ResolveProject(cmd.Context(), server, args[0])
			if e != nil {
				return e
			}
			if mapLocal == "" {
				return errors.New("--local is required")
			}
			if e = a.saveMapping(cmd, c, server, record.Id, mapLocal, defaultSession); e != nil {
				return e
			}
			list, e := c.Agent().ListMappings(cmd.Context(), &apb.ListMappingsRequest{ServerId: server})
			if e != nil {
				return e
			}
			return a.print(list)
		})
	}}
	mapping.Flags().StringVar(&mapLocal, "local", "", "Existing local directory")
	mapping.Flags().StringVar(&defaultSession, "default-session", "", "Preferred session ID or name")
	root.AddCommand(mapping)
	root.AddCommand(&cobra.Command{Use: "mappings", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server := ""
			if a.server != "" {
				record, e := c.ResolveServer(cmd.Context(), a.server)
				if e != nil {
					return e
				}
				server = record.Config.Id
			}
			list, e := c.Agent().ListMappings(cmd.Context(), &apb.ListMappingsRequest{ServerId: server})
			if e != nil {
				return e
			}
			return a.print(list)
		})
	}})
	return root
}
func (a *app) saveMapping(cmd *cobra.Command, c *client.Client, server, project, local, defaultSession string) error {
	path, e := client.ConfigPath(local)
	if e != nil {
		return e
	}
	path, e = filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return errors.New("local mapping root must be a directory")
	}
	if defaultSession != "" {
		session, e := c.ResolveSession(cmd.Context(), server, project, defaultSession)
		if e != nil {
			return e
		}
		defaultSession = session.Id
	}
	mapping := &ppb.Mapping{ServerId: server, ProjectId: project, LocalRoot: path, DefaultSession: defaultSession, SyncEnabled: false}
	expected := ""
	mappings, e := c.Agent().ListMappings(cmd.Context(), &apb.ListMappingsRequest{ServerId: server})
	if e != nil {
		return e
	}
	for _, old := range mappings.Mappings {
		if old.LocalRoot == path {
			mapping.Id = old.Id
			expected = old.Revision
			mapping.SessionHints = old.SessionHints
		}
	}
	_, e = c.Agent().PutMapping(cmd.Context(), &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: expected, RequestId: a.id() + "-mapping"})
	return e
}
