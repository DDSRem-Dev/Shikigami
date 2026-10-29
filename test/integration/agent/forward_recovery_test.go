package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

// A daemon may cancel its active streams during a graceful upgrade while its
// authenticated HTTP/2 route remains healthy (or reconnects internally).
type canceledForwardDaemon struct {
	rpc.UnimplementedShikiDaemonHandler
	streams atomic.Int32
}

func (*canceledForwardDaemon) NegotiateProtocol(context.Context, *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_forward_recovery", Capabilities: []string{sdk.PortCapability, sdk.PortCheckCapability}}, nil
}
func (*canceledForwardDaemon) CheckPort(_ context.Context, in *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	return &pb.CheckPortResponse{Port: &pb.OpenPort{Port: in.Port}}, nil
}
func (*canceledForwardDaemon) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	return &pb.SystemInfo{DaemonId: "daemon_forward_recovery"}, nil
}
func (d *canceledForwardDaemon) ForwardPort(_ context.Context, stream rpc.ShikiDaemonForwardPortServerStream) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	count := d.streams.Add(1)
	if err = stream.Send(&pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: first.ConnectionId}); err != nil {
		return err
	}
	for {
		frame, err := stream.Receive()
		if err != nil {
			return err
		}
		if frame.Type == pb.PortData_DATA {
			if count == 1 {
				return connect.NewError(connect.CodeCanceled, "DAEMON_STOPPING")
			}
			if err = stream.Send(&pb.PortData{Type: pb.PortData_DATA, ConnectionId: first.ConnectionId, Data: append([]byte("echo:"), frame.Data...)}); err != nil {
				return err
			}
		}
		if frame.Type == pb.PortData_HALF_CLOSE {
			return stream.Send(&pb.PortData{Type: pb.PortData_HALF_CLOSE, ConnectionId: first.ConnectionId})
		}
	}
}

func waitForward(t *testing.T, ctx context.Context, check func(context.Context) error) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var last error
	for deadline.Err() == nil {
		call, stop := context.WithTimeout(deadline, time.Second)
		last = check(call)
		stop()
		if last == nil {
			return
		}
		select {
		case <-deadline.Done():
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("forward recovery did not settle:", last)
}

func TestCanceledForwardRecoversOnHealthyAuthenticatedRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-forward-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "shiki-plugin-port")
	if output, err := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "../../../plugins/port").CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	offer, err := sdk.BuiltinProtocol("port", "both")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(host.Manifest{Name: "port", Version: "dev", Side: "both", Executable: "shiki-plugin-port", SHA256: hex.EncodeToString(sum[:]), Protocol: offer})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "port.manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	daemon := new(canceledForwardDaemon)
	server := connect.NewServer(func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, _ := connect.CallInfoForServerContext(ctx)
			if info.RequestHeader().Get("Authorization") != "Bearer shiki_forward_recovery" {
				return connect.NewError(connect.CodeUnauthenticated, "TOKEN_REQUIRED")
			}
			return next(ctx, spec, stream)
		}
	})
	rpc.RegisterShikiDaemonHandler(server, daemon)
	tls := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
	tls.EnableHTTP2 = true
	tls.StartTLS()
	defer tls.Close()
	ca := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tls.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(dir, "node")
	cfg.Agent.StateDir = filepath.Join(dir, "state")
	cfg.Agent.Socket = filepath.Join(dir, "agent.sock")
	cfg.PluginsDir = dir
	configPath := filepath.Join(dir, "client.yaml")
	if err = config.SaveClient(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- agent.RunConfig(agentCtx, cfg, "dev") }()
	defer func() {
		stopAgent()
		if err := <-done; err != nil {
			t.Error("agent shutdown:", err)
		}
	}()
	var client *frontend.Client
	waitForward(t, ctx, func(call context.Context) error {
		client, err = frontend.Open(call, configPath, frontend.Options{Timeout: time.Second})
		return err
	})
	defer client.Close()
	local := client.Agent()
	record, err := local.UpsertServer(ctx, &apb.UpsertServerRequest{RequestId: "server", Config: &apb.ServerConfig{Name: "recovery", DirectTlsAddress: strings.TrimPrefix(tls.URL, "https://"), TlsServerName: "example.com", TlsCaFile: ca}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = local.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte("shiki_forward_recovery"), RequestId: "credential"}); err != nil {
		t.Fatal(err)
	}
	target := &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
	plugins, err := local.ListPlugins(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range plugins.Plugins {
		if plugin.PluginId == "port" {
			_, err = local.ManagePlugin(ctx, &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: "port", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: plugin.Revision, RequestId: "enable"}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	waitForward(t, ctx, func(call context.Context) error {
		plugins, err := local.ListPlugins(call, target)
		if err != nil {
			return err
		}
		for _, plugin := range plugins.Plugins {
			if plugin.PluginId == "port" && plugin.State == "ready" {
				return nil
			}
		}
		return errors.New("port worker not ready")
	})
	initial, err := local.CreateForward(ctx, &apb.CreateForwardRequest{RequestId: "forward", Spec: &ppb.ForwardSpec{ServerId: record.Config.Id, RemotePort: 3000, Enabled: true}})
	if err != nil || initial.GetBoundPort() == 0 {
		t.Fatal("listener not established", initial, err)
	}
	dial := func(port uint32) net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		return conn
	}
	old := dial(initial.BoundPort)
	if _, err = io.WriteString(old, "old-input"); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Read(make([]byte, 1)); err == nil {
		t.Fatal("canceled stream remained open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("canceled stream did not close")
	}
	old.Close()
	var restored *ppb.ForwardState
	waitForward(t, ctx, func(call context.Context) error {
		list, err := local.ListForwards(call, &pb.Empty{})
		if err != nil {
			return err
		}
		if len(list.Forwards) != 1 {
			return errors.New("desired forward disappeared")
		}
		state := list.Forwards[0]
		if state.State != "listening" || state.BoundPort == 0 || state.Connections != 0 {
			return fmt.Errorf("listener not restored: state=%s reason=%s", state.State, state.ReasonCode)
		}
		restored = state
		return nil
	})
	if !proto.Equal(initial.Spec, restored.Spec) || daemon.streams.Load() != 1 {
		t.Fatal("recovery changed desired intent or replayed an old connection")
	}
	fresh := dial(restored.BoundPort)
	if _, err = io.WriteString(fresh, "fresh-input"); err != nil {
		t.Fatal(err)
	}
	if err = fresh.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	want := "echo:fresh-input"
	response, err := io.ReadAll(fresh)
	if err != nil || string(response) != want {
		t.Fatal("fresh application connection failed", string(response), err)
	}
	fresh.Close()
	if _, err = local.StopForward(ctx, &apb.StopForwardRequest{ForwardId: initial.Spec.Id, ExpectedRevision: initial.Spec.Revision, RequestId: "stop"}); err != nil {
		t.Fatal(err)
	}
	// A later authenticated probe must never resurrect explicitly stopped intent.
	if _, err = client.Remote(record.Config.Id).GetSystemInfo(ctx, &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	list, err := local.ListForwards(ctx, &pb.Empty{})
	if err != nil || len(list.GetForwards()) != 0 {
		t.Fatal("explicit stop was undone", list, err)
	}
}
