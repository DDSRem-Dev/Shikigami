package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// Retaining messages after Send returns matches transports whose tracing or
// stats handlers can inspect a frame later. No copying is performed here.
type retainedFrames struct {
	ctx      context.Context
	frames   []*pb.PortData
	accepted chan int
	retain   bool
}

func (s *retainedFrames) Send(frame *pb.PortData) error {
	if s.retain {
		s.frames = append(s.frames, frame)
	}
	if frame.Type == pb.PortData_DATA {
		select {
		case s.accepted <- len(frame.Data):
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	return nil
}
func (*retainedFrames) Receive() (*pb.PortData, error) { return nil, io.EOF }

func ownershipTCPPair(t testing.TB) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	client.SetDeadline(deadline)
	server.SetDeadline(deadline)
	return client, server
}

func sendOwnedChunks(t testing.TB, chunks [][]byte, retain bool) []*pb.PortData {
	t.Helper()
	app, conn := ownershipTCPPair(t)
	defer app.Close()
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := &retainedFrames{ctx: ctx, accepted: make(chan int, 1), retain: retain}
	done := make(chan error, 1)
	go func() { done <- send(ctx, conn, stream, Options{ID: "retained"}, nil, func() {}, func(int) {}) }()
	joined := false
	defer func() {
		cancel()
		conn.Close()
		if !joined {
			<-done
		}
	}()
	// Each new chunk starts only after all preceding bytes have been handed
	// to Send; earlier frames remain retained throughout subsequent TCP reads.
	for _, chunk := range chunks {
		if _, err := io.Copy(app, bytes.NewReader(chunk)); err != nil {
			t.Fatal(err)
		}
		for received := 0; received < len(chunk); {
			select {
			case n := <-stream.accepted:
				received += n
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	if err := app.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return stream.frames
}

func TestSentFramesRetainPayloadAcrossLaterTCPReads(t *testing.T) {
	chunks := [][]byte{bytes.Repeat([]byte("a"), 32<<10), bytes.Repeat([]byte("b"), 32<<10), bytes.Repeat([]byte("c"), 32<<10)}
	frames := sendOwnedChunks(t, chunks, true)
	var got []byte
	for i, frame := range frames {
		if frame.ConnectionId != "retained" {
			t.Fatal("changed connection identity")
		}
		if i == len(frames)-1 {
			if frame.Type != pb.PortData_HALF_CLOSE || len(frame.Data) != 0 {
				t.Fatal("missing final half-close")
			}
			continue
		}
		if frame.Type != pb.PortData_DATA {
			t.Fatal("unexpected control frame")
		}
		got = append(got, frame.Data...)
	}
	if !bytes.Equal(got, bytes.Join(chunks, nil)) {
		t.Fatal("later TCP reads mutated retained frame payloads")
	}
}

func BenchmarkSendPacedChunks(b *testing.B) {
	for _, size := range []int{32 << 10, 256 << 10} {
		b.Run(fmt.Sprintf("chunk_%d", size), func(b *testing.B) {
			chunks := make([][]byte, (1<<20)/size)
			for i := range chunks {
				chunks[i] = bytes.Repeat([]byte{byte(i)}, size)
			}
			b.ReportAllocs()
			b.SetBytes(1 << 20)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sendOwnedChunks(b, chunks, false)
			}
		})
	}
}
