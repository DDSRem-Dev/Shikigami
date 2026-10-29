package daemon_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func TestDaemonInstalledSyncPluginTLSLifecycle(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("daemon requires an ordinary user")
	}
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	root, e := os.MkdirTemp("/tmp", "shiki-sp-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	artifacts := filepath.Join(root, "plugins")
	if e = os.Mkdir(artifacts, 0700); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(artifacts, "shiki-plugin-sync")
	build := exec.Command("go", "build", "-race", "-o", binary, "./plugins/sync")
	build.Dir = filepath.Join("..", "..", "..")
	if output, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build sync worker: %v\n%s", e, output)
	}
	data, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(data)
	contract, e := sdk.BuiltinProtocol("sync", "server")
	if e != nil {
		t.Fatal(e)
	}
	manifest := host.Manifest{Name: "sync", Version: buildinfo.Version, Executable: "shiki-plugin-sync", SHA256: hex.EncodeToString(hash[:]), Side: "server", Protocol: contract}
	saveManifest := func() {
		t.Helper()
		data, e := json.Marshal(manifest)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(artifacts, "sync.manifest.json"), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	saveManifest()
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certServer.TLS.Certificates[0]
	certServer.Close()
	certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	key, e := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(certificate.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	account, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	cfg := config.DefaultDaemon(account.Username, filepath.Join(root, "state"))
	cfg.ProjectsDir = filepath.Join(root, "projects")
	cfg.PluginsDir = artifacts
	cfg.Tailscale.Enabled = false
	cfg.Claude.Executable = "/bin/sh"
	cfg.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: certPath, KeyFile: keyPath}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	runtime, e := server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: buildinfo.Version})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if runtime != nil {
			runtime.Close()
		}
	}()
	token, _, e := runtime.Service.Auth.Create(account.Username, []string{"*"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	connectRPC := func(token string) (rpc.ShikiDaemonClient, *connection.Endpoint) {
		t.Helper()
		endpoint, e := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots})
		if e != nil {
			t.Fatal(e)
		}
		credential, e := connection.BearerToken(token)
		if e != nil {
			t.Fatal(e)
		}
		return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential)), endpoint
	}
	client, endpoint := connectRPC(token)
	defer endpoint.Close()
	if _, e = client.StatFile(ctx, &pb.StatFileRequest{ProjectId: "proj_missing", Path: "file"}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled sync did not fail closed: %v", e)
	}
	project, e := client.CreateProject(ctx, &pb.CreateProjectRequest{Name: "sync-test", Path: filepath.Join(cfg.ProjectsDir, "work"), RequestId: "project"})
	if e != nil {
		t.Fatal(e)
	}
	stats, e := client.GetStats(ctx, &pb.StatsRequest{})
	if e != nil || stats.MemoryUsedBytes == nil || stats.DiskFreeBytes == nil {
		t.Fatalf("real daemon resource statistics: %v %v", stats, e)
	}
	statsCtx, cancelStats := context.WithCancel(ctx)
	statsStream, e := client.WatchStats(statsCtx, &pb.StatsRequest{IntervalSeconds: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = statsStream.Receive(); e != nil {
		t.Fatal(e)
	}
	cancelStats()
	if _, e = statsStream.Receive(); e == nil {
		t.Fatal("stats stream ignored cancellation")
	}
	statsStream.Close()
	status := daemonPluginStatus(t, ctx, client, "sync")
	accepted, e := client.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: status.Revision, RequestId: "enable"})
	if e != nil || accepted.State != "starting" {
		t.Fatalf("enable: %v %v", accepted, e)
	}
	ready := awaitDaemonPlugin(t, ctx, client, "sync", "ready")
	info, e := client.GetSystemInfo(ctx, &pb.Empty{})
	if e != nil || !slices.Contains(info.Capabilities, sdk.FileCapability) || !slices.Contains(info.Capabilities, "plugin.manage.v1") {
		t.Fatalf("dynamic capabilities: %v %v", info, e)
	}
	content := []byte("through authenticated Connect TLS and native gRPC\n")
	written := writeDaemonFile(t, ctx, client, project.Id, "file.txt", "", content)
	stream, e := client.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: project.Id, Path: "file.txt", ExpectedRevision: written.Revision})
	if e != nil {
		t.Fatal(e)
	}
	first, e := stream.Receive()
	if e != nil || first.GetInfo().GetRevision() != written.Revision {
		t.Fatalf("read metadata: %v %v", first, e)
	}
	var read []byte
	for {
		frame, e := stream.Receive()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		read = append(read, frame.GetChunk()...)
	}
	stream.Close()
	if !bytes.Equal(read, content) {
		t.Fatalf("file bytes changed: %q", read)
	}
	restricted, _, e := runtime.Service.Auth.Create(account.Username, []string{"file.read"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	reader, readerEndpoint := connectRPC(restricted)
	defer readerEndpoint.Close()
	if _, e = reader.StatFile(ctx, &pb.StatFileRequest{ProjectId: project.Id, Path: "file.txt"}); e != nil {
		t.Fatalf("file.read requires unrelated project permissions: %v", e)
	}
	if _, e = reader.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_DISABLE, ExpectedRevision: ready.Revision, RequestId: "denied"}); connect.CodeOf(e) != connect.CodePermissionDenied {
		t.Fatalf("unprivileged management: %v", e)
	}
	if _, e = reader.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: project.Id, Path: "file.txt", ExpectedRevision: written.Revision}); connect.CodeOf(e) != connect.CodePermissionDenied {
		t.Fatalf("unprivileged file mutation: %v", e)
	}
	outside := filepath.Join(root, "outside")
	if e = os.WriteFile(outside, []byte("private"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(project.Path, "escape")); e != nil {
		t.Fatal(e)
	}
	if _, e = client.StatFile(ctx, &pb.StatFileRequest{ProjectId: project.Id, Path: "escape"}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("symlink did not fail closed: %v", e)
	}
	if _, e = client.StatFile(ctx, &pb.StatFileRequest{ProjectId: "proj_missing", Path: "file.txt"}); connect.CodeOf(e) != connect.CodeNotFound {
		t.Fatalf("unregistered project admitted: %v", e)
	}
	watch, e := client.WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: project.Id, Path: "."})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = watch.Receive(); e != nil {
		t.Fatal(e)
	}
	disabled, e := client.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_DISABLE, ExpectedRevision: ready.Revision, RequestId: "disable"})
	if e != nil || disabled.Enabled {
		t.Fatalf("disable: %v %v", disabled, e)
	}
	stopped := make(chan error, 1)
	go func() {
		for {
			_, e := watch.Receive()
			if e != nil {
				stopped <- e
				return
			}
		}
	}()
	select {
	case e := <-stopped:
		if e == nil {
			t.Fatal("watch ended without status")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disabled plugin did not cancel active file watch")
	}
	watch.Close()
	if _, e = client.StatFile(ctx, &pb.StatFileRequest{ProjectId: project.Id, Path: "file.txt"}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled plugin fell back to core IO: %v", e)
	}
	if _, e = client.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: disabled.Revision, RequestId: "reenable"}); e != nil {
		t.Fatal(e)
	}
	awaitDaemonPlugin(t, ctx, client, "sync", "ready")
	// Runtime.Close stops the worker before closing project/store handles, and
	// durable enabled intent restores the verified worker on the next start.
	if e = runtime.Close(); e != nil {
		t.Fatal(e)
	}
	runtime = nil
	endpoint.Close()
	readerEndpoint.Close()
	pending, e := os.ReadDir(filepath.Join(cfg.StateDir, "run", "p"))
	if e != nil || len(pending) != 0 {
		t.Fatalf("worker sockets survived shutdown: %v %v", pending, e)
	}
	runtime, e = server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: buildinfo.Version})
	if e != nil {
		t.Fatal(e)
	}
	client, endpoint = connectRPC(token)
	defer endpoint.Close()
	ready = awaitDaemonPlugin(t, ctx, client, "sync", "ready")
	current, e := client.StatFile(ctx, &pb.StatFileRequest{ProjectId: project.Id, Path: "file.txt"})
	if e != nil || current.Revision != written.Revision {
		t.Fatalf("file revision did not survive host restart: %v %v", current, e)
	}
	if _, e = client.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: project.Id, Path: "file.txt", ExpectedRevision: "stale"}); connect.CodeOf(e) != connect.CodeAborted {
		t.Fatalf("stale deletion accepted: %v", e)
	}
	if _, e = client.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: project.Id, Path: "file.txt", ExpectedRevision: current.Revision}); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(project.Path, "file.txt")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("delete not applied: %v", e)
	}
	manifest.SHA256 = strings.Repeat("0", 64)
	saveManifest()
	if _, e = client.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_RESTART, ExpectedRevision: ready.Revision, RequestId: "bad-checksum"}); e != nil {
		t.Fatal(e)
	}
	awaitDaemonPlugin(t, ctx, client, "sync", "failed")
	if _, e = client.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: project.Id, Path: "."}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("changed artifact admitted or core fallback used: %v", e)
	}
	if _, e = client.ListProjects(ctx, &pb.ListProjectsRequest{}); e != nil {
		t.Fatalf("plugin failure broke daemon core: %v", e)
	}
}
func daemonPluginStatus(t *testing.T, ctx context.Context, client rpc.ShikiDaemonClient, id string) *pb.PluginStatus {
	t.Helper()
	list, e := client.ListPlugins(ctx, &pb.ListPluginsRequest{})
	if e != nil {
		t.Fatal(e)
	}
	for _, status := range list.Plugins {
		if status.PluginId == id {
			return status
		}
	}
	t.Fatalf("missing plugin %s", id)
	return nil
}
func awaitDaemonPlugin(t *testing.T, ctx context.Context, client rpc.ShikiDaemonClient, id, want string) *pb.PluginStatus {
	t.Helper()
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := daemonPluginStatus(t, timeout, client, id)
		if status.State == want {
			return status
		}
		select {
		case <-timeout.Done():
			t.Fatalf("plugin %s remained %s/%s, wanted %s", id, status.State, status.ReasonCode, want)
		case <-ticker.C:
		}
	}
}
func writeDaemonFile(t *testing.T, ctx context.Context, client rpc.ShikiDaemonClient, project, path, base string, data []byte) *pb.FileInfo {
	t.Helper()
	sum := sha256.Sum256(data)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, e := client.WriteFile(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: project, Path: path, BaseRevision: base, ContentHash: hex.EncodeToString(sum[:]), Size: int64(len(data)), Mode: 0600}}}); e != nil {
		t.Fatal(e)
	}
	if e = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: data}}); e != nil {
		t.Fatal(e)
	}
	result, e := stream.CloseAndReceive()
	if e != nil {
		t.Fatal(e)
	}
	return result
}
