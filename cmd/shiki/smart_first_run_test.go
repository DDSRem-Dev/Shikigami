//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type firstRunProbe struct {
	*managementProbe
	guard     sync.Mutex
	saved     *ppb.Mapping
	mutations []*apb.PutMappingRequest
}

func (p *firstRunProbe) ListMappings(context.Context, *apb.ListMappingsRequest) (*ppb.MappingList, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	result := &ppb.MappingList{}
	if p.saved != nil {
		result.Mappings = []*ppb.Mapping{proto.Clone(p.saved).(*ppb.Mapping)}
	}
	return result, nil
}
func (p *firstRunProbe) PutMapping(_ context.Context, r *apb.PutMappingRequest) (*ppb.Mapping, error) {
	p.guard.Lock()
	defer p.guard.Unlock()
	if r.Mapping.SyncEnabled || r.RequestId == "" {
		return nil, errors.New("first-run unexpectedly enabled sync")
	}
	if p.saved != nil && r.ExpectedRevision != p.saved.Revision {
		return nil, errors.New("wrong mapping revision")
	}
	for _, previous := range p.mutations {
		if previous.RequestId == r.RequestId {
			return nil, errors.New("smart operation reused batch request ID")
		}
	}
	p.mutations = append(p.mutations, proto.Clone(r).(*apb.PutMappingRequest))
	p.saved = proto.Clone(r.Mapping).(*ppb.Mapping)
	p.saved.Id = "map_first"
	p.saved.Revision = fmt.Sprintf("revision-%d", len(p.mutations))
	return proto.Clone(p.saved).(*ppb.Mapping), nil
}

type firstRunRemote struct{ smartRemote }

func (p *firstRunRemote) ListProjects(context.Context, *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	return &pb.ListProjectsResponse{Projects: []*pb.Project{{Id: "proj_one", Name: "project", Path: "/remote/project"}}}, nil
}
func (p *firstRunRemote) ListProjectSessions(context.Context, *pb.ListProjectSessionsRequest) (*pb.ListProjectSessionsResponse, error) {
	return &pb.ListProjectSessionsResponse{}, nil
}
func (p *firstRunRemote) GetSession(_ context.Context, r *pb.GetSessionRequest) (*pb.Session, error) {
	return &pb.Session{Id: r.Id, ProjectId: "proj_one", Name: "main", ClaudeStatus: "running"}, nil
}

func TestSmartFirstRunKeepsExplicitlySkippedSyncPaused(t *testing.T) {
	remote := &firstRunRemote{}
	probe := &firstRunProbe{managementProbe: &managementProbe{server: &apb.ServerRecord{Config: &apb.ServerConfig{Id: "server", Name: "test"}, RemoteProtocol: &pb.ProtocolAgreement{DaemonId: "daemon", Capabilities: []string{"sync.revision.v1"}, GrantedPermissions: []string{"project.create", "session.create", "session.attach", "session.write", "file.read", "file.write", "file.watch"}}}}}
	path := managementFixtureHandlers(t, probe, remote)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	fd := int(master.Fd())
	if err = unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	root := newRoot(slave, slave, slave)
	root.SetArgs([]string{"--config", path, "--request-id", "first-run-batch"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	smartReadPTY(t, ctx, fd, "Project:")
	if _, err = unix.Write(fd, []byte("2\nyes\n1\nmain\n\nFirst session\n")); err != nil {
		t.Fatal(err)
	}
	smartReadPTY(t, ctx, fd, "SMART_READY")
	if _, err = unix.Write(fd, []byte{17}); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	probe.guard.Lock()
	defer probe.guard.Unlock()
	if len(probe.mutations) != 2 || probe.saved.SyncEnabled || probe.saved.ServerId != "server" || probe.saved.ProjectId != "proj_one" || probe.saved.DefaultSession != "sess_new" {
		t.Fatal("first-run mapping lost choice or scope", probe.saved, probe.mutations)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.created == nil || remote.created.WorkingDir != "." || remote.created.Description != "First session" || remote.created.ProjectId != "proj_one" {
		t.Fatal("first session lost collected fields", remote.created)
	}
}
