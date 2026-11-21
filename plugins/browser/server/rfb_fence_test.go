package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func fenceWireMessage(flags uint32, payload []byte) []byte {
	message := make([]byte, 9, 9+len(payload))
	message[0] = 248
	binary.BigEndian.PutUint32(message[4:8], flags)
	message[8] = byte(len(payload))
	return append(message, payload...)
}

func TestRFBFencePreservesFlagsAndBinaryPayload(t *testing.T) {
	// Fence payloads are opaque binary, including NUL, non-UTF8 bytes and
	// bytes that would be RFB message types if parsing lost its boundary.
	payloads := [][]byte{nil, {0, 0x7f, 0x80, 0xff, 248, 4}, bytes.Repeat([]byte{0, 0x7f, 0x80, 0xff}, 16)}
	for _, flags := range []uint32{0, 1, 2, 3, 1 << 31, 1<<31 | 3, 1<<31 | 4} {
		for _, payload := range payloads {
			t.Run(fmt.Sprintf("flags_%08x/payload_%d", flags, len(payload)), func(t *testing.T) {
				message := fenceWireMessage(flags, payload)
				// The following request must remain independently parseable.
				following := []byte{3, 1, 0, 0, 0, 0, 0, 100, 0, 100}
				reader := bytes.NewReader(append(append([]byte(nil), message...), following...))
				parsed, mutating, err := clientMessage(reader)
				if err != nil || mutating || !bytes.Equal(parsed, message) {
					t.Fatalf("Fence changed or gated as input: got %x, mutating=%v, err=%v; want %x", parsed, mutating, err, message)
				}
				parsed, mutating, err = clientMessage(reader)
				if err != nil || mutating || !bytes.Equal(parsed, following) || reader.Len() != 0 {
					t.Fatalf("Fence consumed following request: got %x, remaining=%d, err=%v", parsed, reader.Len(), err)
				}
			})
		}
	}
}

// Only Write and SetWriteDeadline are used by forwardInput. Embedding net.Conn
// keeps this recorder independent of Linux-only runtime test fixtures.
type fenceRecordingConnection struct {
	net.Conn
	output bytes.Buffer
}

func (connection *fenceRecordingConnection) Write(data []byte) (int, error) {
	return connection.output.Write(data)
}
func (*fenceRecordingConnection) SetWriteDeadline(time.Time) error { return nil }

func TestRFBFenceForwardingSurvivesReadOnlyAndRevisionChanges(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 0x7f, 0x80, 0xff}, 16)
	for _, flags := range []uint32{3, 1<<31 | 3, 1<<31 | 4} {
		message := fenceWireMessage(flags, payload)
		// Split within the flags, before length, at the payload boundary, and
		// within the payload; -1 exercises every byte as a separate DATA frame.
		for _, split := range []int{5, 8, 9, 11, -1} {
			for _, readOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("flags_%08x/split_%d/readonly_%v", flags, split, readOnly), func(t *testing.T) {
					worker := &Worker{controller: "viewer", controlRevision: "new"}
					connection := &fenceRecordingConnection{}
					viewer := &attachment{id: "viewer", profile: "profile", instance: "instance", connection: connection, canControl: !readOnly, readOnly: readOnly}
					parts := [][]byte{message}
					if split == -1 {
						parts = nil
						for i := range message {
							parts = append(parts, message[i:i+1])
						}
					} else {
						parts = [][]byte{message[:split], message[split:]}
					}
					input := make(chan received, len(parts)+3)
					handshake := append([]byte("RFB 003.008\n"), 1, 1)
					input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: handshake, ControlRevision: "old"}}
					for i, part := range parts {
						revision := "new"
						if i == 0 {
							revision = "old"
						}
						input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: part, ControlRevision: revision}}
					}
					// A stale key is still forbidden, while the following readonly
					// update remains valid. Fence preservation must not bypass gates.
					key := []byte{4, 1, 0, 0, 0, 0, 0, 65}
					update := []byte{3, 1, 0, 0, 0, 0, 0, 100, 0, 100}
					input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: append(key, update...), ControlRevision: "old"}}
					input <- received{err: io.EOF}
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					reader := &frameReader{ctx: ctx, input: input, viewer: viewer}
					if err := worker.forwardInput(ctx, viewer, reader); err != io.EOF {
						t.Fatalf("forwarding failed: %v", err)
					}
					want := append(append(append([]byte(nil), handshake...), message...), update...)
					if !bytes.Equal(connection.output.Bytes(), want) {
						t.Fatalf("Fence bytes changed/dropped or stale input escaped: got %x; want %x", connection.output.Bytes(), want)
					}
				})
			}
		}
	}
}
