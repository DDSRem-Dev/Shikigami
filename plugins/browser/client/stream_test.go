package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

type viewerTestScreen struct {
	ctx        context.Context
	cancel     context.CancelFunc
	incoming   chan *epb.BrowserFrame
	sent       chan *epb.BrowserFrame
	sending    atomic.Int32
	concurrent atomic.Bool
}

func newViewerTestScreen(ctx context.Context) *viewerTestScreen {
	lifetime, cancel := context.WithCancel(ctx)
	return &viewerTestScreen{ctx: lifetime, cancel: cancel, incoming: make(chan *epb.BrowserFrame, 16), sent: make(chan *epb.BrowserFrame, 16)}
}
func (s *viewerTestScreen) Send(frame *epb.BrowserFrame) error {
	if s.sending.Add(1) != 1 {
		s.concurrent.Store(true)
	}
	defer s.sending.Add(-1)
	select {
	case s.sent <- proto.Clone(frame).(*epb.BrowserFrame):
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	if frame.Kind == epb.BrowserFrame_OPEN {
		s.incoming <- &epb.BrowserFrame{Kind: epb.BrowserFrame_READY, ProfileId: "profile", InstanceId: "instance", AttachmentId: "attachment", ControlRevision: "revision-1"}
		s.incoming <- &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ProfileId: "profile", InstanceId: "instance", AttachmentId: "attachment", Data: []byte("RFB 003.008\n")}
	}
	return nil
}
func (s *viewerTestScreen) Receive() (*epb.BrowserFrame, error) {
	select {
	case frame := <-s.incoming:
		return frame, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
func (*viewerTestScreen) CloseSend() error { return nil }
func (s *viewerTestScreen) Close() error   { s.cancel(); return nil }

func readViewerScreenEvent(t *testing.T, ctx context.Context, socket *websocket.Conn) screenEvent {
	t.Helper()
	kind, data, err := socket.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageText {
		t.Fatalf("expected screen metadata, got %d", kind)
	}
	var event screenEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestViewerRFBBridgeScopeControllerAndQueuedRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created := make(chan *viewerTestScreen, 1)
	browser := &viewerTestBrowser{stream: func(ctx context.Context) (sdk.BrowserClientStream, error) {
		screen := newViewerTestScreen(ctx)
		created <- screen
		return screen, nil
	}}
	w := startViewerTestWorker(t, browser)
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	target := strings.Replace(w.origin, "http:", "ws:", 1) + "/v/" + viewer.Id + "/rfb"
	socket, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": []string{w.origin}}, Subprotocols: []string{viewerSubprotocol, "csrf." + csrf}})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	screen := <-created
	open := <-screen.sent
	if open.Kind != epb.BrowserFrame_OPEN || open.ServerId != "server" || open.ProfileId != "profile" || open.OwnerId != "" || open.CanControl || len(open.Data) != 0 {
		t.Fatalf("unscoped or caller-granted OPEN: %v", open)
	}
	ready := readViewerScreenEvent(t, ctx, socket)
	if ready.Type != "ready" || ready.Attachment != "attachment" || ready.Instance != "instance" || ready.Controller || !ready.ReadOnly || !ready.CanControl {
		t.Fatalf("first viewer was silently promoted: %+v", ready)
	}
	kind, data, err := socket.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || !bytes.Equal(data, []byte("RFB 003.008\n")) {
		t.Fatalf("RFB bytes not bridged: %d %q %v", kind, data, err)
	}
	screen.incoming <- &epb.BrowserFrame{Kind: epb.BrowserFrame_CONTROL, ProfileId: "profile", InstanceId: "instance", AttachmentId: "attachment", ControllerId: "other-attachment", ControlRevision: "revision-2", ReadOnly: true}
	control := readViewerScreenEvent(t, ctx, socket)
	if control.Controller || control.ControllerID != "other-attachment" || control.Attachment != "attachment" || !control.ReadOnly {
		t.Fatalf("recipient scope confused with controller: %+v", control)
	}
	browser.controlMu.Lock()
	browser.control = func(_ context.Context, request *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
		if request.ServerId != "server" || request.ProfileId != "profile" || request.InstanceId != "instance" || request.AttachmentId != "attachment" || request.ExpectedControlRevision != "revision-2" || request.OwnerId != "" {
			return nil, errors.New("takeover scope changed")
		}
		screen.incoming <- &epb.BrowserFrame{Kind: epb.BrowserFrame_CONTROL, ProfileId: "profile", InstanceId: "instance", AttachmentId: "attachment", ControllerId: "attachment", ControlRevision: "revision-3"}
		return &epb.BrowserStatus{ProfileId: "profile", InstanceId: "instance", State: "running", ControllerId: "attachment", ControlRevision: "revision-3"}, nil
	}
	browser.controlMu.Unlock()
	taken := viewerTestRequest(t, client, http.MethodPost, w.origin+"/v/"+viewer.Id+"/api/control", w.origin, csrf, []byte(`{"revision":"revision-2"}`))
	taken.Body.Close()
	if taken.StatusCode != http.StatusOK {
		t.Fatalf("typed takeover: %d", taken.StatusCode)
	}
	control = readViewerScreenEvent(t, ctx, socket)
	if !control.Controller || control.ReadOnly || control.Revision != "revision-3" {
		t.Fatalf("explicit takeover not reflected: %+v", control)
	}
	// A queued event is intentionally labeled with the older revision. The
	// plugin must preserve it, not relabel it with the latest control update.
	revision, payload := []byte("revision-2"), []byte{4, 1, 0, 0, 0, 0, 0, 65}
	envelope := make([]byte, 2+len(revision)+len(payload))
	binary.BigEndian.PutUint16(envelope, uint16(len(revision)))
	copy(envelope[2:], revision)
	copy(envelope[2+len(revision):], payload)
	if err := socket.Write(ctx, websocket.MessageBinary, envelope); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-screen.sent:
		if frame.Kind != epb.BrowserFrame_DATA || frame.ServerId != "server" || frame.ProfileId != "profile" || frame.AttachmentId != "attachment" || frame.InstanceId != "instance" || frame.ControlRevision != "revision-2" || !bytes.Equal(frame.Data, payload) {
			t.Fatalf("RFB input scope/revision changed: %v", frame)
		}
	case <-ctx.Done():
		t.Fatal("input was not forwarded")
	}
	if screen.concurrent.Load() {
		t.Fatal("concurrent SDK stream senders")
	}
	socket.CloseNow()
	select {
	case <-screen.ctx.Done():
	case <-ctx.Done():
		t.Fatal("disconnect did not cancel remote attachment")
	}
	if w.lifetime.Err() != nil {
		t.Fatal("closing a viewer ended the client plugin")
	}
}

func TestViewerWebSocketAdmission(t *testing.T) {
	var opened atomic.Int32
	w := startViewerTestWorker(t, &viewerTestBrowser{stream: func(context.Context) (sdk.BrowserClientStream, error) {
		opened.Add(1)
		return nil, errors.New("must not reach remote")
	}})
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	target := strings.Replace(w.origin, "http:", "ws:", 1) + "/v/" + viewer.Id + "/rfb"
	for _, scenario := range []struct {
		name, origin string
		protocols    []string
	}{
		{"missing origin", "", []string{viewerSubprotocol, "csrf." + csrf}},
		{"foreign origin", "https://evil.invalid", []string{viewerSubprotocol, "csrf." + csrf}},
		{"missing csrf", w.origin, []string{viewerSubprotocol}},
		{"wrong csrf", w.origin, []string{viewerSubprotocol, "csrf.invalid"}},
		{"wrong protocol", w.origin, []string{"arbitrary-tunnel", "csrf." + csrf}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			header := make(http.Header)
			if scenario.origin != "" {
				header.Set("Origin", scenario.origin)
			}
			socket, response, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: client, HTTPHeader: header, Subprotocols: scenario.protocols})
			if socket != nil {
				socket.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
				t.Fatalf("WebSocket admission: response=%v error=%v", response, err)
			}
			if response.Body != nil {
				response.Body.Close()
			}
		})
	}
	if opened.Load() != 0 {
		t.Fatal("rejected WebSocket opened a remote attachment")
	}
}

// A viewer reconnects explicitly after closing its prior loopback socket.
// Verify rapid replacement does not keep a stale remote attachment alive.
func TestViewerExplicitReconnectReleasesPreviousAttachment(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	created := make(chan *viewerTestScreen, 32)
	w := startViewerTestWorker(t, &viewerTestBrowser{stream: func(ctx context.Context) (sdk.BrowserClientStream, error) {
		screen := newViewerTestScreen(ctx)
		created <- screen
		return screen, nil
	}})
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	target := strings.Replace(w.origin, "http:", "ws:", 1) + "/v/" + viewer.Id + "/rfb"
	for range 20 {
		socket, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": []string{w.origin}}, Subprotocols: []string{viewerSubprotocol, "csrf." + csrf}})
		if err != nil {
			t.Fatal(err)
		}
		<-created
		readViewerScreenEvent(t, ctx, socket)
		// Browser WebSocket.close completes only after the close handshake.
		if err := socket.Close(websocket.StatusNormalClosure, "Viewer detached"); err != nil {
			t.Fatal(err)
		}
		// The next Dial intentionally races handler cleanup after the close handshake.
	}
}
