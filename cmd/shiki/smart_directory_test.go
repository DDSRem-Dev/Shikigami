//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type directoryRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu       sync.Mutex
	pages    []*pb.ListProjectDirectoriesResponse
	requests []*pb.ListProjectDirectoriesRequest
}

func (p *directoryRemote) ListProjectDirectories(_ context.Context, r *pb.ListProjectDirectoriesRequest) (*pb.ListProjectDirectoriesResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.ProjectId != "project" || r.PageSize != 50 {
		return nil, errors.New("wrong directory request")
	}
	p.requests = append(p.requests, proto.Clone(r).(*pb.ListProjectDirectoriesRequest))
	if len(p.pages) == 0 {
		return nil, errors.New("unexpected directory request")
	}
	page := p.pages[0]
	p.pages = p.pages[1:]
	return page, nil
}
func TestSmartDirectoryPickerScopePaginationAndReservedNames(t *testing.T) {
	for _, tc := range []struct {
		name, answers, want string
		pages               []*pb.ListProjectDirectoriesResponse
		wantErr             bool
	}{
		{name: "directory named use", answers: "2\n1\n", want: "use", pages: []*pb.ListProjectDirectoriesResponse{{ProjectId: "project", Directories: []string{"use"}}, {ProjectId: "project", Path: "use"}}},
		{name: "next page", answers: "2\n2\n1\n", want: "frontend", pages: []*pb.ListProjectDirectoriesResponse{{ProjectId: "project", NextPageToken: "next"}, {ProjectId: "project", Directories: []string{"frontend"}}, {ProjectId: "project", Path: "frontend"}}},
		{name: "escape path", wantErr: true, pages: []*pb.ListProjectDirectoriesResponse{{ProjectId: "project", Directories: []string{"../outside"}}}},
		{name: "wrong project", wantErr: true, pages: []*pb.ListProjectDirectoriesResponse{{ProjectId: "other"}}},
		{name: "cancel", answers: "2\n", wantErr: true, pages: []*pb.ListProjectDirectoriesResponse{{ProjectId: "project", Directories: []string{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "cancel" {
				tc.answers = "3\n"
			}
			remote := &directoryRemote{pages: tc.pages}
			cfg := managementFixture(t, &managementProbe{remote: remote})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			c, err := client.Open(ctx, cfg, client.Options{Timeout: time.Second})
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
			type result struct {
				path string
				err  error
			}
			done := make(chan result, 1)
			go func() {
				path, err := a.chooseWorkingDirectory(ctx, c, "server", "project", &pb.ProtocolAgreement{DaemonId: "daemon", Capabilities: []string{"project.directories.v1"}, GrantedPermissions: []string{"file.read"}})
				done <- result{path, err}
			}()
			if tc.answers != "" {
				smartReadPTY(t, ctx, fd, "Working directory · server server · project project:")
				if _, err = unix.Write(fd, []byte(tc.answers)); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-done:
				if (got.err != nil) != tc.wantErr || got.path != tc.want {
					t.Fatalf("got %q %v", got.path, got.err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if tc.name == "next page" && (len(remote.requests) != 3 || remote.requests[1].PageToken != "next" || remote.requests[2].Path != "frontend" || remote.requests[2].PageToken != "") {
				t.Fatal("pagination/navigation lost scope", remote.requests)
			}
		})
	}
}
