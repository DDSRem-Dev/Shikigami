package plugin_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/structpb"
)

var helperSide = flag.String("side", "client", "plugin side")
var grantedScope = &ppb.Scope{ServerId: "srv-test", ProjectId: "proj-test", MappingId: "mapping-test"}

type helperPlugin struct {
	core                 sdk.CoreAPI
	mu                   sync.Mutex
	lifetime             context.Context
	done                 chan struct{}
	mode, instance, host string
}

func (p *helperPlugin) Info() *ppb.PluginInfo {
	capabilities := []string{"core.scope.v1"}
	if *helperSide == "server" && os.Getenv("SHIKI_WORKER_DATA") == "1" {
		capabilities = append(capabilities, sdk.FileCapability, sdk.PortCapability)
	}
	return &ppb.PluginInfo{Name: "sync", Version: "0.0.1", Side: "both", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: capabilities}}
}
func (p *helperPlugin) Init(ctx context.Context, in sdk.InitContext) error {
	p.core = in.Core
	p.mode = in.Config.GetFields()["mode"].GetStringValue()
	p.instance = in.InstanceID
	p.host = in.HostInstanceID
	// This must succeed during Init, proving the host listener is already ready.
	_, err := p.core.GetConfig(ctx, &ppb.ConfigRequest{Scope: grantedScope})
	if err != nil {
		return err
	}
	if p.mode == "fail-init" {
		return connect.NewError(connect.CodeAborted, "secret initializer failure")
	}
	if p.mode == "hang-init" {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (p *helperPlugin) Start(ctx context.Context) error {
	p.mu.Lock()
	p.lifetime = ctx
	p.done = make(chan struct{})
	p.mu.Unlock()
	stream, err := p.core.SubscribeEvents(ctx, &ppb.SubscribeRequest{Scope: grantedScope})
	if err != nil {
		return err
	}
	if _, err = stream.Receive(); err != nil {
		return err
	}
	go func() {
		defer close(p.done)
		defer stream.Close()
		for {
			_, err := stream.Receive()
			if err != nil {
				return
			}
		}
	}()
	return nil
}
func (p *helperPlugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if p.mode == "hang-stop" {
		<-ctx.Done()
		return ctx.Err()
	}
	if marker := os.Getenv("SHIKI_STOP_MARKER"); marker != "" {
		return os.WriteFile(marker, []byte("stopped"), 0600)
	}
	return nil
}
func (p *helperPlugin) Health(ctx context.Context) (*pb.HealthStatus, error) {
	p.mu.Lock()
	lifetime := p.lifetime
	p.mu.Unlock()
	if lifetime == nil || lifetime.Err() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "Start context ended")
	}
	if strings.HasPrefix(p.mode, "chain") {
		return p.chainHealth(ctx)
	}
	switch p.mode {
	case "deny-scope":
		_, err := p.core.GetConfig(ctx, &ppb.ConfigRequest{Scope: &ppb.Scope{ProjectId: "other"}})
		return nil, err
	case "deny-capability":
		_, err := p.core.ListProjects(ctx, grantedScope)
		return nil, err
	case "remote-on-server":
		_, err := p.core.RemoteListFiles(ctx, &ppb.RemoteListFilesRequest{Scope: grantedScope, Request: &pb.ListFilesRequest{ProjectId: "proj-test"}})
		return nil, err
	case "error":
		_, err := p.core.GetProject(ctx, grantedScope)
		return nil, err
	case "publish":
		_, err := p.core.PublishEvent(ctx, &ppb.PublishEventRequest{ExpectedHostInstanceId: p.host, Event: &ppb.Event{Type: "probe", Scope: grantedScope, SourceInstanceId: "forged"}})
		if err != nil {
			return nil, err
		}
	}
	return &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY, Details: map[string]string{"pid": strconv.Itoa(os.Getpid()), "instance": p.instance}}, nil
}
func TestPluginProcess(t *testing.T) {
	if os.Getenv("SHIKI_TEST_PLUGIN") != "1" {
		return
	}
	if marker := os.Getenv("SHIKI_PID_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
	}
	helper := &helperPlugin{}
	var services sdk.Services
	if os.Getenv("SHIKI_WORKER_DATA") == "1" {
		worker := &memoryWorker{plugin: helper}
		services = sdk.Services{Files: worker, Ports: worker}
	}
	if err := sdk.Serve(*helperSide, helper, services); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

type coreProbe struct {
	prpc.UnimplementedCoreAPIHandler
	mu                      sync.Mutex
	initialized, subscribed bool
	closed                  chan struct{}
	source                  string
	latestEvent             string
	errorCode               connect.Code
}

func (p *coreProbe) GetConfig(ctx context.Context, in *ppb.ConfigRequest) (*ppb.PluginConfig, error) {
	p.mu.Lock()
	p.initialized = true
	p.mu.Unlock()
	return &ppb.PluginConfig{Revision: "1"}, nil
}
func (p *coreProbe) GetProject(context.Context, *ppb.Scope) (*pb.Project, error) {
	return nil, connect.NewError(p.errorCode, "secret downstream payload")
}
func (p *coreProbe) PublishEvent(ctx context.Context, in *ppb.PublishEventRequest) (*pb.Empty, error) {
	p.mu.Lock()
	p.source = in.Event.SourceInstanceId
	p.latestEvent = in.Event.Type
	p.mu.Unlock()
	return &pb.Empty{}, nil
}
func (p *coreProbe) SubscribeEvents(ctx context.Context, in *ppb.SubscribeRequest, s prpc.CoreAPISubscribeEventsServerStream) error {
	p.mu.Lock()
	p.subscribed = true
	p.mu.Unlock()
	if err := s.Send(&ppb.Event{Type: "ready"}); err != nil {
		return err
	}
	<-ctx.Done()
	close(p.closed)
	return ctx.Err()
}
func pluginConfig(t *testing.T, mode string) (host.Config, *coreProbe) {
	t.Helper()
	root := os.Getenv("SHIKI_HOST_ROOT")
	var err error
	if root == "" {
		root, err = os.MkdirTemp("/tmp", "shiki-plugin-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	values, _ := structpb.NewStruct(map[string]any{"mode": mode})
	probe := &coreProbe{closed: make(chan struct{}), errorCode: connect.CodeAborted}
	return host.Config{Manifest: host.Manifest{Name: "sync", Version: "0.0.1", Executable: path, SHA256: hex.EncodeToString(sum[:]), Side: "client", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.FileCapability, sdk.PortCapability}}},
		Args: []string{"-test.run=^TestPluginProcess$"}, Env: []string{"SHIKI_TEST_PLUGIN=1", "SHIKI_STOP_MARKER=" + os.Getenv("SHIKI_STOP_MARKER")}, RunDir: filepath.Join(root, "run"), StateDir: filepath.Join(root, "state"), HostInstanceID: "host-test", Core: probe, Values: values,
		Policy: host.Policy{Side: "client", Capabilities: []string{"core.config.read", "core.events.read", "core.events.write", "core.projects.read", "file.read"}, Scopes: []*ppb.Scope{grantedScope}}, StartupTimeout: 5 * time.Second, CredentialLifetime: time.Hour}, probe
}
func waitStopped(t *testing.T, instance *host.Instance) {
	t.Helper()
	select {
	case <-instance.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("plugin did not exit")
	}
	if err := syscall.Kill(instance.Snapshot().PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("plugin remains alive: %v", err)
	}
}
func TestAutoMTLSLifecycleAndConnectCallback(t *testing.T) {
	cfg, probe := pluginConfig(t, "publish")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instance, err := host.Launch(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Stop(context.Background())
	probe.mu.Lock()
	initialized, subscribed := probe.initialized, probe.subscribed
	probe.mu.Unlock()
	if !initialized || !subscribed {
		t.Fatal("Init/Start callback did not complete")
	}
	// Calls after Launch returns verify that Start's short RPC context did not
	// become the worker lifetime. Multiple callbacks share the scoped CoreAPI.
	for range 3 {
		health, err := instance.Health(context.Background())
		if err != nil || health.Status != pb.HealthStatus_HEALTHY {
			t.Fatalf("health: %v %v", health, err)
		}
	}
	probe.mu.Lock()
	source := probe.source
	probe.mu.Unlock()
	if source != instance.Snapshot().InstanceID {
		t.Fatalf("event source was not host-bound: %s", source)
	}
	if err = instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStopped(t, instance)
	select {
	case <-probe.closed:
	default:
		t.Fatal("callback stream was not canceled")
	}
	entries, err := os.ReadDir(cfg.RunDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("plugin runtime files leaked: %v %v", entries, err)
	}
	if err = instance.Stop(context.Background()); err != nil {
		t.Fatalf("idempotent stop: %v", err)
	}
}
func TestCallbackAuthorizationAndErrorMapping(t *testing.T) {
	for _, mode := range []string{"deny-scope", "deny-capability", "remote-on-server", "error"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _ := pluginConfig(t, mode)
			if mode == "deny-capability" {
				cfg.Policy.Capabilities = []string{"core.config.read", "core.events.read"}
			}
			if mode == "remote-on-server" {
				cfg.Policy.Side = "server"
				cfg.Manifest.Side = "server"
			}
			instance, err := host.Launch(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Stop(context.Background())
			_, err = instance.Health(context.Background())
			want := connect.CodePermissionDenied
			if mode == "error" {
				want = connect.CodeAborted
			}
			if connect.CodeOf(err) != want || strings.Contains(err.Error(), "secret") {
				t.Fatalf("callback code propagation: want %v, got %v", want, err)
			}
		})
	}
}
func TestExpiryAndCanceledHostReapProcess(t *testing.T) {
	for _, mode := range []string{"expire", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cfg, probe := pluginConfig(t, "")
			if mode == "expire" {
				cfg.CredentialLifetime = 2 * time.Second
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			instance, err := host.Launch(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Stop(context.Background())
			if mode == "cancel" {
				cancel()
			}
			waitStopped(t, instance)
			select {
			case <-probe.closed:
			default:
				t.Fatal("expired/revoked callback stream remains")
			}
		})
	}
}
func TestInitializationFailureCleansEveryProcess(t *testing.T) {
	for _, mode := range []string{"fail-init", "hang-init", "bad-hash", "wrong-version"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _ := pluginConfig(t, mode)
			if mode == "hang-init" {
				cfg.StartupTimeout = 500 * time.Millisecond
			}
			if mode == "bad-hash" {
				cfg.Manifest.SHA256 = strings.Repeat("0", 64)
			}
			if mode == "wrong-version" {
				cfg.Manifest.Version = "0.0.2"
			}
			if instance, err := host.Launch(context.Background(), cfg); err == nil {
				instance.Stop(context.Background())
				t.Fatal("failure unexpectedly succeeded")
			}
			if mode != "hang-init" {
				entries, err := os.ReadDir(cfg.RunDir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("ordinary failed initialization returned before cleanup: %v %v", entries, err)
				}
				return
			}
			// Only an expired startup deadline bounds Launch before cleanup;
			// that instance keeps its writer lease until cleanup actually ends.
			deadline := time.Now().Add(5 * time.Second)
			for {
				entries, err := os.ReadDir(cfg.RunDir)
				if err == nil && len(entries) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("failed instance files leaked: %v %v", entries, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
func TestPluginSocketRejectsUnauthenticatedGRPC(t *testing.T) {
	cfg, _ := pluginConfig(t, "")
	instance, err := host.Launch(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Stop(context.Background())
	var socket string
	filepath.WalkDir(cfg.RunDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSocket != 0 && filepath.Base(path) != "core.sock" {
			socket = path
		}
		return nil
	})
	if socket == "" {
		t.Fatal("private gRPC socket missing")
	}
	client, err := grpc.NewClient("passthrough:///plugin-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err = grpc_health_v1.NewHealthClient(client).Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err == nil {
		t.Fatal("plugin accepted a client without mTLS")
	}
}
func TestCoreSocketRejectsWrongInstanceAndCredential(t *testing.T) {
	cfg, _ := pluginConfig(t, "")
	instance, err := host.Launch(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Stop(context.Background())
	matches, err := filepath.Glob(filepath.Join(cfg.RunDir, "p-*", "core.sock"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("core socket: %v %v", matches, err)
	}
	ep, err := connection.NewUnixClient(matches[0], uint32(os.Getuid()), "shiki-core")
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()
	client := prpc.NewCoreAPIClient(connection.NewRPCClient(ep))
	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Authorization", "Bearer guessed")
	info.RequestHeader().Set("shiki-plugin-instance-id", instance.Snapshot().InstanceID)
	info.RequestHeader().Set("shiki-host-instance-id", "old-host")
	if _, err = client.GetConfig(ctx, &ppb.ConfigRequest{Scope: grantedScope}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("wrong credential accepted: %v", err)
	}
}
func TestCrossProtocolErrorCodes(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeAborted, connect.CodeResourceExhausted} {
		t.Run(code.String(), func(t *testing.T) {
			cfg, probe := pluginConfig(t, "error")
			probe.errorCode = code
			instance, err := host.Launch(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Stop(context.Background())
			_, err = instance.Health(context.Background())
			if connect.CodeOf(err) != code || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error mapping: %v", err)
			}
		})
	}
	if sdk.FromGRPC(io.EOF) != io.EOF || sdk.ToGRPC(io.EOF) != io.EOF {
		t.Fatal("EOF changed into an error")
	}
}

func TestConcurrentInstancesCannotShareWriterState(t *testing.T) {
	cfg, _ := pluginConfig(t, "")
	first, err := host.Launch(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Stop(context.Background())
	if second, err := host.Launch(context.Background(), cfg); err == nil {
		second.Stop(context.Background())
		t.Fatal("two workers acquired the same state directory")
	}
	if err = first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg.Core = &coreProbe{closed: make(chan struct{})}
	next, err := host.Launch(context.Background(), cfg)
	if err != nil {
		t.Fatalf("state lease was not released after Wait: %v", err)
	}
	defer next.Stop(context.Background())
}

func TestHostProcess(t *testing.T) {
	if os.Getenv("SHIKI_TEST_HOST") != "1" {
		return
	}
	cfg, _ := pluginConfig(t, "")
	instance, err := host.Launch(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(os.Getenv("SHIKI_HOST_ROOT"), "worker.pid")
	if err = os.WriteFile(report, []byte(strconv.Itoa(instance.Snapshot().PID)), 0600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestHostSIGKILLDoesNotLeaveWorker(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shiki-parent-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "worker-stopped")
	command := exec.Command(executable, "-test.run=^TestHostProcess$")
	command.Env = append(os.Environ(), "SHIKI_TEST_HOST=1", "SHIKI_HOST_ROOT="+root, "SHIKI_STOP_MARKER="+marker)
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	var pid int
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		data, e := os.ReadFile(filepath.Join(root, "worker.pid"))
		if e == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("host helper did not publish worker PID")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatalf("orphan worker %d survived host SIGKILL", pid)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "stopped" {
		t.Fatalf("orphan watchdog did not run Stop: %q %v", data, err)
	}
}
