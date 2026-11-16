//go:build darwin || linux

package main

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"github.com/creack/pty"
)

type startupErrorRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	err   error
	calls atomic.Int32
}

func (r *startupErrorRemote) ListProjects(context.Context, *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	r.calls.Add(1)
	return nil, r.err
}

func (r *startupErrorRemote) ListProjectSessions(context.Context, *pb.ListProjectSessionsRequest) (*pb.ListProjectSessionsResponse, error) {
	r.calls.Add(1)
	return nil, r.err
}

func TestSmartStartupIdentifiesFailedRemoteRead(t *testing.T) {
	for _, stage := range []string{"projects", "project sessions"} {
		for _, failure := range []struct {
			name string
			err  error
		}{
			{"unavailable", connect.NewError(connect.CodeUnavailable, "service unavailable")},
			{"denied", connect.NewError(connect.CodePermissionDenied, "access denied")},
			{"deadline", connect.NewError(connect.CodeDeadlineExceeded, "deadline exceeded")},
			{"canceled", connect.NewError(connect.CodeCanceled, "canceled")},
			{"protocol detail", version.MissingRemoteCapability("terminal.v1")},
		} {
			t.Run(stage+"/"+failure.name, func(t *testing.T) {
				remote := &startupErrorRemote{err: failure.err}
				probe := &firstRunProbe{managementProbe: &managementProbe{}}
				if stage == "project sessions" {
					cwd, err := os.Getwd()
					if err != nil {
						t.Fatal(err)
					}
					probe.saved = &ppb.Mapping{Id: "map", ServerId: "server", ProjectId: "proj_one", LocalRoot: cwd}
				}
				path := managementFixtureHandlers(t, probe, remote)
				master, slave, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				defer slave.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				root := newRoot(slave, slave, slave)
				root.SetArgs([]string{"--config", path})
				err = root.ExecuteContext(ctx)
				if err == nil || !strings.HasPrefix(err.Error(), "list remote "+stage+": ") {
					t.Fatalf("missing startup stage: %v", err)
				}
				if connect.CodeOf(err) != connect.CodeOf(failure.err) {
					t.Fatalf("changed failure code: %v", err)
				}
				if remote.calls.Load() != 1 || len(probe.mutations) != 0 {
					t.Fatalf("startup retried or mutated state: calls=%d mutations=%d", remote.calls.Load(), len(probe.mutations))
				}
				if failure.name == "protocol detail" {
					if got, want := version.Describe(err, "test"), version.Describe(failure.err, "test"); got != want {
						t.Fatalf("lost structured diagnostic: %q, want %q", got, want)
					}
				} else if !strings.Contains(version.Describe(err, "test"), "list remote "+stage+": ") {
					t.Fatal("CLI rendering lost the failed startup stage")
				}
			})
		}
	}
}
