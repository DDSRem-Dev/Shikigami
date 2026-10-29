package agent

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

type mappingPreflightProbe struct {
	*authenticatedProbe
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (p *mappingPreflightProbe) GetProject(ctx context.Context, in *pb.GetProjectRequest) (*pb.Project, error) {
	if p.block.CompareAndSwap(true, false) {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &pb.Project{Id: in.Id, Name: "mapping-project"}, nil
}

func mappingPreflightResources(t *testing.T) (*Service, *Resources, *mappingPreflightProbe, *apb.PutMappingRequest, *ppb.Mapping) {
	t.Helper()
	p := &mappingPreflightProbe{authenticatedProbe: &authenticatedProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}, id: "daemon_mapping", capabilities: []string{"terminal.v1"}}, entered: make(chan struct{}), release: make(chan struct{})}
	p.token.Store("shiki_mapping_test")
	server := connect.NewServer(p.authenticate)
	rpc.RegisterShikiDaemonHandler(server, p)
	ts := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	s, r := openResourceService(t, ctx, resourceConfig(t))
	t.Cleanup(func() { r.Close() })
	record, err := s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: &apb.ServerConfig{Name: "mapping-server", DirectTlsAddress: strings.TrimPrefix(ts.URL, "https://"), TlsServerName: "example.com", TlsCaFile: ca}, RequestId: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte("shiki_mapping_test"), RequestId: "credential"}); err != nil {
		t.Fatal(err)
	}
	request := &apb.PutMappingRequest{RequestId: "create", Mapping: &ppb.Mapping{ServerId: record.Config.Id, ProjectId: "proj_mapping", LocalRoot: t.TempDir()}}
	mapping, err := s.PutMapping(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	return s, r, p, request, mapping
}

func TestMappingPreflightDoesNotLockResourceMutation(t *testing.T) {
	for _, mutation := range []string{"remove", "revise", "server"} {
		t.Run(mutation, func(t *testing.T) {
			s, r, probe, _, mapping := mappingPreflightResources(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			probe.block.Store(true)
			release := sync.OnceFunc(func() { close(probe.release) })
			defer release()
			result := make(chan error, 1)
			go func() {
				_, err := s.PutMapping(ctx, &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: mapping.Revision, RequestId: "delayed"})
				result <- err
			}()
			select {
			case <-probe.entered:
			case <-ctx.Done():
				t.Fatal("remote project preflight did not start", ctx.Err())
			}
			changed := make(chan error, 1)
			go func() {
				var err error
				switch mutation {
				case "remove":
					_, err = s.RemoveMapping(ctx, &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: mapping.Revision, ArchivePending: true, RequestId: "remove"})
				case "revise":
					_, err = s.PutMapping(ctx, &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: mapping.Revision, RequestId: "revise"})
				case "server":
					// Read the pinned revision directly: findServer would itself
					// require the lock whose availability this test is checking.
					var snapshotErr error
					snapshot, snapshotErr := r.store.View()
					if snapshotErr != nil {
						changed <- snapshotErr
						return
					}
					record, readErr := storedServer(snapshot, mapping.ServerId)
					if readErr != nil {
						changed <- readErr
						return
					}
					record.Config.Name = "renamed-server"
					_, err = s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: record.Config, ExpectedRevision: record.Revision, RequestId: "rename"})
				}
				changed <- err
			}()
			select {
			case err := <-changed:
				if err != nil {
					t.Fatal("independent mutation failed", err)
				}
			case <-time.After(time.Second):
				release()
				<-changed
				<-result
				t.Fatal("blocked remote preflight held Resources.mu")
			}
			release()
			if err := <-result; err == nil {
				t.Fatal("obsolete remote preflight committed after concurrent mutation")
			}
			snapshot, err := r.store.View()
			if err != nil {
				t.Fatal(err)
			}
			if _, committed := snapshot.Requests["PutMapping\x00delayed"]; committed {
				t.Fatal("obsolete preflight was recorded as committed")
			}
		})
	}
}

func TestMappingReplayDoesNotRequireRemotePreflight(t *testing.T) {
	s, _, probe, request, original := mappingPreflightResources(t)
	probe.block.Store(true)
	defer close(probe.release)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	replayed, err := s.PutMapping(ctx, request)
	if err != nil || !proto.Equal(original, replayed) {
		t.Fatal("committed mapping replay depended on remote project", err)
	}
	select {
	case <-probe.entered:
		t.Fatal("replayed request contacted remote project")
	default:
	}
	changed := proto.Clone(request).(*apb.PutMappingRequest)
	changed.Mapping.SessionHints = map[string]string{"subdir": "changed"}
	if _, err = s.PutMapping(ctx, changed); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("reused request ID did not reject changed request locally", err)
	}
}
