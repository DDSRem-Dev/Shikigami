package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type managementProbe struct {
	arpc.UnimplementedAgentHandler
	mu         sync.Mutex
	managed    *apb.ManagePluginRequest
	resolved   *ppb.ResolveConflictRequest
	mapping    *ppb.Mapping
	conflict   *ppb.Conflict
	data       []byte
	previewDir string
	reads      []ppb.ReadConflictFileRequest_Side
	remote     rpc.ShikiDaemonHandler
	server     *apb.ServerRecord
	logCheck   func(*apb.PluginLogsRequest) error
}

func (p *managementProbe) GetPreferences(context.Context, *pb.Empty) (*apb.Preferences, error) {
	return &apb.Preferences{DetachKey: "ctrl-q"}, nil
}

func (p *managementProbe) WatchEvents(ctx context.Context, _ *pb.Empty, stream arpc.AgentWatchEventsServerStream) error {
	if err := stream.Send(&apb.AgentEvent{InstanceId: "instance", Sequence: 1, Type: "rescan_required"}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func (p *managementProbe) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: "frontend", Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"agent.control.v1", "agent.events.v1"}}}, nil
}
func (p *managementProbe) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (p *managementProbe) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (p *managementProbe) ListPlugins(_ context.Context, target *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	if target.Side != apb.PluginTarget_LOCAL || target.ServerId != "" {
		return nil, errors.New("invalid plugin target")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	revision, state, enabled := "old", "stopped", false
	if p.managed != nil {
		revision, state, enabled = "new", "ready", true
	}
	return &pb.ListPluginsResponse{Plugins: []*pb.PluginStatus{{PluginId: "file-sync", Revision: revision, State: state, Enabled: enabled}}}, nil
}
func (p *managementProbe) ManagePlugin(_ context.Context, request *apb.ManagePluginRequest) (*pb.PluginStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if request.Request.ExpectedRevision != "old" || request.Request.RequestId != "request" {
		return nil, connect.NewError(connect.CodeAborted, "bad request revision")
	}
	p.managed = proto.Clone(request).(*apb.ManagePluginRequest)
	return &pb.PluginStatus{PluginId: "file-sync", Revision: "new", State: "starting", Enabled: true}, nil
}
func (p *managementProbe) ListMappings(context.Context, *apb.ListMappingsRequest) (*ppb.MappingList, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &ppb.MappingList{Mappings: []*ppb.Mapping{proto.Clone(p.mapping).(*ppb.Mapping)}}, nil
}
func (p *managementProbe) PutMapping(_ context.Context, request *apb.PutMappingRequest) (*ppb.Mapping, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if request.ExpectedRevision != p.mapping.Revision || request.RequestId == "" {
		return nil, connect.NewError(connect.CodeAborted, "revision")
	}
	p.mapping = proto.Clone(request.Mapping).(*ppb.Mapping)
	p.mapping.Revision = "next"
	return proto.Clone(p.mapping).(*ppb.Mapping), nil
}
func (p *managementProbe) ListConflicts(context.Context, *ppb.Scope) (*ppb.ConflictList, error) {
	return &ppb.ConflictList{Conflicts: []*ppb.Conflict{p.conflict}}, nil
}
func (p *managementProbe) ResolveConflict(_ context.Context, request *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	if request.ExpectedGeneration != p.conflict.Generation {
		return nil, connect.NewError(connect.CodeAborted, "stale")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolved = proto.Clone(request).(*ppb.ResolveConflictRequest)
	return &ppb.SyncState{MappingId: request.MappingId, State: "active"}, nil
}
func (p *managementProbe) ReadConflictFile(_ context.Context, request *ppb.ReadConflictFileRequest, stream arpc.AgentReadConflictFileServerStream) error {
	p.mu.Lock()
	p.reads = append(p.reads, request.Side)
	p.mu.Unlock()
	if request.ExpectedGeneration != p.conflict.Generation || request.Side != ppb.ReadConflictFileRequest_LOCAL {
		return connect.NewError(connect.CodeAborted, "wrong side or generation")
	}
	dirs, e := filepath.Glob(filepath.Join(p.previewDir, "shiki-diff-*"))
	if e != nil || len(dirs) != 1 {
		return errors.New("private preview directory missing")
	}
	dir, e := os.Stat(dirs[0])
	if e != nil || dir.Mode().Perm() != 0700 {
		return errors.New("preview directory is public")
	}
	file, e := os.Stat(filepath.Join(dirs[0], "local"))
	if e != nil || file.Mode().Perm() != 0600 {
		return errors.New("preview file is public")
	}
	if e = stream.Send(&pb.FileData{Payload: &pb.FileData_Info{Info: &pb.FileInfo{ContentHash: p.conflict.Local.ContentHash, Size: int64(len(p.data))}}}); e != nil {
		return e
	}
	return stream.Send(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: p.data}})
}
func managementFixture(t *testing.T, p *managementProbe) string {
	return managementFixtureHandlers(t, p, p.remote)
}
func managementFixtureHandlers(t *testing.T, p arpc.AgentHandler, remote rpc.ShikiDaemonHandler) string {
	t.Helper()
	root, e := os.MkdirTemp("/tmp", "shiki-cli-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Agent.AutoStart = false
	cfg.Tailscale.Enabled = false
	data, e := yaml.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "config.yaml")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	listener, e := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	server := connect.NewServer()
	arpc.RegisterAgentHandler(server, p)
	if remote != nil {
		rpc.RegisterShikiDaemonHandler(server, remote)
	}
	ctx, cancel := context.WithCancel(context.Background())
	httpServer := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	t.Cleanup(func() { cancel(); httpServer.Close(); <-done })
	return path
}
func runManagement(t *testing.T, path string, args ...string) (string, error) {
	t.Helper()
	var output, stderr bytes.Buffer
	root := newRoot(nil, &output, &stderr)
	root.SetArgs(append([]string{"--config", path, "--json"}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := root.ExecuteContext(ctx)
	return output.String(), e
}
func TestPluginMutationWaitsForReady(t *testing.T) {
	p := &managementProbe{}
	path := managementFixture(t, p)
	output, e := runManagement(t, path, "plugins", "enable", "file-sync", "--side", "local", "--request-id", "request")
	if e != nil {
		t.Fatal(e)
	}
	var status map[string]any
	if e = json.Unmarshal([]byte(output), &status); e != nil || status["state"] != "ready" {
		t.Fatalf("accepted starting was reported as completion: %s %v", output, e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.managed.GetRequest().GetExpectedRevision() != "old" || p.managed.Request.Action != pb.ManagePluginRequest_ENABLE {
		t.Fatalf("request: %v", p.managed)
	}
}
func TestExplicitManagementFlagsFailBeforeIPC(t *testing.T) {
	for _, args := range [][]string{{"plugins", "list"}, {"plugins", "list", "--side", "remote"}, {"plugins", "list", "--side", "both"}, {"plugins", "list", "--side", "local", "--server", "remote"}, {"sync", "resolve", "conflict", "--choice", "keep-local"}, {"sync", "resolve", "conflict", "--choice", "keep-local", "--generation", "g"}, {"sync", "show-diff", "conflict"}} {
		output, e := runManagement(t, "/does/not/exist", args...)
		if e == nil || output != "" {
			t.Fatalf("%v: %q %v", args, output, e)
		}
		if strings.Contains(e.Error(), "socket") || strings.Contains(e.Error(), "executable") {
			t.Fatalf("invalid flags touched agent: %v: %v", args, e)
		}
	}
}
func TestSyncVersionedChoicesAndPrivateDeletedDiff(t *testing.T) {
	preview := t.TempDir()
	t.Setenv("TMPDIR", preview)
	data := []byte("local contents\n")
	sum := sha256.Sum256(data)
	p := &managementProbe{mapping: &ppb.Mapping{Id: "mapping", ServerId: "server", ProjectId: "project", Revision: "old", LocalRoot: t.TempDir()}, conflict: &ppb.Conflict{Id: "conflict", MappingId: "mapping", Path: "file.txt", Generation: "generation", Local: &ppb.FileVersion{Exists: true, ContentHash: hex.EncodeToString(sum[:])}, Remote: &ppb.FileVersion{Exists: false}}, data: data, previewDir: preview}
	path := managementFixture(t, p)
	output, e := runManagement(t, path, "sync", "show-diff", "conflict", "--mapping", "mapping", "--generation", "generation")
	if e != nil {
		t.Fatal(e)
	}
	var diff map[string]any
	if e = json.Unmarshal([]byte(output), &diff); e != nil || diff["remote_exists"] != false || diff["initial_sync"] != true || !strings.Contains(fmt.Sprint(diff["diff"]), "(deleted)") {
		t.Fatalf("diff: %s %v", output, e)
	}
	leftover, e := os.ReadDir(preview)
	if e != nil || len(leftover) != 0 {
		t.Fatalf("preview retained: %v %v", leftover, e)
	}
	p.mu.Lock()
	if len(p.reads) != 1 || p.reads[0] != ppb.ReadConflictFileRequest_LOCAL {
		t.Fatalf("deleted side read: %v", p.reads)
	}
	p.mu.Unlock()
	if _, e = runManagement(t, path, "sync", "resolve", "conflict", "--mapping", "mapping", "--generation", "stale", "--choice", "keep-remote", "--yes"); e == nil {
		t.Fatal("stale choice accepted")
	}
	if _, e = runManagement(t, path, "sync", "resolve", "conflict", "--mapping", "mapping", "--generation", "generation", "--choice", "keep-remote", "--yes", "--request-id", "decision"); e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	if p.resolved.RequestId != "decision" || p.resolved.Choice != ppb.ResolveConflictRequest_KEEP_REMOTE || p.resolved.ExpectedGeneration != "generation" {
		t.Fatalf("resolve request: %v", p.resolved)
	}
	p.mu.Unlock()
	if _, e = runManagement(t, path, "sync", "resume", "mapping"); e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.mapping.SyncEnabled || p.mapping.Revision != "next" {
		t.Fatal("mapping was not revisioned")
	}
}

type sliceConflictReader struct {
	frames []*pb.FileData
	index  int
}

func (s *sliceConflictReader) Receive() (*pb.FileData, error) {
	if s.index == len(s.frames) {
		return nil, io.EOF
	}
	frame := s.frames[s.index]
	s.index++
	return frame, nil
}
func (s *sliceConflictReader) Close() error { return nil }
func TestConflictPreviewRejectsContentMismatch(t *testing.T) {
	hash := strings.Repeat("0", 64)
	stream := &sliceConflictReader{frames: []*pb.FileData{{Payload: &pb.FileData_Info{Info: &pb.FileInfo{Size: 3, ContentHash: hash}}}, {Payload: &pb.FileData_Chunk{Chunk: []byte("bad")}}}}
	if e := writeConflictPreview(io.Discard, stream, &ppb.FileVersion{Exists: true, ContentHash: hash}); e == nil {
		t.Fatal("corrupt preview accepted")
	}
}
func TestPluginWaitBoundedAndFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	starting := &pb.PluginStatus{Revision: "revision", State: "starting", Enabled: true}
	if _, e := waitForPlugin(ctx, starting, func(context.Context) (*pb.PluginStatus, error) { return starting, nil }); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("wait was not bounded: %v", e)
	}
	for _, state := range []string{"failed", "incompatible"} {
		if _, e := waitForPlugin(context.Background(), &pb.PluginStatus{Revision: "revision", State: state}, nil); e == nil {
			t.Fatalf("%s accepted as ready", state)
		}
	}
}

func TestNodeAuthKeyFileAndArgvBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth-key")
	if e := os.WriteFile(path, []byte("tskey-auth-sensitive\n"), 0600); e != nil {
		t.Fatal(e)
	}
	key, e := readPrivateSecret(path, 4096)
	if e != nil || string(key) != "tskey-auth-sensitive" {
		t.Fatalf("key not read: %v", e)
	}
	clear(key)
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = readPrivateSecret(path, 4096); e == nil {
		t.Fatal("public auth key file accepted")
	}
	output, e := runManagement(t, "/does/not/exist", "node", "login", "--auth-key", "tskey-auth-sensitive")
	if e == nil || output != "" || strings.Contains(e.Error(), "tskey-auth-sensitive") {
		t.Fatalf("argv key accepted or exposed: %q %v", output, e)
	}
}

func (p *managementProbe) ListServers(context.Context, *pb.Empty) (*apb.ServerList, error) {
	if p.server != nil {
		return &apb.ServerList{Servers: []*apb.ServerRecord{proto.Clone(p.server).(*apb.ServerRecord)}}, nil
	}
	return &apb.ServerList{Servers: []*apb.ServerRecord{{Config: &apb.ServerConfig{Id: "server", Name: "test"}}}}, nil
}
