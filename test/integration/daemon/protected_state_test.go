package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

const browserCookiePath = "profiles/profile/chrome/Cookies"

// Persist an association an older daemon permitted. The current daemon must
// continue serving safe projects while denying this existing association at
// each CoreAPI resolution, including calls made by the real sync subprocess.
func seedLegacyBrowserAssociation(t *testing.T, cfg *config.Daemon) *pb.Project {
	t.Helper()
	root := filepath.Join(cfg.StateDir, "plugin-data", "browser")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, browserCookiePath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, browserCookiePath), []byte("private browser cookies"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".shikiignore"), []byte("!**\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(cfg.StateDir, storage.Options{Validate: storage.ValidateDaemon(cfg.RunUser)})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	legacy, err := project.New(state, cfg.ProjectsDir, cfg.RunUser)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	p, err := legacy.Create(t.Context(), &pb.CreateProjectRequest{Name: "legacy-browser-private", Path: root, RequestId: "legacy-browser-private"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkProtectedBrowserFileAdmission(t *testing.T, ctx context.Context, client rpc.ShikiDaemonClient, p *pb.Project) {
	t.Helper()
	if value, err := client.GetProject(ctx, &pb.GetProjectRequest{Id: p.Id}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("protected project resolved through consumer RPC", value, err)
	}
	for _, path := range []string{"", "profiles/profile/chrome"} {
		if value, err := client.ListProjectDirectories(ctx, &pb.ListProjectDirectoriesRequest{ProjectId: p.Id, Path: path}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
			t.Fatal("consumer directory picker traversed browser state", value, err)
		}
	}
	if value, err := client.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: p.Id, Path: "."}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("sync file inventory admitted protected browser state", value, err)
	}
	if value, err := client.StatFile(ctx, &pb.StatFileRequest{ProjectId: p.Id, Path: browserCookiePath}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("sync stat admitted protected browser state", value, err)
	}
	read, err := client.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: p.Id, Path: browserCookiePath, ExpectedRevision: "untrusted"})
	if err == nil {
		_, err = read.Receive()
		_ = read.Close()
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("sync download read protected browser state", err)
	}
	watch, err := client.WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: p.Id, Path: "."})
	if err == nil {
		_, err = watch.Receive()
		_ = watch.Close()
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("sync watch admitted protected browser state", err)
	}
	write, err := client.WriteFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	empty := sha256.Sum256(nil)
	if err = write.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: p.Id, Path: "profiles/profile/chrome/injected", ContentHash: hex.EncodeToString(empty[:]), Mode: 0600}}}); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if value, err := write.CloseAndReceive(); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("sync upload admitted protected browser state", value, err)
	}
	if _, err := client.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: p.Id, Path: browserCookiePath, ExpectedRevision: "untrusted"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("sync delete admitted protected browser state", err)
	}
	if data, err := os.ReadFile(filepath.Join(p.Path, browserCookiePath)); err != nil || string(data) != "private browser cookies" {
		t.Fatal("protected browser cookie state changed", err)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "profiles/profile/chrome/injected")); !os.IsNotExist(err) {
		t.Fatal("denied upload created a browser profile file", err)
	}
}
