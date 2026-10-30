package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"golang.org/x/net/http2"
)

func TestGracefulHTTP2ShutdownDoesNotReplayStreamAndAllowsFreshProbe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var probes atomic.Int32
	var mu sync.Mutex
	var connections []net.Conn
	var workers sync.WaitGroup
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, &routeProbe{probe: func(ctx context.Context) (*pb.ProtocolAgreement, error) {
		probes.Add(1)
		return routeAgreement(ctx)
	}})
	shutdownSent := make(chan error, 1)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			first := accepted.Add(1) == 1
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				if first {
					shutdownSent <- sendGoAwayAfterData(conn)
					return
				}
				(&http2.Server{}).ServeConn(conn, &http2.ServeConnOpts{Handler: connection.NewRPCHandler(server)})
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-acceptDone
		mu.Lock()
		for _, conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()
	endpoint := &connection.Endpoint{HTTPClient: &http.Client{Transport: transport}, BaseURL: "http://" + listener.Addr().String()}
	bearer, err := connection.BearerToken("shiki_route_fixture")
	if err != nil {
		t.Fatal(err)
	}
	base := connection.NewRPCClient(endpoint, bearer)
	client := rpc.NewShikiDaemonClient(base)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	sendErr := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess_not_replayed"}}})
	_, receiveErr := stream.Receive()
	err = receiveErr
	if err == nil {
		err = sendErr
	}
	if serverErr := <-shutdownSent; serverErr != nil {
		t.Fatal(serverErr)
	}
	if err == nil {
		t.Fatal("GOAWAY did not terminate old stream")
	}
	if accepted.Load() != 1 || probes.Load() != 0 {
		t.Fatal("stream body was replayed automatically")
	}
	var ce *connect.Error
	remote := errors.As(err, &ce) && ce.IsRemote()
	nonReplayable := strings.Contains(err.Error(), "after Request.Body was written; define Request.GetBody to avoid this error")
	t.Logf("old_stream_code=%s remote=%t non_replayable_body=%t retryable=%t connections=%d", connect.CodeOf(err), remote, nonReplayable, connection.IsRetryableNetworkError(err), accepted.Load())
	// The new read-only probe cannot replay the failed terminal request body.
	agreement, daemonID, probeErr := negotiateRemote(ctx, base, "daemon_route")
	if probeErr != nil || agreement == nil || daemonID != "daemon_route" || probes.Load() != 1 || accepted.Load() != 2 {
		t.Fatalf("fresh authenticated probe did not recover: code=%s probes=%d connections=%d", connect.CodeOf(probeErr), probes.Load(), accepted.Load())
	}
}

func sendGoAwayAfterData(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return err
	}
	if string(preface) != http2.ClientPreface {
		return errors.New("unexpected HTTP/2 preface")
	}
	framer := http2.NewFramer(conn, conn)
	if err := framer.WriteSettings(); err != nil {
		return err
	}
	for {
		frame, err := framer.ReadFrame()
		if err != nil {
			return err
		}
		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				if err := framer.WriteSettingsAck(); err != nil {
					return err
				}
			}
		case *http2.DataFrame:
			return framer.WriteGoAway(0, http2.ErrCodeNo, nil)
		}
	}
}
