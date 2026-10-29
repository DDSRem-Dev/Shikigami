package rotation_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
)

func testPortRotation(t *testing.T, root string, artifact binary) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	target, completed := echoTarget(t)
	remotePort := uint32(target.Addr().(*net.TCPAddr).Port)
	server := startSupervisor(t, ctx, root, "port-server", "port", "server", artifact, config(t, map[string]any{"allow_ports": []any{float64(remotePort)}, "idle_timeout_seconds": 60.0}), false, func() prpc.CoreAPIHandler { return &prpc.UnimplementedCoreAPIHandler{} }, nil)
	serverPID := resources.Children(t, artifact.path, 1)[0]
	api := daemonClient(t, ctx, root, server)
	spec := &ppb.ForwardSpec{Id: "forward_rotation", ServerId: "srv_test", RemoteAddress: "127.0.0.1", RemotePort: remotePort, LocalAddress: "127.0.0.1", Enabled: true, Revision: "rev_rotation"}
	var specMu sync.Mutex
	var bound atomic.Uint32
	client := startSupervisor(t, ctx, root, "port-client", "port", "client", artifact, config(t, map[string]any{"idle_timeout_seconds": 60.0}), true, func() prpc.CoreAPIHandler {
		return &host.RemoteCore{Resolve: resolve(api)}
	}, func(ctx context.Context, _ string, worker sdk.Lifecycle) error {
		// This is the fixture host's durable desired-forward replay, invoked by
		// the production Supervisor after each fresh Init/Start.
		specMu.Lock()
		defer specMu.Unlock()
		state, err := worker.Port().StartForward(ctx, proto.Clone(spec).(*ppb.ForwardSpec))
		if err == nil {
			spec.LocalPort = state.BoundPort
			bound.Store(state.BoundPort)
		}
		return err
	})
	first := firstLaunch(t, client)
	oldID, oldPID, oldSocket := client.ReadyInstanceID("port"), workerPID(t, artifact, serverPID), coreSocket(t, client)
	oldPort := bound.Load()
	old := dial(t, oldPort)
	defer old.Close()
	oldPayload := []byte("unique-old-connection-request")
	if _, err := old.Write(oldPayload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(oldPayload))
	if _, err := io.ReadFull(old, got); err != nil || !bytes.Equal(got, oldPayload) {
		t.Fatalf("old request was not acknowledged before expiry: %q %v", got, err)
	}
	wait(t, ctx, "live bidirectional callback before expiry", func() bool { return first.core.ports.active.Load() == 1 })
	if time.Until(first.started.Add(firstLifetime)) < time.Second {
		t.Fatal("insufficient lifetime left to establish pre-expiry evidence")
	}
	rotation(t, ctx, client, oldID, first, oldPID, oldSocket, &first.core.ports)
	if bound.Load() != oldPort {
		t.Fatal("replacement did not restore the desired listener")
	}
	if err := old.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	n, err := old.Read(one[:])
	var timeout net.Error
	if n != 0 || err == nil || errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatalf("old TCP connection survived rotation: n=%d err=%v", n, err)
	}
	old.Close()
	select {
	case body := <-completed:
		if !bytes.Equal(body, oldPayload) {
			t.Fatal("old target bytes changed")
		}
	case <-ctx.Done():
		t.Fatal("old remote target was not closed")
	}
	fresh := dial(t, oldPort)
	defer fresh.Close()
	payload := bytes.Repeat([]byte("new-connection-only\x00"), 16384)
	if _, err := fresh.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := fresh.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(fresh)
	if err != nil || !bytes.Equal(response, append(append([]byte{}, payload...), []byte("half-close-tail")...)) {
		t.Fatalf("new TCP bytes/half-close failed: len=%d err=%v", len(response), err)
	}
	fresh.Close()
	select {
	case body := <-completed:
		if !bytes.Equal(body, payload) {
			t.Fatal("new target received replayed or corrupted bytes")
		}
	case <-ctx.Done():
		t.Fatal("new target did not finish after half-close")
	}
	remainingPIDs := resources.Children(t, artifact.path, 2)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	resources.Reaped(t, remainingPIDs...)
	select {
	case <-completed:
		t.Fatal("unexpected target connection/replay")
	default:
	}
	t.Logf("new port instance restored listener %d; old TCP closed; new payload=%d bytes and half-close tail verified without replay", oldPort, len(payload))
}

func dial(t *testing.T, port uint32) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)})
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.SetDeadline(time.Now().Add(25 * time.Second)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	return conn
}

func echoTarget(t *testing.T) (net.Listener, <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan []byte, 8)
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			active[conn] = true
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { conn.Close(); mu.Lock(); delete(active, conn); mu.Unlock() }()
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				var body bytes.Buffer
				chunk := make([]byte, 32<<10)
				for {
					n, err := conn.Read(chunk)
					if n > 0 {
						body.Write(chunk[:n])
						if _, e := conn.Write(chunk[:n]); e != nil {
							break
						}
					}
					if err != nil {
						if errors.Is(err, io.EOF) {
							_, _ = conn.Write([]byte("half-close-tail"))
						}
						break
					}
				}
				completed <- append([]byte{}, body.Bytes()...)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
		mu.Lock()
		for conn := range active {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener, completed
}
