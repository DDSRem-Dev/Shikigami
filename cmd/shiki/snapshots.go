package main

import (
	"errors"
	"fmt"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/spf13/cobra"
)

func (a *app) addSnapshotCommands(root *cobra.Command) {
	var description string
	snapshot := &cobra.Command{Use: "snapshot <session-name-or-id>", Args: cobra.ExactArgs(1), Short: "Save session launch configuration without files, environment, or conversation history", RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			source, e := c.ResolveSession(cmd.Context(), server, project, args[0])
			if e != nil {
				return e
			}
			result, e := c.Remote(server).CreateSnapshot(cmd.Context(), &pb.CreateSnapshotRequest{SessionId: source.Id, Description: description})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	snapshot.Flags().StringVar(&description, "description", "", "Description of this configuration snapshot")
	root.AddCommand(snapshot)
	var sourceID string
	list := &cobra.Command{Use: "snapshots", Args: cobra.NoArgs, Short: "List saved configuration snapshots, including deleted source sessions", RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			result, e := c.Remote(server).ListSnapshots(cmd.Context(), &pb.ListSnapshotsRequest{ProjectId: project, SessionId: sourceID})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	list.Flags().StringVar(&sourceID, "session-id", "", "Filter by the original session ID, even after its deletion")
	root.AddCommand(list)
	var name string
	var yes bool
	restore := &cobra.Command{Use: "restore <snapshot-id>", Args: cobra.ExactArgs(1), Short: "Create a new session from saved configuration using current project files", RunE: func(cmd *cobra.Command, args []string) error {
		if name == "" {
			return errors.New("restore requires --name for the new session")
		}
		if !a.interactive() && !yes {
			return errors.New("restore starts a new session and requires --yes in non-interactive mode")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			if project != "" {
				if e = checkSnapshotProject(cmd, c, server, project, args[0]); e != nil {
					return e
				}
			}
			if e = a.confirm(cmd.Context(), "Create a new session using saved configuration and current project files; files and Claude conversations are not restored", yes); e != nil {
				return e
			}
			result, e := c.Remote(server).RestoreSnapshot(cmd.Context(), &pb.RestoreSnapshotRequest{SnapshotId: args[0], NewSessionName: name, RequestId: a.id()})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	restore.Flags().StringVar(&name, "name", "", "Required new session name")
	restore.Flags().BoolVar(&yes, "yes", false, "Confirm restoring configuration into a new session")
	root.AddCommand(restore)
	var deleteYes bool
	remove := &cobra.Command{Use: "delete-snapshot <snapshot-id>", Args: cobra.ExactArgs(1), Short: "Remove a saved configuration snapshot while retaining files and sessions", RunE: func(cmd *cobra.Command, args []string) error {
		if !a.interactive() && !deleteYes {
			return errors.New("delete-snapshot requires --yes in non-interactive mode")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, e := a.selected(cmd.Context(), c)
			if e != nil {
				return e
			}
			if project != "" {
				if e = checkSnapshotProject(cmd, c, server, project, args[0]); e != nil {
					return e
				}
			}
			if e = a.confirm(cmd.Context(), "Delete configuration snapshot "+safeText(args[0]), deleteYes); e != nil {
				return e
			}
			result, e := c.Remote(server).DeleteSnapshot(cmd.Context(), &pb.DeleteSnapshotRequest{SnapshotId: args[0]})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	remove.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deleting this snapshot record")
	root.AddCommand(remove)
}
func checkSnapshotProject(cmd *cobra.Command, c *client.Client, server, project, id string) error {
	list, e := c.Remote(server).ListSnapshots(cmd.Context(), &pb.ListSnapshotsRequest{ProjectId: project})
	if e != nil {
		return e
	}
	for _, snapshot := range list.Snapshots {
		if snapshot.Id == id {
			return nil
		}
	}
	return fmt.Errorf("snapshot %q does not belong to --project or no longer exists", id)
}
