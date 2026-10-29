package rotation_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/types/known/structpb"
)

const firstLifetime = 12 * time.Second

type binary struct{ path, checksum string }

func TestProductionWorkerCredentialRotation(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-rotation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Config.Env is explicitly passed after the production host's environment
	// filtering. Detect child race failures even when the supervisor restarts it.
	t.Cleanup(func() {
		paths, err := filepath.Glob(filepath.Join(dir, "race.*"))
		if err != nil {
			t.Error(err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			t.Errorf("child race report %s: %v\n%s", filepath.Base(path), err, data[:min(len(data), 64<<10)])
		}
	})
	binaries := map[string]binary{}
	for _, id := range []string{"sync", "port"} {
		path := filepath.Join(dir, id)
		cmd := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", path, "../../../plugins/"+id)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", id, err, output)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		binaries[id] = binary{path, hex.EncodeToString(sum[:])}
	}
	t.Run("sync", func(t *testing.T) { testSyncRotation(t, dir, binaries["sync"]) })
	t.Run("port", func(t *testing.T) { testPortRotation(t, dir, binaries["port"]) })
}

func wait(t *testing.T, ctx context.Context, description string, predicate func() bool) {
	t.Helper()
	for {
		if predicate() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", description, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type launch struct {
	core    *trackedCore
	started time.Time
	ordinal int
}

type supervisor struct {
	*host.Supervisor
	id, runDir string
	launches   chan launch
}

func startSupervisor(t *testing.T, ctx context.Context, root, name, id, side string, artifact binary, values *structpb.Struct, rotate bool, factory func() prpc.CoreAPIHandler, ready func(context.Context, string, sdk.Lifecycle) error) *supervisor {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s := &supervisor{id: id, runDir: filepath.Join(dir, "run"), launches: make(chan launch, 8)}
	ordinal := 0
	opts := host.SupervisorOptions{StateDir: filepath.Join(dir, "intent")}
	if ready != nil {
		opts.Ready = func(ctx context.Context, id string, worker sdk.Lifecycle) error {
			err := ready(ctx, id, worker)
			if err != nil {
				t.Logf("fixture Ready %s: %v", name, err)
			}
			return err
		}
	}
	opts.Validate = func(_ string, v *structpb.Struct) error {
		if id == "sync" {
			if side == "client" {
				return schema.ValidateSyncClient(v)
			}
			return schema.ValidateSyncServer(v)
		}
		_, err := schema.Parse(v, side)
		return err
	}
	opts.Config = func(_ string, _ *structpb.Struct) (host.Config, error) {
		ordinal++
		core := &trackedCore{CoreAPIHandler: factory()}
		lifetime := time.Minute
		if rotate && ordinal == 1 {
			lifetime = firstLifetime
		}
		capability := sdk.FileCapability
		policy := host.Policy{Side: side, Scopes: []*ppb.Scope{{}, {ProjectId: "proj_test"}, {ServerId: "srv_test"}, {ServerId: "srv_test", ProjectId: "proj_test", MappingId: "map_test"}}}
		if id == "sync" && side == "client" {
			capability = sdk.SyncCapability
		}
		if id == "port" {
			capability = sdk.PortCapability
			if side == "client" {
				capability = sdk.PortWorkerCapability
			}
		}
		if side == "server" {
			policy.Capabilities = []string{"core.projects.read"}
		} else {
			policy.Capabilities = []string{"core.mappings.read", "core.events.read", "core.events.write", "core.log", "file.read", "file.write", "file.watch", "port.forward"}
		}
		s.launches <- launch{core, time.Now(), ordinal}
		return host.Config{Manifest: host.Manifest{Name: id, Version: buildinfo.Version, Executable: artifact.path, SHA256: artifact.checksum, Side: side, Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", capability}}}, Env: []string{"GORACE=halt_on_error=1 log_path=" + filepath.Join(root, "race")}, RunDir: s.runDir, StateDir: filepath.Join(dir, "worker-state"), HostInstanceID: "host_" + name, Values: values, Core: core, Policy: policy, StartupTimeout: 5 * time.Second, CredentialLifetime: lifetime}, nil
	}
	var err error
	s.Supervisor, err = host.NewSupervisor(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		entries, err := os.ReadDir(s.runDir)
		if err != nil || len(entries) != 0 {
			t.Errorf("runtime files remain: %v %v", entries, err)
		}
	})
	s.Start()
	_, revision, err := s.Settings(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Manage(ctx, &pb.ManagePluginRequest{PluginId: id, Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable"}); err != nil {
		t.Fatal(err)
	}
	wait(t, ctx, "first worker ready", func() bool { return s.ReadyInstanceID(id) != "" })
	return s
}

func daemonClient(t *testing.T, ctx context.Context, root string, s *supervisor) rpc.ShikiDaemonClient {
	t.Helper()
	socket := filepath.Join(root, s.id+"-daemon.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, &host.DaemonDataPlane{Supervisor: s.Supervisor})
	http := connection.NewHTTPServer(ctx, host.WithStreamCancellation(connection.NewRPCHandler(server)))
	done := make(chan error, 1)
	go func() { done <- http.Serve(listener) }()
	t.Cleanup(func() { http.Close(); <-done })
	endpoint, err := connection.NewUnixClient(socket, uint32(os.Getuid()), "rotation-daemon")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint))
}

type trackedCore struct {
	prpc.CoreAPIHandler
	events, files, ports streamCounts
}

func (c *trackedCore) SubscribeEvents(ctx context.Context, in *ppb.SubscribeRequest, out prpc.CoreAPISubscribeEventsServerStream) (err error) {
	finish := c.events.track(ctx, "events")
	defer func() { finish(err) }()
	return c.CoreAPIHandler.SubscribeEvents(ctx, in, out)
}
func (c *trackedCore) RemoteWatchFiles(ctx context.Context, in *ppb.RemoteWatchFilesRequest, out prpc.CoreAPIRemoteWatchFilesServerStream) (err error) {
	finish := c.files.track(ctx, "files")
	defer func() { finish(err) }()
	return c.CoreAPIHandler.RemoteWatchFiles(ctx, in, out)
}
func (c *trackedCore) RemoteForwardPort(ctx context.Context, out prpc.CoreAPIRemoteForwardPortServerStream) (err error) {
	finish := c.ports.track(ctx, "ports")
	defer func() { finish(err) }()
	return c.CoreAPIHandler.RemoteForwardPort(ctx, out)
}

func firstLaunch(t *testing.T, s *supervisor) launch {
	t.Helper()
	select {
	case result := <-s.launches:
		if result.ordinal != 1 {
			t.Fatal("first worker failed before readiness")
		}
		return result
	default:
		t.Fatal("missing launch evidence")
		return launch{}
	}
}

func rotation(t *testing.T, ctx context.Context, s *supervisor, oldID string, first launch, oldPID int, oldSocket string, streams ...*streamCounts) {
	t.Helper()
	wait(t, ctx, "new credential instance ready", func() bool { id := s.ReadyInstanceID(s.id); return id != "" && id != oldID })
	if time.Now().Before(first.started.Add(firstLifetime)) {
		t.Fatal("worker replaced before configured credential expiry")
	}
	resources.Reaped(t, oldPID)
	if _, err := os.Lstat(oldSocket); !os.IsNotExist(err) {
		t.Fatalf("old CoreAPI endpoint remains: %v", err)
	}
	for _, stream := range streams {
		wait(t, ctx, "original callbacks returned and contexts canceled", func() bool {
			return stream.started.Load() > 0 && stream.active.Load() == 0 && stream.canceled.Load() == stream.started.Load()
		})
		t.Logf("callback terminal states: %+v", stream.terminalStates())
		if err := stream.expiryEvidence(first.started.Add(firstLifetime)); err != nil {
			t.Fatalf("old callback expiry evidence: %v; started=%d active=%d canceled=%d", err, stream.started.Load(), stream.active.Load(), stream.canceled.Load())
		}
	}
	select {
	case next := <-s.launches:
		if next.ordinal != 2 {
			t.Fatalf("unexpected restart count: %d", next.ordinal)
		}
	default:
		t.Fatal("replacement config not observed")
	}
	t.Logf("%s expiry=%s old_instance=%s new_instance=%s old_pid=%d reaped=true callback_canceled=true", s.id, first.started.Add(firstLifetime).Format(time.RFC3339Nano), oldID, s.ReadyInstanceID(s.id), oldPID)
}

func workerPID(t *testing.T, artifact binary, serverPID int) int {
	t.Helper()
	for _, pid := range resources.Children(t, artifact.path, 2) {
		if pid != serverPID {
			return pid
		}
	}
	t.Fatal("client worker PID missing")
	return 0
}
func coreSocket(t *testing.T, s *supervisor) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(s.runDir, "p-*", "core.sock"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("CoreAPI sockets: %v %v", paths, err)
	}
	return paths[0]
}
func config(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	result, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func resolve(remote rpc.ShikiDaemonClient) func(context.Context, *ppb.Scope) (rpc.ShikiDaemonClient, error) {
	return func(_ context.Context, scope *ppb.Scope) (rpc.ShikiDaemonClient, error) {
		if scope.GetServerId() != "srv_test" {
			return nil, fmt.Errorf("unexpected server scope")
		}
		return remote, nil
	}
}
