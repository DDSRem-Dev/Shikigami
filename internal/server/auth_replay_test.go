package server

import (
	"crypto/x509"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// A persisted result is not an authorization grant. Exercise the production
// middleware, interceptor and project transaction over the same HTTP/2 client.
func TestProjectReplayRevalidatesTokenAndPermission(t *testing.T) {
	dir := t.TempDir()
	state, err := storage.Open(filepath.Join(dir, "state"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projects, err := project.New(state, filepath.Join(dir, "projects"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer projects.Close()
	authentication, err := auth.Open(filepath.Join(dir, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer authentication.Close()
	service := &Service{Auth: authentication, Projects: projects}
	remote := httptest.NewUnstartedServer(service.Handler())
	remote.EnableHTTP2 = true
	remote.StartTLS()
	defer remote.Close()
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	endpoint, err := connection.NewTLSClient(remote.URL, connection.TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	client := func(permission string) (rpc.ShikiDaemonClient, auth.TokenInfo) {
		t.Helper()
		token, info, err := authentication.Create("tester", []string{permission}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := connection.BearerToken(token)
		if err != nil {
			t.Fatal(err)
		}
		return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential)), info
	}
	creator, creatorInfo := client("project.create")
	otherCreator, _ := client("project.create")
	reader, _ := client("project.list")
	request := &pb.CreateProjectRequest{Name: "replayed", Path: filepath.Join(dir, "projects", "replayed"), RequestId: "same-request"}
	created, err := creator.CreateProject(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	// The daemon is single-user: another authorized jti shares the same durable
	// request namespace and must not create a duplicate resource.
	replayed, err := otherCreator.CreateProject(t.Context(), request)
	if err != nil || replayed.GetId() != created.Id {
		t.Fatalf("authorized retry lost original result: %v %v", replayed, err)
	}
	if _, err := reader.CreateProject(t.Context(), request); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("weaker token accessed saved success: %v", err)
	}
	if err := authentication.Revoke(creatorInfo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := creator.CreateProject(t.Context(), request); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("revoked token accessed saved success: %v", err)
	}
	changed := &pb.CreateProjectRequest{Name: "different", Path: request.Path, RequestId: request.RequestId}
	if _, err := otherCreator.CreateProject(t.Context(), changed); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("same request ID accepted different parameters: %v", err)
	}
	listed, err := reader.ListProjects(t.Context(), &pb.ListProjectsRequest{})
	if err != nil || len(listed.GetProjects()) != 1 || listed.Projects[0].Id != created.Id {
		t.Fatalf("retries changed resources or unrelated token stopped working: %v %v", listed, err)
	}
}
