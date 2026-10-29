package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func canSmartSync(agreement *pb.ProtocolAgreement) bool {
	return slices.Contains(agreement.GetCapabilities(), "sync.revision.v1") && smartGrant(agreement, "file.read") && smartGrant(agreement, "file.write") && smartGrant(agreement, "file.watch")
}

// setupSmartSync is invoked only for a newly, explicitly created paused mapping.
// It uses the same revisioned mutations as sync resume/pause/resolve. Neither
// plugin ownership nor reconciliation is moved into the frontend.
func (a *app) setupSmartSync(ctx context.Context, c *client.Client, mapping *ppb.Mapping, agreement *pb.ProtocolAgreement) (*ppb.Mapping, error) {
	fmt.Fprintf(a.errorOutput, "Mapping %s · server %s · project %s\nLocal directory: %s\nSynchronization is paused.\n", safeText(mapping.Id), safeText(mapping.ServerId), safeText(mapping.ProjectId), safeText(mapping.LocalRoot))
	if !canSmartSync(agreement) {
		fmt.Fprintln(a.errorOutput, "Background sync requires sync.revision.v1 and file.read/file.write/file.watch. Continuing with the paused mapping.")
		return mapping, nil
	}
	selected, err := a.choose(ctx, "Initial synchronization", []string{"Continue without sync (keep mapping paused)", "Enable background sync and review remote → local choices", "Enable background sync and review local → remote choices", "Cancel setup (keep mapping paused)"})
	if err != nil {
		return mapping, err
	}
	if selected == 0 {
		return mapping, nil
	}
	if selected == 3 {
		return mapping, errors.New("setup canceled; mapping remains paused")
	}
	plugins, err := c.Agent().ListPlugins(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
	if err != nil {
		return mapping, err
	}
	ready := false
	for _, plugin := range plugins.Plugins {
		if plugin.PluginId == "sync" && plugin.Enabled && plugin.State == "ready" {
			ready = true
		}
	}
	if !ready {
		return mapping, errors.New("local sync plugin is not ready; mapping remains paused. Inspect shiki plugins status sync --side local, then explicitly enable it if needed")
	}
	fmt.Fprintln(a.errorOutput, "The first scan preserves every existing difference as a per-file choice. The selected direction is only a review preference; no initial file is overwritten without its own confirmation. Once enabled, established baselines synchronize future changes in the background, including after detach.")
	if err = a.confirm(ctx, "Enable background sync for this mapping and inspect the initial differences", false); err != nil {
		return mapping, err
	}
	next := proto.Clone(mapping).(*ppb.Mapping)
	next.SyncEnabled = true
	next, err = c.Agent().PutMapping(ctx, &apb.PutMappingRequest{Mapping: next, ExpectedRevision: mapping.Revision, RequestId: a.smartRequestID("enable:" + mapping.Id + ":" + mapping.Revision)})
	if err != nil {
		return a.smartMappingFailure(ctx, c, mapping, err)
	}
	mapping = next
	state, err := awaitInitialSync(ctx, c, mapping)
	if err != nil {
		fmt.Fprintf(a.errorOutput, "Sync enablement was accepted; review status with shiki sync status %s or pause with shiki sync pause %s.\n", safeText(mapping.Id), safeText(mapping.Id))
		return mapping, err
	}
	fmt.Fprintf(a.errorOutput, "Observed sync state: %s · pending %d · conflicts %d · %s\n", safeText(state.State), state.PendingFiles, state.ConflictCount, safeText(state.ReasonCode))
	conflicts, err := c.Agent().ListConflicts(ctx, mappingScope(mapping))
	if err != nil {
		return mapping, err
	}
	if len(conflicts.Conflicts) == 0 {
		fmt.Fprintln(a.errorOutput, "The observed initial scan has no pending per-file choices. Background synchronization remains enabled.")
		return mapping, nil
	}
	preference := "keep-remote"
	if selected == 2 {
		preference = "keep-local"
	}
	for _, conflict := range conflicts.Conflicts {
		fmt.Fprintf(a.errorOutput, "\nPath: %s · local: %s · remote: %s · generation: %s\n", safeText(conflict.Path), conflictVersionLabel(conflict.Local), conflictVersionLabel(conflict.Remote), safeText(conflict.Generation))
		for {
			choice, err := a.choose(ctx, "Review file", []string{"Leave this file unresolved (preserve both observed versions)", "Review " + preference, "Review keep-both", "View diff", "Pause background sync and finish setup"})
			if err != nil {
				return mapping, err
			}
			if choice == 0 {
				break
			}
			if choice == 4 {
				return a.pauseSmartSync(ctx, c, mapping)
			}
			current, err := findConflict(ctx, c, mapping, conflict.Id, conflict.Generation)
			if err != nil {
				return mapping, err
			}
			if choice == 3 {
				if err = a.showConflictDiff(ctx, c, current); err != nil {
					return mapping, err
				}
				continue
			}
			decision := preference
			if choice == 2 {
				decision = "keep-both"
			}
			if err = a.confirm(ctx, conflictDecisionLabel(current, decision), false); err != nil {
				return mapping, err
			}
			resolved, _ := conflictChoice(decision)
			state, err = c.Agent().ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: current.Id, ExpectedGeneration: current.Generation, Choice: resolved, RequestId: a.smartRequestID("resolve:" + current.Id + ":" + current.Generation + ":" + decision)})
			if err != nil {
				return mapping, err
			}
			fmt.Fprintf(a.errorOutput, "Decision accepted for %s; observed sync state %s, %d conflicts remain.\n", safeText(current.Path), safeText(state.State), state.ConflictCount)
			break
		}
	}
	fmt.Fprintf(a.errorOutput, "Background sync remains enabled. Inspect current per-file choices with shiki sync conflicts %s.\n", safeText(mapping.Id))
	return mapping, nil
}

func (a *app) pauseSmartSync(ctx context.Context, c *client.Client, mapping *ppb.Mapping) (*ppb.Mapping, error) {
	next := proto.Clone(mapping).(*ppb.Mapping)
	next.SyncEnabled = false
	paused, err := c.Agent().PutMapping(ctx, &apb.PutMappingRequest{Mapping: next, ExpectedRevision: mapping.Revision, RequestId: a.smartRequestID("pause:" + mapping.Id + ":" + mapping.Revision)})
	if err != nil {
		return a.smartMappingFailure(ctx, c, mapping, err)
	}
	fmt.Fprintln(a.errorOutput, "Background sync is paused; project files and unresolved choices remain.")
	return paused, nil
}

func (a *app) smartMappingFailure(ctx context.Context, c *client.Client, previous *ppb.Mapping, mutationErr error) (*ppb.Mapping, error) {
	// PutMapping persists desired state before worker I/O. An error does not
	// prove rollback; observe once, and never replay an uncertain mutation.
	list, err := c.Agent().ListMappings(ctx, &apb.ListMappingsRequest{ServerId: previous.ServerId})
	if err == nil {
		for _, mapping := range list.Mappings {
			if mapping.Id == previous.Id {
				fmt.Fprintf(a.errorOutput, "Sync operation failed; current saved sync_enabled=%t, revision %s. Inspect actual worker state with shiki sync status %s.\n", mapping.SyncEnabled, safeText(mapping.Revision), safeText(mapping.Id))
				return mapping, mutationErr
			}
		}
	}
	fmt.Fprintf(a.errorOutput, "Sync operation outcome is unconfirmed; inspect shiki sync status %s before retrying.\n", safeText(previous.Id))
	return previous, mutationErr
}

func awaitInitialSync(ctx context.Context, c *client.Client, mapping *ppb.Mapping) (*ppb.SyncState, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		state, err := c.Agent().GetSyncState(ctx, mappingScope(mapping))
		if err != nil {
			return nil, err
		}
		if state.MappingRevision != mapping.Revision {
			return nil, errors.New("mapping revision changed during initial sync; inspect the current mapping before continuing")
		}
		switch state.State {
		case "active", "conflicted":
			return state, nil
		case "offline", "auth_required", "error", "paused":
			return nil, fmt.Errorf("initial sync unavailable: %s (%s)", safeText(state.State), safeText(state.ReasonCode))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
