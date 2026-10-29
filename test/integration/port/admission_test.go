package portplugin_test

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	cfg "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	coreRPC "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/types/known/structpb"
)

// Supply an empty session inventory, not a replacement scanner or worker.
// The real subprocess observes this test's TCP listener through the OS scanner.
type admissionCore struct {
	coreRPC.UnimplementedCoreAPIHandler
}

func (*admissionCore) ListSessions(context.Context, *ppb.Scope) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{}, nil
}

func TestProductionPortWatchBudgetKeepsMetadataAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-port-admit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	binary := filepath.Join(dir, "port.race")
	command := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "../../../plugins/port")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := []byte("#!/bin/sh\nGORACE=" + quote("halt_on_error=1 log_path="+filepath.Join(dir, "race")) + " exec " + quote(binary) + " \"$@\"\n")
	executable := filepath.Join(dir, "shiki-plugin-port")
	if err := os.WriteFile(executable, script, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		paths, err := filepath.Glob(filepath.Join(dir, "race.*"))
		if err != nil {
			t.Error(err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			t.Errorf("worker race report: %v\n%s", err, data[:min(len(data), 64<<10)])
		}
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	target := int32(listener.Addr().(*net.TCPAddr).Port)
	sum := sha256.Sum256(script)
	values, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(target)}, "scan_interval_seconds": 60.0})
	if err != nil {
		t.Fatal(err)
	}
	offer, err := sdk.BuiltinProtocol("port", "server")
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := host.NewSupervisor(ctx, host.SupervisorOptions{StateDir: filepath.Join(dir, "supervisor"), Validate: func(_ string, v *structpb.Struct) error { _, err := cfg.Parse(v, "server"); return err }, Config: func(string, *structpb.Struct) (host.Config, error) {
		return host.Config{Manifest: host.Manifest{Name: "port", Version: buildinfo.Version, Executable: executable, SHA256: hex.EncodeToString(sum[:]), Side: "server", Protocol: offer}, RunDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "state"), HostInstanceID: "host_admission", Values: values, Core: &admissionCore{}, Policy: host.Policy{Side: "server", Capabilities: []string{"core.sessions.read"}, Scopes: []*ppb.Scope{{}}}, StartupTimeout: 10 * time.Second, CredentialLifetime: time.Hour}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := supervisor.Close(); err != nil {
			t.Error(err)
		}
	})
	supervisor.Start()
	initial, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, plugin := range initial.Plugins {
		if plugin.PluginId == "port" {
			revision = plugin.Revision
		}
	}
	if _, err := supervisor.Manage(ctx, &pb.ManagePluginRequest{PluginId: "port", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		list, err := supervisor.List()
		if err != nil {
			return false
		}
		for _, p := range list.Plugins {
			if p.PluginId == "port" && p.State == "ready" {
				return true
			}
		}
		return false
	})
	pids := resources.Children(t, binary, 1)
	manager, err := auth.Open(filepath.Join(dir, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	service := &server.Service{Auth: manager, DaemonDataPlane: host.DaemonDataPlane{Supervisor: supervisor}}
	httpServer := httptest.NewUnstartedServer(service.Handler())
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	defer httpServer.Close()
	client := func(permissions ...string) rpc.ShikiDaemonClient {
		token, _, err := manager.Create("tester", permissions, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(httpServer.Certificate())
		endpoint, err := connection.NewTLSClient(httpServer.URL, connection.TLSOptions{RootCAs: roots})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(endpoint.Close)
		bearer, err := connection.BearerToken(token)
		if err != nil {
			t.Fatal(err)
		}
		return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, bearer))
	}
	clients := []rpc.ShikiDaemonClient{client("port.list"), client("port.list")}
	var streams []rpc.ShikiDaemonWatchPortsClientStream
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
		for _, s := range streams {
			_ = s.Close()
		}
	}()
	open := func(api rpc.ShikiDaemonClient) (rpc.ShikiDaemonWatchPortsClientStream, context.CancelFunc, error) {
		call, stop := context.WithCancel(ctx)
		stream, err := api.WatchPorts(call, &pb.WatchPortsRequest{})
		if err != nil {
			stop()
			return rpc.ShikiDaemonWatchPortsClientStream{}, nil, err
		}
		for {
			event, err := stream.Receive()
			if err != nil {
				stop()
				_ = stream.Close()
				return rpc.ShikiDaemonWatchPortsClientStream{}, nil, err
			}
			if event.GetType() == pb.PortEvent_OPENED && event.GetPort().GetPort() == target {
				return stream, stop, nil
			}
		}
	}
	for i := 0; i < 32; i++ {
		stream, stop, err := open(clients[i%2])
		if err != nil {
			t.Fatalf("watch %d initial real scan: %v", i, err)
		}
		streams = append(streams, stream)
		cancels = append(cancels, stop)
	}
	third := client("port.list", "port.forward")
	overflow, stop, err := open(third)
	if stop != nil {
		stop()
		_ = overflow.Close()
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("33rd watch: %v", err)
	}
	list, err := third.ListPorts(ctx, &pb.ListPortsRequest{})
	if err != nil {
		t.Fatalf("full watch budget blocked metadata: %v", err)
	}
	found := false
	for _, p := range list.Ports {
		found = found || p.Port == target
	}
	if !found {
		t.Fatal("metadata missed actual TCP listener")
	}
	if _, err := third.CheckPort(ctx, &pb.CheckPortRequest{RemoteAddress: "127.0.0.1", Port: target}); err != nil {
		t.Fatalf("full watch budget blocked authorization check: %v", err)
	}
	denied := client("system.info")
	overflow, stop, err = open(denied)
	if stop != nil {
		stop()
		_ = overflow.Close()
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("capacity checked before permission: %v", err)
	}
	cancels[0]()
	_ = streams[0].Close()
	wait(t, func() bool {
		replacement, stop, err := open(third)
		if err != nil {
			return false
		}
		stop()
		_ = replacement.Close()
		return true
	})
	for _, cancel := range cancels {
		cancel()
	}
	for _, stream := range streams {
		_ = stream.Close()
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	resources.Reaped(t, pids...)
	t.Log(fmt.Sprintf("32 real admitted watches across two credentials; overflow rejected; ListPorts/CheckPort available; PID %v reaped", pids))
}
