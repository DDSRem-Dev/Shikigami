package portplugin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	cfg "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	coreRPC "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	daemonRPC "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func wait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("port process condition timed out")
}
func supervisor(t *testing.T, ctx context.Context, dir, executable, sum, side string, values map[string]any, core coreRPC.CoreAPIHandler) *host.Supervisor {
	t.Helper()
	capability := sdk.PortCapability
	if side == "client" {
		capability = sdk.PortWorkerCapability
	}
	config, err := structValue(values)
	if err != nil {
		t.Fatal(err)
	}
	s, err := host.NewSupervisor(ctx, host.SupervisorOptions{StateDir: filepath.Join(dir, side+"-supervisor"), Validate: func(_ string, v *structpb.Struct) error { _, e := cfg.Parse(v, side); return e }, Config: func(_ string, _ *structpb.Struct) (host.Config, error) {
		policy := host.Policy{Side: side}
		if side == "client" {
			policy.Capabilities = []string{"port.forward"}
			policy.Scopes = []*ppb.Scope{{ServerId: "srv_test"}}
		}
		return host.Config{Manifest: host.Manifest{Name: "port", Version: buildinfo.Version, Executable: executable, SHA256: sum, Side: side, Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", capability}}}, RunDir: filepath.Join(dir, side+"-run"), StateDir: filepath.Join(dir, side+"-state"), HostInstanceID: "host_" + side, Values: config, Core: core, Policy: policy, StartupTimeout: 10 * time.Second, CredentialLifetime: time.Hour}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	s.Start()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, p := range list.Plugins {
		if p.PluginId == "port" {
			revision = p.Revision
		}
	}
	if _, err = s.Manage(ctx, &pb.ManagePluginRequest{PluginId: "port", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		list, err := s.List()
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
	return s
}
func structValue(v map[string]any) (*structpb.Struct, error) { return structpb.NewStruct(v) }
func TestTwoProductionProcessesHalfClosePolicyAndStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-port-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	executable := filepath.Join(dir, "shiki-plugin-port")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", executable, "../../../plugins/port")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(binary)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var targetWG sync.WaitGroup
	var targetActive atomic.Int64
	targetDone := make(chan struct{})
	go func() {
		defer close(targetDone)
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			targetWG.Add(1)
			targetActive.Add(1)
			go func() {
				defer targetWG.Done()
				defer targetActive.Add(-1)
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				body, err := io.ReadAll(conn)
				if err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
				_, _ = conn.Write(append([]byte("response:"), body...))
			}()
		}
	}()
	defer func() { target.Close(); <-targetDone; targetWG.Wait() }()
	remotePort := uint32(target.Addr().(*net.TCPAddr).Port)
	server := supervisor(t, ctx, dir, executable, hex.EncodeToString(checksum[:]), "server", map[string]any{"allow_ports": []any{float64(remotePort)}, "idle_timeout_seconds": 3.0, "max_connections": 1.0}, &coreRPC.UnimplementedCoreAPIHandler{})
	socket := filepath.Join(dir, "daemon.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	rpcServer := connect.NewServer()
	daemonRPC.RegisterShikiDaemonHandler(rpcServer, &host.DaemonDataPlane{Supervisor: server})
	httpServer := connection.NewHTTPServer(ctx, host.WithStreamCancellation(connection.NewRPCHandler(rpcServer)))
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	defer func() { httpServer.Close(); <-served }()
	endpoint, err := connection.NewUnixClient(socket, uint32(os.Getuid()), "shiki-daemon")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	remote := daemonRPC.NewShikiDaemonClient(connection.NewRPCClient(endpoint))
	core := &host.RemoteCore{Resolve: func(_ context.Context, scope *ppb.Scope) (daemonRPC.ShikiDaemonClient, error) {
		if scope.ServerId != "srv_test" {
			return nil, connect.NewError(connect.CodePermissionDenied, "bad scope")
		}
		return remote, nil
	}}
	client := supervisor(t, ctx, dir, executable, hex.EncodeToString(checksum[:]), "client", map[string]any{"idle_timeout_seconds": 3.0}, core)
	spec := &ppb.ForwardSpec{Id: "forward_test", ServerId: "srv_test", RemoteAddress: "127.0.0.1", RemotePort: remotePort, LocalAddress: "127.0.0.1", Enabled: true, Revision: "rev_test"}
	var state *ppb.ForwardState
	short, shortCancel := context.WithCancel(ctx)
	err = client.Call(short, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
		var e error
		state, e = l.Port().StartForward(ctx, spec)
		return e
	})
	shortCancel()
	if err != nil || state.BoundPort == 0 {
		t.Fatal(state, err)
	}
	bound := state.BoundPort
	exchange := func(body []byte) {
		t.Helper()
		conn, err := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(bound)})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err = conn.Write(body); err != nil {
			t.Fatal(err)
		}
		if err = conn.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(conn)
		want := append([]byte("response:"), body...)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("response bytes %d/%d err=%v", len(got), len(want), err)
		}
		// TCP HALF_CLOSE reaches the application before the downstream RPC's
		// final status. With a one-slot server, wait for the actual client worker
		// to finish that RPC before opening another application connection.
		wait(t, func() bool {
			finished := false
			err := client.Call(ctx, "port", sdk.PortWorkerCapability, func(call context.Context, l sdk.Lifecycle) error {
				list, err := l.Port().ListForwards(call)
				if err == nil {
					for _, current := range list.Forwards {
						if current.Spec.Id == spec.Id {
							finished = current.Connections == 0 && current.State == "listening"
						}
					}
				}
				return err
			})
			return err == nil && finished
		})
	}
	exchange(bytes.Repeat([]byte("large-payload"), 50000))
	exchange([]byte("listener survives caller context"))
	wait(t, func() bool {
		err := client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			list, e := l.Port().ListForwards(ctx)
			if e == nil && len(list.Forwards) == 1 {
				state = list.Forwards[0]
			}
			return e
		})
		return err == nil && state.Connections == 0 && state.BytesTransferred > 1<<20
	})
	if state.BoundPort != bound {
		t.Fatal("listener changed port")
	}
	// An unlisted loopback endpoint and an arbitrary LAN endpoint are both denied.
	for _, address := range []string{"127.0.0.1", "192.168.0.1"} {
		stream, err := remote.ForwardPort(ctx)
		if err != nil {
			t.Fatal(err)
		}
		port := int32(22)
		if address != "127.0.0.1" {
			port = int32(remotePort)
		}
		if err = stream.Send(&pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "denied", RemoteAddress: address, Port: port}); err != nil {
			t.Fatal(err)
		}
		_, err = stream.Receive()
		stream.Close()
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatal("target policy bypassed", address, err)
		}
	}
	// Stop must close an accepted idle socket and release the listening port.
	idle, err := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound))))
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	wait(t, func() bool {
		var count uint64
		_ = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			list, e := l.Port().ListForwards(ctx)
			if e == nil {
				count = list.Forwards[0].Connections
			}
			return e
		})
		return count == 1
	})
	wait(t, func() bool { return targetActive.Load() == 1 })
	// The existing idle stream holds the server's only connection slot.
	limited, e := remote.ForwardPort(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = limited.Send(&pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "limit", RemoteAddress: "127.0.0.1", Port: int32(remotePort)}); e != nil && !errors.Is(e, io.EOF) {
		t.Fatal(e)
	}
	// Admission can reject before the request's first write completes. Its
	// response-side status, not the sender's EOF, is the rejection evidence.
	_, e = limited.Receive()
	limited.Close()
	if connect.CodeOf(e) != connect.CodeResourceExhausted {
		t.Fatal("server connection limit not enforced", e)
	}
	err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
		return l.Port().StopForward(ctx, &ppb.ForwardID{Id: spec.Id})
	})
	if err != nil {
		t.Fatal(err)
	}
	idle.SetReadDeadline(time.Now().Add(time.Second))
	_, err = idle.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("stop left idle connection open")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("stop did not cancel idle stream")
	}
	rebound, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound))))
	if err != nil {
		t.Fatal("listener not released", err)
	}
	rebound.Close()
	idle.Close()
	wait(t, func() bool { return targetActive.Load() == 0 })
	cycles := 8
	stress := resources.Cycles(t)
	var baseline resources.Sample
	var workerPIDs []int
	if stress != 0 {
		cycles = stress
		workerPIDs = resources.Children(t, executable, 2)
		baseline = resources.Capture(t, append(append([]int{}, workerPIDs...), os.Getpid())...)
	}
	// Repeated listeners/streams exercise cleanup through both RPC adapters.
	for i := 0; i < cycles; i++ {
		err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			var e error
			state, e = l.Port().StartForward(ctx, spec)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		bound = state.BoundPort
		exchange([]byte("cycle"))
		var pending net.Conn
		if stress != 0 {
			wait(t, func() bool { return targetActive.Load() == 0 })
			pending, err = net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound))))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pending.Close() })
			wait(t, func() bool { return targetActive.Load() == 1 })
		}
		err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			return l.Port().StopForward(ctx, &ppb.ForwardID{Id: spec.Id})
		})
		if err != nil {
			t.Fatal(err)
		}
		if stress != 0 {
			pending.SetReadDeadline(time.Now().Add(time.Second))
			_, err = pending.Read(make([]byte, 1))
			pending.Close()
			if err == nil {
				t.Fatal("Stop left an idle forwarded connection open")
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("Stop failed to cancel an idle forwarded read")
			}
			wait(t, func() bool { return targetActive.Load() == 0 })
			err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
				list, err := l.Port().ListForwards(ctx)
				if err == nil {
					for _, state := range list.Forwards {
						if state.BoundPort != 0 || state.Connections != 0 || state.State != "stopped" {
							return errors.New("stopped forward retained listener or connection ownership")
						}
					}
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			rebound, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound))))
			if err != nil {
				t.Fatal("stopped forward retained its listening socket", err)
			}
			rebound.Close()
			if (i+1)%4 == 0 || i+1 == cycles {
				resources.Restored(t, fmt.Sprintf("port half-close/idle cancel %d/%d", i+1, cycles), baseline, 3, 6)
			}
		}
	}
	// A rejected remote handshake tears down its listener, preserves the
	// diagnostic snapshot, and permits the host to reapply the same desired ID.
	deniedSpec := proto.Clone(spec).(*ppb.ForwardSpec)
	deniedSpec.Id = "forward_denied"
	deniedSpec.RemotePort = 22
	err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
		var e error
		state, e = l.Port().StartForward(ctx, deniedSpec)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	deniedConn, err := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(state.BoundPort))))
	if err != nil {
		t.Fatal(err)
	}
	deniedConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = deniedConn.Read(make([]byte, 1))
	deniedConn.Close()
	if err == nil {
		t.Fatal("denied tunnel remained open")
	}
	wait(t, func() bool {
		_ = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			list, e := l.Port().ListForwards(ctx)
			for _, v := range list.GetForwards() {
				if v.Spec.Id == deniedSpec.Id {
					state = v
				}
			}
			return e
		})
		return state.Spec.Id == deniedSpec.Id && state.State == "error" && state.BoundPort == 0 && state.Connections == 0
	})
	if state.ReasonCode != "REMOTE_TARGET_DENIED" {
		t.Fatal("failure snapshot lost", state)
	}
	wait(t, func() bool {
		err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
			var e error
			state, e = l.Port().StartForward(ctx, deniedSpec)
			return e
		})
		return err == nil && state.State == "listening" && state.BoundPort != 0
	})
	err = client.Call(ctx, "port", sdk.PortWorkerCapability, func(ctx context.Context, l sdk.Lifecycle) error {
		return l.Port().StopForward(ctx, &ppb.ForwardID{Id: deniedSpec.Id})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	if err = server.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if stress != 0 {
		resources.Reaped(t, workerPIDs...)
	}
}
