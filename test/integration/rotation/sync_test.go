package rotation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/pluginhost"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
)

// Mapping ownership is a fixture, while every file operation crosses the
// production CoreAPI -> daemon adapter -> real server worker process chain.
type syncCore struct {
	host.RemoteCore
	mappings *pluginhost.Core
}

func (c *syncCore) GetMapping(ctx context.Context, scope *ppb.Scope) (*ppb.Mapping, error) {
	return c.mappings.GetMapping(ctx, scope)
}
func (c *syncCore) ListMappings(ctx context.Context, scope *ppb.Scope) (*ppb.MappingList, error) {
	return c.mappings.ListMappings(ctx, scope)
}
func (c *syncCore) SubscribeEvents(ctx context.Context, in *ppb.SubscribeRequest, out prpc.CoreAPISubscribeEventsServerStream) error {
	return c.mappings.SubscribeEvents(ctx, in, out)
}
func (c *syncCore) PublishEvent(ctx context.Context, in *ppb.PublishEventRequest) (*pb.Empty, error) {
	return c.mappings.PublishEvent(ctx, in)
}
func (c *syncCore) Log(context.Context, *pb.LogRecord) (*pb.Empty, error) { return &pb.Empty{}, nil }

func testSyncRotation(t *testing.T, root string, artifact binary) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	local, remote := filepath.Join(root, "local"), filepath.Join(root, "remote")
	for _, dir := range []string{local, remote} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	baseline := bytes.Repeat([]byte("common-baseline\x00"), 4096)
	for _, dir := range []string{local, remote} {
		if err := os.WriteFile(filepath.Join(dir, "payload.bin"), baseline, 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := startSupervisor(t, ctx, root, "sync-server", "sync", "server", artifact, nil, false, func() prpc.CoreAPIHandler { return &pluginhost.Core{Root: remote} }, nil)
	serverPID := resources.Children(t, artifact.path, 1)[0]
	api := daemonClient(t, ctx, root, server)
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local, Revision: "rev_rotation", SyncEnabled: true}
	mappings := &pluginhost.Core{}
	mappings.SetMapping(mapping)
	client := startSupervisor(t, ctx, root, "sync-client", "sync", "client", artifact, config(t, map[string]any{"scan_interval_ms": 100.0}), true, func() prpc.CoreAPIHandler {
		return &syncCore{RemoteCore: host.RemoteCore{Resolve: resolve(api)}, mappings: mappings}
	}, func(ctx context.Context, _ string, worker sdk.Lifecycle) error {
		_, err := worker.Sync().ConfigureMapping(ctx, mapping)
		return err
	})
	first := firstLaunch(t, client)
	oldID, oldPID, oldSocket := client.ReadyInstanceID("sync"), workerPID(t, artifact, serverPID), coreSocket(t, client)
	scope := &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}
	settled := func() bool {
		var state *ppb.SyncState
		err := client.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, w sdk.Lifecycle) error {
			var err error
			state, err = w.Sync().GetSyncState(ctx, scope)
			return err
		})
		return err == nil && state.State == "active" && state.ConflictCount == 0
	}
	wait(t, ctx, "initial baseline and live callbacks", func() bool {
		return settled() && first.core.events.active.Load() > 0 && first.core.files.active.Load() > 0
	})
	before := bytes.Repeat([]byte("before-rotation\x00"), 8192)
	writeAndObserve(t, ctx, local, remote, before)
	wait(t, ctx, "pre-expiry upload acknowledgement and baseline commit", settled)
	if client.ReadyInstanceID("sync") != oldID {
		t.Fatal("first credential expired before exercised transfer")
	}
	if time.Until(first.started.Add(firstLifetime)) < time.Second {
		t.Fatal("insufficient lifetime left to establish pre-expiry evidence")
	}
	rotation(t, ctx, client, oldID, first, oldPID, oldSocket, &first.core.events, &first.core.files)
	wait(t, ctx, "reconciled replacement sync state", settled)
	writeAndObserve(t, ctx, local, remote, bytes.Repeat([]byte("replacement-local\x00"), 8192))
	// Visible destination bytes precede RPC acknowledgement/baseline commit.
	// GetSyncState waits for the worker's reconciliation lock, so the next
	// opposite-side edit is a sequential write, not an accidental conflict.
	wait(t, ctx, "replacement upload acknowledgement and baseline commit", settled)
	writeAndObserve(t, ctx, remote, local, bytes.Repeat([]byte("replacement-remote\x00"), 8192))
	wait(t, ctx, "replacement sync settled without conflicts", settled)
	remainingPIDs := resources.Children(t, artifact.path, 2)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	resources.Reaped(t, remainingPIDs...)
	t.Log("new sync instance reconciled unchanged baseline and completed fresh writes in both directions with SHA-256 equality")
}

func writeAndObserve(t *testing.T, ctx context.Context, source, destination string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	wait(t, ctx, "synchronized file hash", func() bool {
		got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
		return err == nil && sha256.Sum256(got) == want
	})
}
