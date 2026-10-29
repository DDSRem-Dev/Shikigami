//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type smartSyncProbe struct {
	*managementProbe
	guard         sync.Mutex
	changes       []*apb.PutMappingRequest
	decisions     []*ppb.ResolveConflictRequest
	current       *ppb.Mapping
	conflict      *ppb.Conflict
	stale         bool
	failAfterSave bool
	lists         int
}

func (p *smartSyncProbe) ListPlugins(context.Context, *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	return &pb.ListPluginsResponse{Plugins: []*pb.PluginStatus{{PluginId: "sync", Enabled: true, State: "ready"}}}, nil
}
func (p *smartSyncProbe) PutMapping(_ context.Context, r *apb.PutMappingRequest) (*ppb.Mapping, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	if r.ExpectedRevision != p.current.Revision || r.RequestId == "" {
		return nil, errors.New("wrong mapping revision/request ID")
	}
	p.changes = append(p.changes, proto.Clone(r).(*apb.PutMappingRequest))
	p.current = proto.Clone(r.Mapping).(*ppb.Mapping)
	p.current.Revision += "-next"
	if p.failAfterSave {
		return nil, errors.New("worker apply failed after persistence")
	}
	return proto.Clone(p.current).(*ppb.Mapping), nil
}
func (p *smartSyncProbe) ListMappings(context.Context, *apb.ListMappingsRequest) (*ppb.MappingList, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	return &ppb.MappingList{Mappings: []*ppb.Mapping{proto.Clone(p.current).(*ppb.Mapping)}}, nil
}
func (p *smartSyncProbe) GetSyncState(_ context.Context, r *ppb.Scope) (*ppb.SyncState, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	if r.MappingId != p.current.Id || r.ProjectId != p.current.ProjectId || r.ServerId != p.current.ServerId {
		return nil, errors.New("wrong mapping scope")
	}
	return &ppb.SyncState{MappingId: p.current.Id, MappingRevision: p.current.Revision, State: "conflicted", ConflictCount: 1}, nil
}
func (p *smartSyncProbe) ListConflicts(context.Context, *ppb.Scope) (*ppb.ConflictList, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	p.lists++
	conflict := proto.Clone(p.conflict).(*ppb.Conflict)
	if p.stale && p.lists > 1 {
		conflict.Generation = "changed"
	}
	return &ppb.ConflictList{Conflicts: []*ppb.Conflict{conflict}}, nil
}
func (p *smartSyncProbe) ResolveConflict(_ context.Context, r *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	if r.ExpectedGeneration != p.conflict.Generation || r.ConflictId != p.conflict.Id || r.MappingId != p.current.Id || r.RequestId == "" {
		return nil, errors.New("wrong conflict generation/scope/request ID")
	}
	p.decisions = append(p.decisions, proto.Clone(r).(*ppb.ResolveConflictRequest))
	return &ppb.SyncState{MappingId: p.current.Id, MappingRevision: p.current.Revision, State: "active"}, nil
}
func TestSmartInitialSyncExplicitDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, answers                          string
		puts, decisions                        int
		enabled, wantErr, stale, failAfterSave bool
	}{
		{name: "skip", answers: "1\n"},
		{name: "decline enable", answers: "2\nno\n", wantErr: true},
		{name: "keep remote", answers: "2\nyes\n2\nyes\n", puts: 1, decisions: 1, enabled: true},
		{name: "pause review", answers: "2\nyes\n5\n", puts: 2},
		{name: "stale decision", answers: "2\nyes\n2\n", puts: 1, enabled: true, wantErr: true, stale: true},
		{name: "persisted before worker failure", answers: "2\nyes\n", puts: 1, enabled: true, wantErr: true, failAfterSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapping := &ppb.Mapping{Id: "map", ServerId: "server", ProjectId: "project", LocalRoot: t.TempDir(), Revision: "revision"}
			probe := &smartSyncProbe{managementProbe: &managementProbe{}, current: proto.Clone(mapping).(*ppb.Mapping), stale: tc.stale, failAfterSave: tc.failAfterSave, conflict: &ppb.Conflict{Id: "conflict", MappingId: "map", Path: "file.txt", Generation: "generation", Local: &ppb.FileVersion{Exists: true}, Remote: &ppb.FileVersion{Exists: true}}}
			path := managementFixtureHandlers(t, probe, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			c, err := client.Open(ctx, path, client.Options{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			fd := int(master.Fd())
			if err = unix.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			a := &app{input: slave, output: slave, errorOutput: slave}
			done := make(chan error, 1)
			go func() {
				_, err := a.setupSmartSync(ctx, c, mapping, &pb.ProtocolAgreement{DaemonId: "daemon", Capabilities: []string{"sync.revision.v1"}, GrantedPermissions: []string{"file.read", "file.write", "file.watch"}})
				done <- err
			}()
			smartReadPTY(t, ctx, fd, "Initial synchronization:")
			if _, err = unix.Write(fd, []byte(tc.answers)); err != nil {
				t.Fatal(err)
			}
			if tc.failAfterSave {
				smartReadPTY(t, ctx, fd, "current saved sync_enabled=true")
			}
			select {
			case err = <-done:
				if (err != nil) != tc.wantErr {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			probe.guard.Lock()
			defer probe.guard.Unlock()
			if len(probe.changes) != tc.puts || len(probe.decisions) != tc.decisions || probe.current.SyncEnabled != tc.enabled {
				t.Fatalf("unexpected sync mutations: changes=%v decisions=%v current=%v", probe.changes, probe.decisions, probe.current)
			}
			if tc.decisions > 0 && probe.decisions[0].Choice != ppb.ResolveConflictRequest_KEEP_REMOTE {
				t.Fatal("review direction did not govern explicit per-file decision")
			}
			if tc.stale && !strings.Contains(err.Error(), "generation changed") {
				t.Fatal(err)
			}
		})
	}
}
