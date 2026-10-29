package node

import (
	"context"
	"net"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"tailscale.com/tailcfg"
)

type idlePeerTerminal struct {
	rpc.UnimplementedShikiDaemonHandler
	closed chan struct{}
}

func (h *idlePeerTerminal) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	defer close(h.closed)
	if _, err := stream.Receive(); err != nil {
		return err
	}
	if err := stream.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("ready")}}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestPeerRemovalClosesEstablishedIdleStreams(t *testing.T) {
	for _, tc := range []struct {
		direction      string
		periodicExpiry bool
	}{{"inbound", false}, {"outbound", false}, {"inbound", true}, {"outbound", true}} {
		name := tc.direction + "/removed"
		if tc.periodicExpiry {
			name = tc.direction + "/expired-without-bus"
		}
		t.Run(name, func(t *testing.T) {
			control := localControl(t, true, "")
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			left := newTestNode(t, ctx, control, "peer-lifetime-client")
			right := newTestNode(t, ctx, control, "peer-lifetime-server")
			loginInteractive(t, left, control)
			loginInteractive(t, right, control)
			listener, err := right.Listen(9527)
			if err != nil {
				t.Fatal(err)
			}
			handler := &idlePeerTerminal{closed: make(chan struct{})}
			server := connect.NewServer()
			rpc.RegisterShikiDaemonHandler(server, handler)
			hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
			serveDone := make(chan error, 1)
			go func() { serveDone <- hs.Serve(listener) }()
			defer func() { hs.Close(); <-serveDone }()
			pool, err := left.NewRPCPool(net.JoinHostPort(right.snapshot().Addresses[0], "9527"), "shiki_test")
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			client := rpc.NewShikiDaemonClient(pool.Client)
			stream, err := client.TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "idle"}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Receive(); err != nil {
				t.Fatal(err)
			}
			observing, removed := left, right
			if tc.direction == "inbound" {
				observing, removed = right, left
			}
			local, err := observing.local.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			remote, err := removed.local.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			peer := control.Node(remote.Self.PublicKey)
			if peer == nil {
				t.Fatal("test peer is absent")
			}
			change := &tailcfg.MapResponse{PeersRemoved: []tailcfg.NodeID{peer.ID}}
			if tc.periodicExpiry {
				// Remove every bus wakeup from this observer; the bounded refresh
				// must still see live SDK expiry and retire the idle connection.
				observing.mu.Lock()
				observing.watchCancel()
				pollCtx, pollCancel := context.WithCancel(ctx)
				observing.watchCancel = pollCancel
				generation, lc := observing.generation, observing.local
				observing.wg.Add(1)
				go observing.pollStatus(pollCtx, lc, generation)
				observing.mu.Unlock()
				expired := *peer
				expired.Expired = true
				expired.KeyExpiry = time.Now().Add(-time.Minute)
				change = &tailcfg.MapResponse{PeersChanged: []*tailcfg.Node{&expired}}
			}
			started := time.Now()
			if !control.AddRawMapResponse(local.Self.PublicKey, change) {
				t.Fatal("peer removal was not published")
			}
			if tc.direction == "inbound" {
				select {
				case <-handler.closed:
				case <-time.After(3 * time.Second):
					t.Fatal("accepted idle RPC survived explicit peer removal")
				}
			} else {
				received := make(chan error, 1)
				go func() { _, err := stream.Receive(); received <- err }()
				select {
				case err := <-received:
					if err == nil {
						t.Fatal("removed peer's idle stream returned success")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("dialed idle RPC survived explicit peer removal")
				}
			}
			if ctx.Err() != nil || time.Since(started) > 3*time.Second {
				t.Fatal("stream ended only after the independent test lifetime or cancellation budget")
			}
		})
	}
}

func TestSDKListenerAcceptsFirstConnectionFromNewPeer(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	right := newTestNode(t, ctx, control, "early-listener")
	loginInteractive(t, right, control)
	listener, err := right.Listen(9527)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptError := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptError <- err
			return
		}
		accepted <- conn
	}()
	left := newTestNode(t, ctx, control, "later-peer")
	loginInteractive(t, left, control)
	leftStatus, err := left.local.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Observe publication, without making any preliminary client connection.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		right.mu.Lock()
		peer := right.peers[leftStatus.TailscaleIPs[0]]
		right.mu.Unlock()
		if peer.key == leftStatus.Self.PublicKey {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("new peer was not observed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	conn, err := left.Dial(ctx, "tcp", net.JoinHostPort(right.snapshot().Addresses[0], "9527"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case remote := <-accepted:
		defer remote.Close()
		if remote.(*peerConn).identity.key != leftStatus.Self.PublicKey {
			t.Fatal("listener bound a different peer identity")
		}
	case err := <-acceptError:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("listener rejected the new peer's first connection")
	}
}
