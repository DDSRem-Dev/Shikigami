//go:build memoryprofile

package memoryprofile_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/types/known/structpb"
)

const tcpBytes = 1 << 20
const responseTail = "MEMORY_PROFILE_AFTER_HALF_CLOSE\n"

func tcpPayload(sequence int) []byte {
	payload := bytes.Repeat([]byte{byte(sequence), 0xff, 0, byte(sequence + 1)}, tcpBytes/4)
	copy(payload, fmt.Sprintf("memory-profile-%08d", sequence))
	return payload
}

func runTCPStage(t *testing.T, f *dataFixture) (time.Duration, int64) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int64
	results := make(chan error, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			sequence := int(accepted.Add(1) - 1)
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			// Read until the sender's half-close. Bound unexpected extra bytes and
			// validate the exact unique sequence, so replay is never accepted.
			data, err := io.ReadAll(io.LimitReader(conn, tcpBytes+1))
			if err == nil && (sequence >= 32 || len(data) != tcpBytes || sha256.Sum256(data) != sha256.Sum256(tcpPayload(sequence))) {
				err = errors.New("target payload/sequence/length mismatch or replay")
			}
			if err == nil {
				_, err = io.Copy(conn, bytes.NewReader(append(data, []byte(responseTail)...)))
			}
			conn.Close()
			results <- err
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	port := listener.Addr().(*net.TCPAddr).Port
	values, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(port)}, "idle_timeout_seconds": 30.0})
	if err != nil {
		t.Fatal(err)
	}
	f.manage(t, "port", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_SET_CONFIG, values)
	f.manage(t, "port", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_ENABLE, nil)
	f.manage(t, "port", apb.PluginTarget_LOCAL, pb.ManagePluginRequest_ENABLE, nil)
	forward, err := f.client.Agent().CreateForward(f.ctx, &apb.CreateForwardRequest{RequestId: "fixed-forward", Spec: &ppb.ForwardSpec{ServerId: f.serverID, RemoteAddress: "127.0.0.1", RemotePort: uint32(port), LocalAddress: "127.0.0.1"}})
	if err != nil || forward.GetState() != "listening" || forward.GetBoundPort() == 0 {
		t.Fatal("forward", forward, err)
	}
	settle := func() {
		ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		if err := await(ctx, func() error {
			list, err := f.client.Agent().ListForwards(ctx, &pb.Empty{})
			if err != nil {
				return err
			}
			for _, state := range list.Forwards {
				if state.GetSpec().GetId() == forward.Spec.Id && state.State == "listening" && state.BoundPort == forward.BoundPort && state.Connections == 0 {
					return nil
				}
			}
			return errors.New("forward connections not reclaimed")
		}); err != nil {
			t.Fatal(err)
		}
	}
	settle()
	pids := f.samples(t, "port", "tcp-baseline")
	started := time.Now()
	for sequence := 0; sequence < 32; sequence++ {
		payload := tcpPayload(sequence)
		if err := tcpExchange(f.ctx, forward.BoundPort, payload); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
		settle()
		if got := accepted.Load(); got != int64(sequence+1) {
			t.Fatal("target extra/replayed connection", got)
		}
	}
	elapsed := time.Since(started)
	f.samples(t, "port", "tcp-loaded")
	f.idle(t, elapsed)
	f.samples(t, "port", "tcp-idle")
	if _, err := f.client.Agent().StopForward(f.ctx, &apb.StopForwardRequest{ForwardId: forward.Spec.Id, ExpectedRevision: forward.Spec.Revision, RequestId: "stop-forward"}); err != nil {
		t.Fatal(err)
	}
	f.stopWorkers(t, "port", pids)
	listener.Close()
	<-done
	if accepted.Load() != 32 {
		t.Fatal("target accepted unexpected connection count", accepted.Load())
	}
	select {
	case err := <-results:
		t.Fatal("extra target result", err)
	default:
	}
	t.Logf("TCP stage: 32 x 1MiB, accepted=%d elapsed=%s, half-close tail and both SHA256s verified; workers exited", accepted.Load(), elapsed)
	return elapsed, accepted.Load()
}

func tcpExchange(ctx context.Context, port uint32, payload []byte) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	readDone := make(chan error, 1)
	go func() {
		got, err := io.ReadAll(io.LimitReader(conn, tcpBytes+int64(len(responseTail))+1))
		if err == nil && (len(got) != tcpBytes+len(responseTail) || sha256.Sum256(got[:min(len(got), tcpBytes)]) != sha256.Sum256(payload) || !bytes.Equal(got[min(len(got), tcpBytes):], []byte(responseTail))) {
			err = errors.New("response length/hash/half-close tail mismatch")
		}
		readDone <- err
	}()
	joined := false
	defer func() {
		conn.Close()
		if !joined {
			<-readDone
		}
	}()
	for offset := 0; offset < len(payload); {
		end := min(len(payload), offset+(32<<10))
		if _, err := io.Copy(conn, bytes.NewReader(payload[offset:end])); err != nil {
			return err
		}
		offset = end
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return err
	}
	err = <-readDone
	joined = true
	return err
}
