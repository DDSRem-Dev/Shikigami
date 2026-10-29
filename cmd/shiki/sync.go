package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

func (a *app) syncCommands() *cobra.Command {
	var mappingID string
	root := &cobra.Command{Use: "sync", Short: "Manage background mappings and explicit per-file conflict decisions", Long: "Manage synchronization through the agent. New mappings start paused. Enabling a mapping preserves existing differences as per-path conflicts with no base version (INITIAL_SYNC_CHOICE_REQUIRED). Choose keep-local, keep-remote, or keep-both explicitly for each conflict; no initial direction is selected automatically.", Args: cobra.NoArgs}
	root.PersistentFlags().StringVar(&mappingID, "mapping", "", "Mapping ID; otherwise use the most specific mapping for the current directory")
	for _, command := range a.projectCommands().Commands() {
		if command.Name() == "map" || command.Name() == "mappings" {
			root.AddCommand(command)
		}
	}
	status := &cobra.Command{Use: "status [mapping-id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			mapping, e := a.syncMapping(cmd.Context(), c, mappingArgument(mappingID, args))
			if e != nil {
				return e
			}
			result, e := c.Agent().GetSyncState(cmd.Context(), mappingScope(mapping))
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	root.RunE = status.RunE
	root.AddCommand(status)
	for _, enabled := range []bool{false, true} {
		name := "pause"
		var aliases []string
		if enabled {
			name = "resume"
			aliases = []string{"enable"}
		}
		var expected string
		command := &cobra.Command{Use: name + " [mapping-id]", Aliases: aliases, Args: cobra.MaximumNArgs(1), Short: "Persist mapping state; resume reconciles before any transfer", RunE: func(cmd *cobra.Command, args []string) error {
			return a.withClient(cmd.Context(), true, func(c *client.Client) error {
				mapping, e := a.syncMapping(cmd.Context(), c, mappingArgument(mappingID, args))
				if e != nil {
					return e
				}
				revision := expected
				if revision == "" {
					revision = mapping.Revision
				}
				next := proto.Clone(mapping).(*ppb.Mapping)
				next.SyncEnabled = enabled
				result, e := c.Agent().PutMapping(cmd.Context(), &apb.PutMappingRequest{Mapping: next, ExpectedRevision: revision, RequestId: a.id()})
				if e != nil {
					return e
				}
				return a.print(result)
			})
		}}
		command.Flags().StringVar(&expected, "expected-revision", "", "Require this observed mapping revision")
		root.AddCommand(command)
	}
	var archive, yes bool
	var expected string
	remove := &cobra.Command{Use: "unmap [mapping-id]", Args: cobra.MaximumNArgs(1), Short: "Remove the mapping record while retaining source files", RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			mapping, e := a.syncMapping(cmd.Context(), c, mappingArgument(mappingID, args))
			if e != nil {
				return e
			}
			if e = a.confirm(cmd.Context(), "Remove mapping "+safeText(mapping.Id)+" (source files remain)", yes); e != nil {
				return e
			}
			revision := expected
			if revision == "" {
				revision = mapping.Revision
			}
			result, e := c.Agent().RemoveMapping(cmd.Context(), &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: revision, RequestId: a.id(), ArchivePending: archive})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	remove.Flags().BoolVar(&archive, "archive-pending", false, "Explicitly archive pending synchronization and conflicts")
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm removing the mapping")
	remove.Flags().StringVar(&expected, "expected-revision", "", "Require this observed mapping revision")
	root.AddCommand(remove)
	root.AddCommand(&cobra.Command{Use: "conflicts [mapping-id]", Args: cobra.MaximumNArgs(1), Short: "List versioned per-path choices, including initial-sync differences", RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			mapping, e := a.syncMapping(cmd.Context(), c, mappingArgument(mappingID, args))
			if e != nil {
				return e
			}
			result, e := c.Agent().ListConflicts(cmd.Context(), mappingScope(mapping))
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}})
	var generation, choice string
	var confirm bool
	resolve := &cobra.Command{Use: "resolve <conflict-id>", Args: cobra.ExactArgs(1), Short: "Apply one explicit decision to exactly the observed conflict generation", RunE: func(cmd *cobra.Command, args []string) error {
		selected, e := conflictChoice(choice)
		if e != nil {
			return e
		}
		if generation == "" {
			return errors.New("--generation is required; copy it from sync conflicts")
		}
		if !a.interactive() && !confirm {
			return errors.New("sync resolve changes files and requires --yes in non-interactive mode")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			mapping, e := a.syncMapping(cmd.Context(), c, mappingID)
			if e != nil {
				return e
			}
			conflict, e := findConflict(cmd.Context(), c, mapping, args[0], generation)
			if e != nil {
				return e
			}
			if e = a.confirm(cmd.Context(), conflictDecisionLabel(conflict, choice), confirm); e != nil {
				return e
			}
			result, e := c.Agent().ResolveConflict(cmd.Context(), &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: generation, Choice: selected, RequestId: a.id()})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	resolve.Flags().StringVar(&generation, "generation", "", "Required generation from sync conflicts; stale choices are rejected")
	resolve.Flags().StringVar(&choice, "choice", "", "Required: keep-local, keep-remote, or keep-both")
	resolve.Flags().BoolVar(&confirm, "yes", false, "Confirm this data-changing conflict decision")
	root.AddCommand(resolve)
	var diffGeneration string
	diff := &cobra.Command{Use: "show-diff <conflict-id>", Args: cobra.ExactArgs(1), Short: "Compare the observed versions using private temporary files; deleted sides are explicit", RunE: func(cmd *cobra.Command, args []string) error {
		if diffGeneration == "" {
			return errors.New("--generation is required; copy it from sync conflicts")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			mapping, e := a.syncMapping(cmd.Context(), c, mappingID)
			if e != nil {
				return e
			}
			conflict, e := findConflict(cmd.Context(), c, mapping, args[0], diffGeneration)
			if e != nil {
				return e
			}
			return a.showConflictDiff(cmd.Context(), c, conflict)
		})
	}}
	diff.Flags().StringVar(&diffGeneration, "generation", "", "Required generation from sync conflicts")
	root.AddCommand(diff)
	return root
}
func mappingArgument(flag string, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return flag
}
func mappingScope(m *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: m.ServerId, ProjectId: m.ProjectId, MappingId: m.Id}
}
func (a *app) syncMapping(ctx context.Context, c *client.Client, id string) (*ppb.Mapping, error) {
	serverID := ""
	if a.server != "" {
		server, e := c.ResolveServer(ctx, a.server)
		if e != nil {
			return nil, e
		}
		serverID = server.Config.Id
	}
	list, e := c.Agent().ListMappings(ctx, &apb.ListMappingsRequest{ServerId: serverID})
	if e != nil {
		return nil, e
	}
	if id != "" {
		for _, mapping := range list.Mappings {
			if mapping.Id == id {
				return mapping, nil
			}
		}
		return nil, fmt.Errorf("mapping %q was not found", id)
	}
	cwd, e := os.Getwd()
	if e != nil {
		return nil, e
	}
	mapping, _, e := client.MappingForDirectory(cwd, list.Mappings)
	if e != nil {
		return nil, e
	}
	if mapping == nil {
		return nil, errors.New("no mapping for this directory; use --mapping or sync map")
	}
	return mapping, nil
}
func conflictChoice(value string) (ppb.ResolveConflictRequest_Choice, error) {
	switch value {
	case "keep-local":
		return ppb.ResolveConflictRequest_KEEP_LOCAL, nil
	case "keep-remote":
		return ppb.ResolveConflictRequest_KEEP_REMOTE, nil
	case "keep-both":
		return ppb.ResolveConflictRequest_KEEP_BOTH, nil
	default:
		return ppb.ResolveConflictRequest_CHOICE_UNSPECIFIED, errors.New("--choice must be keep-local, keep-remote, or keep-both")
	}
}
func findConflict(ctx context.Context, c *client.Client, mapping *ppb.Mapping, id, generation string) (*ppb.Conflict, error) {
	list, e := c.Agent().ListConflicts(ctx, mappingScope(mapping))
	if e != nil {
		return nil, e
	}
	for _, conflict := range list.Conflicts {
		if conflict.Id == id {
			if conflict.Generation != generation {
				return nil, errors.New("conflict generation changed; review sync conflicts again")
			}
			return conflict, nil
		}
	}
	return nil, errors.New("conflict no longer exists; refresh sync conflicts")
}
func conflictDecisionLabel(c *ppb.Conflict, choice string) string {
	label := choice + " for " + safeText(c.Path)
	if c.Base == nil {
		label += " (initial-sync choice)"
	}
	if choice == "keep-local" && !c.GetLocal().GetExists() {
		label += "; the local version is deleted, so the remote file will be deleted"
	}
	if choice == "keep-remote" && !c.GetRemote().GetExists() {
		label += "; the remote version is deleted, so the local file will be deleted"
	}
	return label
}

func conflictVersionLabel(version *ppb.FileVersion) string {
	if version == nil {
		return "unknown"
	}
	if !version.Exists {
		return "deleted"
	}
	return "exists"
}
