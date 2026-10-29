package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type templateRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu       sync.Mutex
	requests []*pb.CreateSessionRequest
}

func (r *templateRemote) GetProject(context.Context, *pb.GetProjectRequest) (*pb.Project, error) {
	return &pb.Project{Id: "proj_fixture", Name: "remote", Path: "/remote/project"}, nil
}
func (r *templateRemote) CreateSession(_ context.Context, request *pb.CreateSessionRequest) (*pb.Session, error) {
	r.mu.Lock()
	r.requests = append(r.requests, proto.Clone(request).(*pb.CreateSessionRequest))
	r.mu.Unlock()
	if request.Name == "missing" {
		return nil, connect.NewError(connect.CodeNotFound, "remote working directory missing")
	}
	return &pb.Session{Id: "sess_" + request.Name, Name: request.Name, ProjectId: request.ProjectId, WorkingDir: "/remote/project/" + request.WorkingDir, AutoStartClaude: request.AutoStartClaude}, nil
}
func TestTemplateReportsEveryItemAndUsesRemoteRelativePaths(t *testing.T) {
	remote := &templateRemote{}
	path := managementFixture(t, &managementProbe{remote: remote})
	cfg, e := config.LoadClient(path)
	if e != nil {
		t.Fatal(e)
	}
	no := false
	cfg.Templates = map[string]config.SessionTemplate{"fullstack": {Sessions: []config.TemplateSession{{Name: "frontend", WorkingDir: "frontend"}, {Name: "missing", WorkingDir: "missing"}, {Name: "database", WorkingDir: ".", AutoStart: &no}}}}
	data, e := yaml.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"init", "--template", "fullstack", "--project", "proj_fixture", "--server", "server", "--yes", "--request-id", "batch"}
	output, e := runManagement(t, path, args...)
	if e == nil || !strings.Contains(e.Error(), "1 of 3") {
		t.Fatalf("partial failure was not reported: %s %v", output, e)
	}
	var report templateReport
	if e = json.Unmarshal([]byte(output), &report); e != nil {
		t.Fatal(e)
	}
	if len(report.Results) != 3 || report.Results[0].Session == nil || report.Results[1].ErrorCode != "not_found" || report.Results[2].Session == nil {
		t.Fatalf("items omitted: %s", output)
	}
	if _, e = runManagement(t, path, args...); e == nil {
		t.Fatal("repeated batch hid a failed item")
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if len(remote.requests) != 6 {
		t.Fatalf("batch skipped items: %d", len(remote.requests))
	}
	for i, working := range []string{"frontend", "missing", "."} {
		request := remote.requests[i]
		if request.WorkingDir != working || request.ProjectId != "proj_fixture" || request.RequestId != remote.requests[i+3].RequestId {
			t.Fatalf("wrong request: %v", request)
		}
	}
	if !remote.requests[0].GetAutoStartClaude() || remote.requests[2].GetAutoStartClaude() {
		t.Fatal("auto_start did not control Claude defaults")
	}
	if remote.requests[0].RequestId == remote.requests[1].RequestId {
		t.Fatal("batch reused one request ID for different sessions")
	}
}
func TestSnapshotAndTemplateNoninteractiveSafety(t *testing.T) {
	for _, args := range [][]string{{"sessions", "snapshot"}, {"sessions", "restore", "snap_example"}, {"sessions", "restore", "snap_example", "--name", "copy"}, {"sessions", "delete-snapshot", "snap_example"}, {"init"}, {"init", "--template", "fullstack"}, {"init", "--template", "fullstack", "--yes"}, {"init", "--template", "fullstack", "--project", "proj_example", "--path", "/remote", "--yes"}} {
		output, e := runManagement(t, "/missing/config", args...)
		if e == nil || output != "" || strings.Contains(e.Error(), "socket") || strings.Contains(e.Error(), "executable") {
			t.Fatalf("unsafe command reached IPC or produced data: %v %q %v", args, output, e)
		}
	}
}
