//go:build linux

package server

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

type recordingConnection struct{ bytes.Buffer }

func (connection *recordingConnection) Read([]byte) (int, error) { return 0, io.EOF }
func (connection *recordingConnection) Close() error             { return nil }
func (connection *recordingConnection) LocalAddr() net.Addr {
	return &net.UnixAddr{Name: "test", Net: "unix"}
}
func (connection *recordingConnection) RemoteAddr() net.Addr {
	return &net.UnixAddr{Name: "test", Net: "unix"}
}
func (connection *recordingConnection) SetDeadline(time.Time) error      { return nil }
func (connection *recordingConnection) SetReadDeadline(time.Time) error  { return nil }
func (connection *recordingConnection) SetWriteDeadline(time.Time) error { return nil }
func cutText(data []byte, extended bool) []byte {
	message := make([]byte, 8)
	message[0] = 6
	length := int32(len(data))
	if extended {
		length = -length
	}
	binary.BigEndian.PutUint32(message[4:], uint32(length))
	return append(message, data...)
}
func TestRFBParsesAndGatesEveryKnownMutation(t *testing.T) {
	key := []byte{4, 1, 0, 0, 0, 0, 0, 65}
	pointer := []byte{5, 1, 0, 1, 0, 1}
	resize := []byte{251, 0, 0, 100, 0, 100, 1, 0}
	resize = append(resize, make([]byte, 16)...)
	binary.BigEndian.PutUint16(resize[16:18], 100)
	binary.BigEndian.PutUint16(resize[18:20], 100)
	qemu := []byte{255, 0, 0, 1, 0, 0, 0, 65, 0, 0, 0, 30}
	extended := make([]byte, 4)
	binary.BigEndian.PutUint32(extended, 1<<27|1)
	for name, message := range map[string][]byte{"key": key, "pointer": pointer, "resize": resize, "cuttext": cutText([]byte("clipboard"), false), "extended-clipboard": cutText(extended, true), "qemu-key": qemu} {
		t.Run(name, func(t *testing.T) {
			parsed, mutating, err := clientMessage(bytes.NewReader(message))
			if err != nil || !mutating || !bytes.Equal(parsed, message) {
				t.Fatalf("parse mutation=%v err=%v", mutating, err)
			}
			for _, eligible := range []bool{false, true} {
				worker := NewWithManager(&testManager{})
				connection := &recordingConnection{}
				worker.controller = "viewer"
				worker.controlRevision = "revision"
				viewer := &attachment{id: "viewer", profile: "one", instance: "instance", connection: connection, canControl: eligible, readOnly: !eligible}
				input := make(chan received, 2)
				handshake := append([]byte("RFB 003.008\n"), 1, 1)
				data := append(append([]byte(nil), handshake...), message...)
				input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: data, ControlRevision: "revision"}}
				input <- received{err: io.EOF}
				err = worker.forwardInput(context.Background(), viewer, &frameReader{ctx: context.Background(), viewer: viewer, input: input})
				if err != io.EOF {
					t.Fatalf("gate %v", err)
				}
				expected := len(handshake)
				if eligible {
					expected += len(message)
				}
				if connection.Len() != expected {
					t.Fatalf("read-only=%v forwarded %d want%d", !eligible, connection.Len(), expected)
				}
			}
		})
	}
}
func TestRFBRejectsUnknownOversizedAndCompressedBomb(t *testing.T) {
	oversized := make([]byte, 8)
	oversized[0] = 6
	binary.BigEndian.PutUint32(oversized[4:], maxClipboard+1)
	encodings := []byte{2, 0, 255, 255}
	badResize := []byte{251, 0, 255, 255, 255, 255, 1, 0}
	for _, message := range [][]byte{{1}, {250, 0, 0, 0}, oversized, encodings, badResize} {
		if _, _, err := clientMessage(bytes.NewReader(message)); err == nil {
			t.Fatalf("accepted unsafe RFB %v", message)
		}
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	writer.Write(bytes.Repeat([]byte{0}, maxClipboard+1))
	writer.Close()
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, 1<<28|1)
	payload = append(payload, compressed.Bytes()...)
	if _, _, err := clientMessage(bytes.NewReader(cutText(payload, true))); err == nil {
		t.Fatal("accepted expanded clipboard bomb")
	}
}

func TestRFBClipboardAcceptsFlushedUnicodeAndRejectsTruncation(t *testing.T) {
	text := []byte("日本語 / 中文\r\nclipboard\x00")
	plain := make([]byte, 4, 4+len(text))
	binary.BigEndian.PutUint32(plain, uint32(len(text)))
	plain = append(plain, text...)
	frame := func(data []byte) []byte {
		t.Helper()
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
		// Like noVNC's first Z_FULL_FLUSH block: complete data, no zlib
		// stream terminator/checksum. Do not Close before taking the bytes.
		if err := writer.Flush(); err != nil {
			t.Fatal(err)
		}
		payload := make([]byte, 4, 4+compressed.Len())
		binary.BigEndian.PutUint32(payload, 1<<28|1)
		payload = append(payload, compressed.Bytes()...)
		writer.Close()
		return cutText(payload, true)
	}
	valid := frame(plain)
	parsed, mutating, err := clientMessage(bytes.NewReader(valid))
	if err != nil || !mutating || !bytes.Equal(parsed, valid) {
		t.Fatalf("valid flushed Unicode clipboard was rejected: %v", err)
	}
	for _, malformed := range [][]byte{
		frame(plain[:len(plain)-1]),
		frame(append(append([]byte(nil), plain...), 1)),
		valid[:len(valid)-1],
	} {
		if _, _, err := clientMessage(bytes.NewReader(malformed)); err == nil {
			t.Fatal("accepted truncated or trailing clipboard data")
		}
	}
}
func TestControlOwnershipRevisionAndNoPromotion(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	status := startProfile(t, worker, "one", "start")
	for _, id := range []string{"first", "second", "readonly"} {
		_, cancel := context.WithCancel(context.Background())
		worker.attachments[id] = &attachment{id: id, profile: "one", instance: status.InstanceId, owner: "owner", canControl: id != "readonly", readOnly: id == "readonly", connection: &recordingConnection{}, cancel: cancel, out: make(chan *epb.BrowserFrame, 8)}
	}
	if worker.controller != "" {
		t.Fatal("automatic controller assignment")
	}
	request := &epb.BrowserControlRequest{ProfileId: "one", InstanceId: status.InstanceId, AttachmentId: "first", OwnerId: "wrong", ExpectedControlRevision: worker.controlRevision}
	if _, err := worker.TakeBrowserControl(context.Background(), request); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("owner: %v", err)
	}
	request.OwnerId = "owner"
	taken, err := worker.TakeBrowserControl(context.Background(), request)
	if err != nil || taken.ControllerId != "first" {
		t.Fatalf("take %#v %v", taken, err)
	}
	request.AttachmentId = "second"
	if _, err := worker.TakeBrowserControl(context.Background(), request); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale takeover: %v", err)
	}
	request.ExpectedControlRevision = taken.ControlRevision
	request.AttachmentId = "readonly"
	if _, err := worker.TakeBrowserControl(context.Background(), request); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("readonly takeover: %v", err)
	}
	request.AttachmentId = "second"
	taken, err = worker.TakeBrowserControl(context.Background(), request)
	if err != nil || taken.ControllerId != "second" {
		t.Fatal(err)
	}
	worker.control.Lock()
	delete(worker.attachments, "second")
	worker.controller = ""
	worker.controlRevision = browserruntime.NewID()
	worker.broadcastControlLocked()
	worker.control.Unlock()
	if worker.controller != "" {
		t.Fatal("disconnect promoted viewer")
	}
}
func TestRFBStaleRevisionInputNeverReachesController(t *testing.T) {
	worker := NewWithManager(&testManager{})
	worker.controller = "viewer"
	worker.controlRevision = "new"
	connection := &recordingConnection{}
	viewer := &attachment{id: "viewer", profile: "one", instance: "instance", canControl: true, connection: connection}
	handshake := append([]byte("RFB 003.008\n"), 1, 1)
	key := []byte{4, 1, 0, 0, 0, 0, 0, 65}
	input := make(chan received, 2)
	input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: append(handshake, key...), ControlRevision: "old"}}
	input <- received{err: io.EOF}
	if err := worker.forwardInput(context.Background(), viewer, &frameReader{ctx: context.Background(), viewer: viewer, input: input}); err != io.EOF {
		t.Fatal(err)
	}
	if connection.Len() != len(handshake) {
		t.Fatal("stale revision reached Chrome")
	}
}

type blockedBrowserStream struct {
	opened  bool
	ready   chan *epb.BrowserFrame
	release chan struct{}
}

func (stream *blockedBrowserStream) Receive() (*epb.BrowserFrame, error) {
	if !stream.opened {
		stream.opened = true
		return &epb.BrowserFrame{Kind: epb.BrowserFrame_OPEN, ProfileId: "one", OwnerId: "owner", CanControl: true}, nil
	}
	<-stream.release
	return nil, context.Canceled
}
func (stream *blockedBrowserStream) Send(frame *epb.BrowserFrame) error {
	if frame.Kind == epb.BrowserFrame_READY {
		stream.ready <- frame
	}
	<-stream.release
	return context.Canceled
}

func TestPluginStopCancelsBlockedRFBWriterAndReleasesController(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "start")
	stream := &blockedBrowserStream{ready: make(chan *epb.BrowserFrame, 1), release: make(chan struct{})}
	defer close(stream.release)
	result := make(chan error, 1)
	go func() { result <- worker.BrowserStream(context.Background(), stream) }()
	var ready *epb.BrowserFrame
	select {
	case ready = <-stream.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("missing READY")
	}
	if ready.ControllerId != "" {
		t.Fatal("first attachment was auto-promoted")
	}
	if _, err := worker.TakeBrowserControl(context.Background(), &epb.BrowserControlRequest{ProfileId: "one", InstanceId: ready.InstanceId, AttachmentId: ready.AttachmentId, OwnerId: "owner", ExpectedControlRevision: ready.ControlRevision}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Stop(ctx); err != nil {
		t.Fatalf("Stop deadlocked on RFB writer: %v", err)
	}
	select {
	case <-result:
	case <-ctx.Done():
		t.Fatal("RFB handler did not return")
	}
	worker.control.Lock()
	controller, attachments := worker.controller, len(worker.attachments)
	worker.control.Unlock()
	if controller != "" || attachments != 0 || manager.stops != 0 {
		t.Fatalf("disconnect control/browser: controller=%s attachments=%d stops=%d", controller, attachments, manager.stops)
	}
	for _, peer := range manager.peers {
		peer.Close()
	}
}

func TestReadOnlyRFBStillAllowsBoundedFramebufferNegotiation(t *testing.T) {
	pixel := make([]byte, 20)
	pixel[0] = 0
	copy(pixel[4:], []byte{32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0})
	update := []byte{3, 1, 0, 0, 0, 0, 0, 100, 0, 100}
	for _, message := range [][]byte{pixel, {2, 0, 0, 0}, update, {150, 1, 0, 0, 0, 0, 0, 100, 0, 100}, {248, 0, 0, 0, 0, 0, 0, 0, 0}} {
		parsed, mutating, err := clientMessage(bytes.NewReader(message))
		if err != nil || mutating || !bytes.Equal(parsed, message) {
			t.Fatalf("read-only negotiation rejected: mutating=%v err=%v", mutating, err)
		}
	}
	pixel[5] = 255
	if _, _, err := clientMessage(bytes.NewReader(pixel)); err == nil {
		t.Fatal("invalid pixel depth accepted")
	}
}

func TestRFBFrameIncludesMetadataInItsBound(t *testing.T) {
	input := make(chan received, 1)
	input <- received{frame: &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, Data: []byte{3}, ControlRevision: string(make([]byte, browserruntime.MaxWireFrame+4096))}}
	reader := &frameReader{ctx: context.Background(), input: input, viewer: &attachment{id: "viewer", profile: "one", instance: "instance"}}
	var output [1]byte
	if _, err := reader.Read(output[:]); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversized metadata accepted: %v", err)
	}
}
