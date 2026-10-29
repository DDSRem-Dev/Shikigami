// Package pluginhost supplies real private CoreAPI sockets to plugin tests.
// Host implementation dependencies stay in the test harness, outside plugins.
package pluginhost

import (
	"connectrpc.com/connect/v2"
	"context"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type Core struct {
	rpc.UnimplementedCoreAPIHandler
	mu        sync.Mutex
	mapping   *ppb.Mapping
	changed   chan struct{}
	Tree      sdk.FileService
	Root      string
	Watches   atomic.Int64
	ListCalls atomic.Int64
	Denied    atomic.Bool
}

func (c *Core) GetProject(_ context.Context, scope *ppb.Scope) (*pb.Project, error) {
	if scope.ProjectId != "proj_test" || c.Root == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, "PROJECT_NOT_GRANTED")
	}
	return &pb.Project{Id: scope.ProjectId, Path: c.Root}, nil
}

func (c *Core) SetMapping(m *ppb.Mapping) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m == nil {
		c.mapping = nil
	} else {
		c.mapping = proto.Clone(m).(*ppb.Mapping)
	}
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})
}
func (c *Core) GetMapping(_ context.Context, s *ppb.Scope) (*ppb.Mapping, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mapping == nil {
		return nil, connect.NewError(connect.CodeNotFound, "MAPPING_NOT_FOUND")
	}
	if s.MappingId != c.mapping.Id || s.ServerId != c.mapping.ServerId || s.ProjectId != c.mapping.ProjectId {
		return nil, connect.NewError(connect.CodePermissionDenied, "BAD_SCOPE")
	}
	return proto.Clone(c.mapping).(*ppb.Mapping), nil
}
func (c *Core) PublishEvent(context.Context, *ppb.PublishEventRequest) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (c *Core) RemoteListFiles(ctx context.Context, in *ppb.RemoteListFilesRequest) (*pb.ListFilesResponse, error) {
	c.ListCalls.Add(1)
	if c.Denied.Load() {
		return nil, connect.NewError(connect.CodeUnauthenticated, "EXPIRED")
	}
	return c.Tree.ListFiles(ctx, in.Request)
}
func (c *Core) RemoteStatFile(ctx context.Context, in *ppb.RemoteStatFileRequest) (*pb.FileInfo, error) {
	return c.Tree.StatFile(ctx, in.Request)
}
func (c *Core) RemoteReadFile(ctx context.Context, in *ppb.RemoteReadFileRequest, out rpc.CoreAPIRemoteReadFileServerStream) error {
	return c.Tree.ReadFile(ctx, in.Request, out.Send)
}
func (c *Core) RemoteDeleteFile(ctx context.Context, in *ppb.RemoteDeleteFileRequest) (*pb.Empty, error) {
	err := c.Tree.DeleteFile(ctx, in.Request)
	return &pb.Empty{}, err
}
func (c *Core) RemoteWatchFiles(ctx context.Context, in *ppb.RemoteWatchFilesRequest, out rpc.CoreAPIRemoteWatchFilesServerStream) error {
	c.Watches.Add(1)
	defer c.Watches.Add(-1)
	return c.Tree.WatchFiles(ctx, in.Request, out.Send)
}
func (c *Core) RemoteWriteFile(ctx context.Context, stream rpc.CoreAPIRemoteWriteFileServerStream) (*pb.FileInfo, error) {
	first, err := stream.Receive()
	if err != nil {
		return nil, err
	}
	open := first.GetOpen()
	if open == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "OPEN_REQUIRED")
	}
	initial := true
	return c.Tree.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if initial {
			initial = false
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: open.Header}}, nil
		}
		frame, err := stream.Receive()
		if err != nil {
			return nil, err
		}
		if _, ok := frame.Payload.(*ppb.RemoteWriteFrame_Chunk); !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, "CHUNK_REQUIRED")
		}
		return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: frame.GetChunk()}}, nil
	})
}
func ServeCore(t *testing.T, ctx context.Context, probe rpc.CoreAPIHandler) sdk.CoreAPI {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-sync-core-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "core.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	rpc.RegisterCoreAPIHandler(server, probe)
	httpServer := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	t.Cleanup(func() { httpServer.Close(); <-done })
	endpoint, err := connection.NewUnixClient(socket, uint32(os.Getuid()), "shiki-core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return rpc.NewCoreAPIClient(connection.NewRPCClient(endpoint))
}

func Write(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}
func Read(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func WaitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync condition timed out")
}

func (c *Core) ListMappings(context.Context, *ppb.Scope) (*ppb.MappingList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := new(ppb.MappingList)
	if c.mapping != nil {
		out.Mappings = append(out.Mappings, proto.Clone(c.mapping).(*ppb.Mapping))
	}
	return out, nil
}
func (c *Core) SubscribeEvents(ctx context.Context, _ *ppb.SubscribeRequest, stream rpc.CoreAPISubscribeEventsServerStream) error {
	for {
		c.mu.Lock()
		if c.changed == nil {
			c.changed = make(chan struct{})
		}
		changed := c.changed
		c.mu.Unlock()
		if err := stream.Send(&ppb.Event{Type: "rescan_required", Payload: &ppb.Event_RescanRequired{RescanRequired: true}}); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
