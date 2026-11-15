package client

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"github.com/coder/websocket"
)

const viewerSubprotocol = "shiki-browser-rfb-v1"
const maxScreenFrameBytes = 1 << 20

type attachment struct {
	ctx        context.Context
	cancel     context.CancelFunc
	id         string
	instance   string
	canControl bool
}

type screenEvent struct {
	Type         string `json:"type"`
	Attachment   string `json:"attachment,omitempty"`
	Instance     string `json:"instance,omitempty"`
	Revision     string `json:"revision,omitempty"`
	ReadOnly     bool   `json:"readOnly"`
	Controller   bool   `json:"controller"`
	ControllerID string `json:"controllerId,omitempty"`
	CanControl   bool   `json:"canControl"`
	Code         string `json:"code,omitempty"`
	Message      string `json:"message,omitempty"`
}

func validWebSocketCSRF(r *http.Request, v *viewer) bool {
	values := r.Header.Values("Sec-WebSocket-Protocol")
	if len(values) != 1 {
		return false
	}
	protocols := strings.Split(values[0], ",")
	if len(protocols) != 2 {
		return false
	}
	protocol, csrf := strings.TrimSpace(protocols[0]), strings.TrimSpace(protocols[1])
	return protocol == viewerSubprotocol && subtle.ConstantTimeCompare([]byte(csrf), []byte("csrf."+v.csrf)) == 1
}

func (w *Worker) handleRFB(out http.ResponseWriter, r *http.Request, v *viewer) {
	if singleHeader(r, "Origin") != w.origin || !validWebSocketCSRF(r, v) {
		http.Error(out, "Viewer WebSocket origin or CSRF mismatch", http.StatusForbidden)
		return
	}
	w.mu.Lock()
	if w.closed || v.ctx.Err() != nil {
		w.mu.Unlock()
		http.Error(out, "Viewer session closed", http.StatusGone)
		return
	}
	w.mu.Unlock()
	ctx, cancel := context.WithCancel(v.ctx)
	connection := &attachment{ctx: ctx, cancel: cancel}
	defer cancel()
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer stopRequest()
	v.mu.Lock()
	if v.connection != nil && v.connection.ctx.Err() == nil {
		v.mu.Unlock()
		writeHTTPError(out, connect.NewError(connect.CodeAlreadyExists, "VIEWER_ALREADY_ATTACHED"))
		return
	}
	v.connection = connection
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		if v.connection == connection {
			v.connection = nil
		}
		v.mu.Unlock()
	}()
	// A bounded handshake is distinct from the long-lived screen context.
	// Ending this RPC or the launching CLI must not cancel other viewers.
	handshake := time.AfterFunc(w.config.operationTimeout, cancel)
	defer handshake.Stop()
	stream, err := w.remote.BrowserStream(ctx)
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	defer stream.Close()
	// The only destination is this immutable viewer's explicit remote scope.
	// No owner/grant, address, port, VNC password, or CDP endpoint comes from JS.
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_OPEN, ServerId: v.serverID, ProfileId: v.profileID, ReadOnly: v.readOnly}); err != nil {
		writeHTTPError(out, err)
		return
	}
	ready, err := stream.Receive()
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	if ready == nil || ready.Kind != epb.BrowserFrame_READY || ready.AttachmentId == "" || ready.InstanceId == "" || len(ready.AttachmentId) > 128 || len(ready.InstanceId) > 128 || len(ready.ControllerId) > 128 || len(ready.ControlRevision) > 256 || ready.ProfileId != "" && ready.ProfileId != v.profileID || ready.ServerId != "" && ready.ServerId != v.serverID {
		writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "INVALID_BROWSER_READY"))
		return
	}
	handshake.Stop()
	if err := ctx.Err(); err != nil {
		writeHTTPError(out, err)
		return
	}
	v.mu.Lock()
	connection.id, connection.instance = ready.AttachmentId, ready.InstanceId
	connection.canControl = !v.readOnly && !ready.ReadOnly
	v.mu.Unlock()
	socket, err := websocket.Accept(out, r, &websocket.AcceptOptions{Subprotocols: []string{viewerSubprotocol}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer socket.CloseNow()
	if socket.Subprotocol() != viewerSubprotocol {
		socket.Close(websocket.StatusPolicyViolation, "Viewer subprotocol required")
		return
	}
	socket.SetReadLimit((256 << 10) + 258)
	controller := connection.canControl && ready.ControllerId == connection.id
	if err := writeScreenEvent(ctx, socket, screenEvent{Type: "ready", Attachment: ready.AttachmentId, Instance: ready.InstanceId, Revision: ready.ControlRevision, ReadOnly: !controller, Controller: controller, ControllerID: ready.ControllerId, CanControl: connection.canControl}); err != nil {
		return
	}
	results := make(chan error, 2)
	// Exactly one sender owns the SDK stream after OPEN. A slow Send stops
	// WebSocket reads; there is no unbounded queue, retry, or input replay.
	go func() { results <- sendScreenInput(ctx, socket, stream, v, connection) }()
	// Exactly one writer owns WebSocket output, including control/status events.
	go func() { results <- receiveScreenOutput(ctx, socket, stream, v, connection) }()
	<-results
	cancel()
	stream.Close()
	socket.CloseNow()
	<-results
}

func sendScreenInput(ctx context.Context, socket *websocket.Conn, stream sdk.BrowserClientStream, v *viewer, connection *attachment) error {
	for {
		kind, data, err := socket.Read(ctx)
		if err != nil {
			return err
		}
		if kind != websocket.MessageBinary || len(data) < 3 {
			return connect.NewError(connect.CodeInvalidArgument, "INVALID_RFB_INPUT_MESSAGE")
		}
		// Pin the revision when JS enqueues an input message, not when a
		// potentially backpressured message finally reaches this sender.
		revisionBytes := int(data[0])<<8 | int(data[1])
		if revisionBytes > 256 || len(data) <= 2+revisionBytes || len(data)-2-revisionBytes > 256<<10 || !utf8.Valid(data[2:2+revisionBytes]) {
			return connect.NewError(connect.CodeInvalidArgument, "INVALID_RFB_REVISION_ENVELOPE")
		}
		revision, payload := string(data[2:2+revisionBytes]), data[2+revisionBytes:]
		// Read-only protocol negotiation/update requests are valid RFB traffic.
		// The runtime parses the RFB stream and authoritatively gates all input,
		// clipboard and DesktopSize requests against its current controller.
		if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ServerId: v.serverID, ProfileId: v.profileID, InstanceId: connection.instance, AttachmentId: connection.id, ReadOnly: v.readOnly, ControlRevision: revision, Data: payload}); err != nil {
			return err
		}
	}
}

func receiveScreenOutput(ctx context.Context, socket *websocket.Conn, stream sdk.BrowserClientStream, v *viewer, connection *attachment) error {
	for {
		frame, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				writeScreenEvent(ctx, socket, screenEvent{Type: "closed", ReadOnly: true, Message: "The remote screen attachment ended. The independent browser may still be running."})
			} else if ctx.Err() == nil {
				_, failure := safeError(err)
				writeScreenEvent(ctx, socket, screenEvent{Type: "error", ReadOnly: true, Code: failure.Code, Message: failure.Message})
			}
			return err
		}
		if frame == nil || frame.ServerId != "" && frame.ServerId != v.serverID || frame.ProfileId != "" && frame.ProfileId != v.profileID || frame.InstanceId != "" && frame.InstanceId != connection.instance || frame.AttachmentId != "" && frame.AttachmentId != connection.id {
			return connect.NewError(connect.CodeDataLoss, "BROWSER_STREAM_SCOPE_CHANGED")
		}
		switch frame.Kind {
		case epb.BrowserFrame_DATA:
			if len(frame.Data) == 0 || len(frame.Data) > maxScreenFrameBytes || frame.AttachmentId != "" && frame.AttachmentId != connection.id {
				return connect.NewError(connect.CodeDataLoss, "INVALID_BROWSER_DATA")
			}
			writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := socket.Write(writeCtx, websocket.MessageBinary, frame.Data)
			cancel()
			if err != nil {
				return err
			}
		case epb.BrowserFrame_CONTROL:
			if len(frame.ControlRevision) > 256 || len(frame.ControllerId) > 128 {
				return connect.NewError(connect.CodeDataLoss, "INVALID_BROWSER_CONTROL")
			}
			controller := connection.canControl && frame.ControllerId == connection.id && !frame.ReadOnly
			if err := writeScreenEvent(ctx, socket, screenEvent{Type: "control", Attachment: connection.id, Instance: connection.instance, Revision: frame.ControlRevision, ReadOnly: !controller, Controller: controller, ControllerID: frame.ControllerId}); err != nil {
				return err
			}
		case epb.BrowserFrame_CLOSE:
			writeScreenEvent(ctx, socket, screenEvent{Type: "closed", ReadOnly: true, Message: "The remote screen attachment closed. Refresh status to learn whether the browser is still running."})
			return io.EOF
		default:
			return connect.NewError(connect.CodeDataLoss, "UNEXPECTED_BROWSER_STREAM_FRAME")
		}
	}
}

func writeScreenEvent(ctx context.Context, socket *websocket.Conn, event screenEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return socket.Write(writeCtx, websocket.MessageText, data)
}
