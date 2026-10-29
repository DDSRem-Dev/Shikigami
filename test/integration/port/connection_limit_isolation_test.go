package portplugin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	coreRPC "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	daemonRPC "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
)

// Each local application connection has its own real CoreAPI -> TLS daemon ->
// server-worker stream. Rejecting one over-capacity stream must not cancel the
// listener or the two already admitted streams that share that route.
func TestProductionPortRemoteLimitKeepsExistingConnectionsAndListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-port-isolation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	binary := filepath.Join(dir, "port.race")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "../../../plugins/port")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
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
	sum := sha256.Sum256(script)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted, active atomic.Int64
	var targetMu sync.Mutex
	connections := make(map[net.Conn]struct{})
	var handlers sync.WaitGroup
	acceptDone := make(chan struct{})
	targetErrors := make(chan error, 4)
	const tail = "after-client-half-close"
	go func() {
		defer close(acceptDone)
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			targetMu.Lock()
			connections[conn] = struct{}{}
			targetMu.Unlock()
			accepted.Add(1)
			active.Add(1)
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer active.Add(-1)
				defer conn.Close()
				defer func() { targetMu.Lock(); delete(connections, conn); targetMu.Unlock() }()
				err := conn.SetDeadline(time.Now().Add(45 * time.Second))
				if err == nil {
					// Echo immediately, but keep the connection open until its own
					// client HALF_CLOSE; only then send independent trailing data.
					_, err = io.Copy(conn, conn)
				}
				if err == nil {
					_, err = io.WriteString(conn, tail)
				}
				if err != nil {
					select {
					case targetErrors <- err:
					default:
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = target.Close()
		<-acceptDone // No further WaitGroup Add or owned-connection registration.
		targetMu.Lock()
		for conn := range connections {
			_ = conn.Close()
		}
		targetMu.Unlock()
		handlers.Wait()
	})
	remotePort := uint32(target.Addr().(*net.TCPAddr).Port)
	serverWorker := supervisor(t, ctx, dir, executable, hex.EncodeToString(sum[:]), "server", map[string]any{
		"allow_ports": []any{float64(remotePort)}, "max_connections": 2.0, "idle_timeout_seconds": 30.0,
	}, &coreRPC.UnimplementedCoreAPIHandler{})
	manager, err := auth.Open(filepath.Join(dir, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	service := &server.Service{Auth: manager, DaemonDataPlane: host.DaemonDataPlane{Supervisor: serverWorker}}
	httpServer := httptest.NewUnstartedServer(service.Handler())
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	t.Cleanup(httpServer.Close)
	roots := x509.NewCertPool()
	roots.AddCert(httpServer.Certificate())
	endpoint, err := connection.NewTLSClient(httpServer.URL, connection.TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	token, _, err := manager.Create("tester", []string{"port.forward"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bearer, err := connection.BearerToken(token)
	if err != nil {
		t.Fatal(err)
	}
	remote := daemonRPC.NewShikiDaemonClient(connection.NewRPCClient(endpoint, bearer))
	core := &host.RemoteCore{Resolve: func(_ context.Context, scope *ppb.Scope) (daemonRPC.ShikiDaemonClient, error) {
		if scope.ServerId != "srv_test" {
			return nil, connect.NewError(connect.CodePermissionDenied, "bad scope")
		}
		return remote, nil
	}}
	clientWorker := supervisor(t, ctx, dir, executable, hex.EncodeToString(sum[:]), "client", map[string]any{
		"max_connections": 4.0, "idle_timeout_seconds": 30.0,
	}, core)
	pids := resources.Children(t, binary, 2)
	spec := &ppb.ForwardSpec{Id: "forward_isolation", ServerId: "srv_test", RemoteAddress: "127.0.0.1", RemotePort: remotePort, LocalAddress: "127.0.0.1", Enabled: true, Revision: "rev_isolation"}
	var state *ppb.ForwardState
	if err := clientWorker.Call(ctx, "port", sdk.PortWorkerCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
		var err error
		state, err = lifecycle.Port().StartForward(call, spec)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if state == nil || state.BoundPort == 0 {
		t.Fatal("forward did not bind", state)
	}
	bound := state.BoundPort
	dial := func() *net.TCPConn {
		t.Helper()
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(bound)), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn.(*net.TCPConn)
	}
	echo := func(conn *net.TCPConn, body string) {
		t.Helper()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, body); err != nil {
			t.Fatalf("existing connection write %q: %v", body, err)
		}
		got := make([]byte, len(body))
		if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, []byte(body)) {
			t.Fatalf("existing connection echo %q: got %q, err %v", body, got, err)
		}
	}
	finish := func(conn *net.TCPConn) {
		t.Helper()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := conn.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(conn)
		if err != nil || string(got) != tail {
			t.Fatalf("half-close lost independent tail or final EOF: %q, %v", got, err)
		}
		_ = conn.Close()
	}
	waitCount := func(want uint64) {
		t.Helper()
		wait(t, func() bool {
			var found *ppb.ForwardState
			err := clientWorker.Call(ctx, "port", sdk.PortWorkerCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
				list, err := lifecycle.Port().ListForwards(call)
				for _, current := range list.GetForwards() {
					if current.Spec.Id == spec.Id {
						found = current
					}
				}
				return err
			})
			return err == nil && found != nil && found.State == "listening" && found.ReasonCode == "" && found.BoundPort == bound && found.Connections == want
		})
	}
	first, second := dial(), dial()
	echo(first, "first-before-limit")
	echo(second, "second-before-limit")
	waitCount(2)
	if accepted.Load() != 2 || active.Load() != 2 {
		t.Fatal("two held streams did not reach the real target", accepted.Load(), active.Load())
	}
	// Confirm the server worker's actual quota and final status independently;
	// the subsequent third local socket still exercises the whole client chain.
	probeCtx, stopProbe := context.WithTimeout(ctx, 5*time.Second)
	defer stopProbe()
	probe, err := remote.ForwardPort(probeCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := probe.Send(&pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "quota_probe", RemoteAddress: "127.0.0.1", Port: int32(remotePort)}); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	_, err = probe.Receive()
	_ = probe.Close()
	stopProbe()
	var remoteError *connect.Error
	if connect.CodeOf(err) != connect.CodeResourceExhausted || !errors.As(err, &remoteError) || !remoteError.IsRemote() || remoteError.Message() != "resource limit exceeded" {
		t.Fatalf("actual remote connection quota status: %v", err)
	}
	overflow := dial()
	if err := overflow.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(overflow, "overflow-must-not-reach-target")
	n, rejected := overflow.Read(make([]byte, 1))
	_ = overflow.Close()
	var timeout net.Error
	if n != 0 || rejected == nil || (errors.As(rejected, &timeout) && timeout.Timeout()) {
		t.Fatalf("third local connection was not actively rejected: n=%d err=%v", n, rejected)
	}
	// These writes are the regression assertion: the old worker cancels both
	// streams and its listener when the third handshake is ResourceExhausted.
	echo(first, "first-still-live-after-rejection")
	echo(second, "second-still-live-after-rejection")
	waitCount(2)
	if accepted.Load() != 2 || active.Load() != 2 {
		t.Fatal("rejection reached target or canceled admitted streams", accepted.Load(), active.Load())
	}
	finish(first)
	waitCount(1)
	wait(t, func() bool { return active.Load() == 1 })
	replacement := dial()
	echo(replacement, "replacement-uses-exactly-one-released-slot")
	echo(second, "second-survives-slot-replacement")
	waitCount(2)
	if accepted.Load() != 3 || active.Load() != 2 {
		t.Fatal("released server slot did not admit exactly one replacement", accepted.Load(), active.Load())
	}
	finish(second)
	finish(replacement)
	waitCount(0)
	wait(t, func() bool { return active.Load() == 0 })
	select {
	case err := <-targetErrors:
		t.Fatal("real target stream failed", err)
	default:
	}
	if err := clientWorker.Call(ctx, "port", sdk.PortWorkerCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
		return lifecycle.Port().StopForward(call, &ppb.ForwardID{Id: spec.Id})
	}); err != nil {
		t.Fatal(err)
	}
	rebound, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(bound)))
	if err != nil {
		t.Fatal("explicit Stop did not release listener", err)
	}
	_ = rebound.Close()
	if err := clientWorker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := serverWorker.Close(); err != nil {
		t.Fatal(err)
	}
	resources.Reaped(t, pids...)
}
