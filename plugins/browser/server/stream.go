package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

type attachment struct {
	id         string
	profile    string
	instance   string
	owner      string
	readOnly   bool
	canControl bool
	connection net.Conn
	cancel     context.CancelFunc
	out        chan *epb.BrowserFrame
	controlOut chan *epb.BrowserFrame
}

func (worker *Worker) detachAllLocked() {
	worker.control.Lock()
	defer worker.control.Unlock()
	for _, viewer := range worker.attachments {
		viewer.cancel()
		viewer.connection.Close()
	}
	worker.controller, worker.controlRevision = "", browserruntime.NewID()
}
func (worker *Worker) broadcastControlLocked() {
	for _, viewer := range worker.attachments {
		frame := &epb.BrowserFrame{Kind: epb.BrowserFrame_CONTROL, ProfileId: viewer.profile, InstanceId: viewer.instance, AttachmentId: viewer.id, ControlRevision: worker.controlRevision, ControllerId: worker.controller, CanControl: viewer.canControl && !viewer.readOnly, ReadOnly: worker.controller != viewer.id, Reason: "controller changed; no automatic promotion"}
		select {
		case viewer.controlOut <- frame:
		default:
			viewer.cancel()
			viewer.connection.Close()
		}
	}
}
func (worker *Worker) TakeBrowserControl(parent context.Context, request *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || request.OwnerId == "" || request.ExpectedControlRevision == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "authenticated owner and expected control revision are required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	current := worker.db.Runtime
	if current == nil || current.ProfileID != request.ProfileId || current.InstanceID != request.InstanceId || worker.db.State != "running" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "browser instance changed or is not running")
	}
	snapshot, err := worker.manager.Inspect(ctx, *current)
	if err != nil {
		return nil, err
	}
	if snapshot.State != "running" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "Chrome is not running")
	}
	worker.control.Lock()
	viewer := worker.attachments[request.AttachmentId]
	if viewer == nil || viewer.owner != request.OwnerId || viewer.profile != request.ProfileId || viewer.instance != request.InstanceId || viewer.readOnly || !viewer.canControl {
		worker.control.Unlock()
		return nil, connect.NewError(connect.CodePermissionDenied, "attachment cannot take browser control")
	}
	if worker.controlRevision != request.ExpectedControlRevision {
		worker.control.Unlock()
		return nil, connect.NewError(connect.CodeAborted, "browser control revision changed")
	}
	worker.controller, worker.controlRevision = viewer.id, browserruntime.NewID()
	worker.broadcastControlLocked()
	worker.control.Unlock()
	return worker.statusLocked(snapshot), nil
}

type received struct {
	frame *epb.BrowserFrame
	err   error
}
type frameReader struct {
	ctx                context.Context
	input              <-chan received
	viewer             *attachment
	buffer             []byte
	bufferRevision     string
	messageRevision    string
	messageHasRevision bool
}

func (reader *frameReader) Read(destination []byte) (int, error) {
	for len(reader.buffer) == 0 {
		select {
		case <-reader.ctx.Done():
			return 0, reader.ctx.Err()
		case received := <-reader.input:
			if received.err != nil {
				return 0, received.err
			}
			frame := received.frame
			if frame == nil || frame.Kind == epb.BrowserFrame_CLOSE {
				return 0, io.EOF
			}
			if frame.Kind != epb.BrowserFrame_DATA || len(frame.Data) == 0 || len(frame.Data) > browserruntime.MaxWireFrame || proto.Size(frame) > browserruntime.MaxWireFrame+4096 || (frame.ProfileId != "" && frame.ProfileId != reader.viewer.profile) || (frame.InstanceId != "" && frame.InstanceId != reader.viewer.instance) || (frame.AttachmentId != "" && frame.AttachmentId != reader.viewer.id) {
				return 0, connect.NewError(connect.CodeInvalidArgument, "invalid scoped browser data frame")
			}
			reader.buffer, reader.bufferRevision = frame.Data, frame.ControlRevision
		}
	}
	if !reader.messageHasRevision {
		reader.messageRevision, reader.messageHasRevision = reader.bufferRevision, true
	} else if reader.messageRevision != reader.bufferRevision {
		reader.messageRevision = "mixed-revisions"
	}
	count := copy(destination, reader.buffer)
	reader.buffer = reader.buffer[count:]
	return count, nil
}

func (worker *Worker) BrowserStream(parent context.Context, stream sdk.BrowserServerStream) error {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	input := make(chan received, 1)
	go func() {
		for {
			frame, err := stream.Receive()
			select {
			case input <- received{frame, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	firstTimer := time.NewTimer(10 * time.Second)
	defer firstTimer.Stop()
	var open *epb.BrowserFrame
	select {
	case first := <-input:
		if first.err != nil {
			return first.err
		}
		open = first.frame
	case <-ctx.Done():
		return ctx.Err()
	case <-firstTimer.C:
		return connect.NewError(connect.CodeDeadlineExceeded, "browser OPEN frame timeout")
	}
	if open == nil || proto.Size(open) > 4096 || open.Kind != epb.BrowserFrame_OPEN || !browserruntime.ValidID(open.ProfileId) || open.OwnerId == "" || len(open.Data) != 0 {
		return connect.NewError(connect.CodeInvalidArgument, "bounded authenticated scoped OPEN frame is required")
	}
	worker.mu.Lock()
	current := worker.db.Runtime
	if current == nil || current.ProfileID != open.ProfileId || worker.db.State != "running" || (open.InstanceId != "" && open.InstanceId != current.InstanceID) {
		worker.mu.Unlock()
		return connect.NewError(connect.CodeFailedPrecondition, "browser instance is not running or changed")
	}
	worker.control.Lock()
	count := len(worker.attachments)
	worker.control.Unlock()
	if count >= worker.config.MaxAttachments {
		worker.mu.Unlock()
		return connect.NewError(connect.CodeResourceExhausted, "browser attachment limit")
	}
	connection, err := worker.manager.Dial(ctx, *current)
	if err != nil {
		worker.mu.Unlock()
		return err
	}
	viewer := &attachment{id: browserruntime.NewID(), profile: current.ProfileID, instance: current.InstanceID, owner: open.OwnerId, readOnly: open.ReadOnly || !open.CanControl, canControl: open.CanControl, connection: connection, cancel: cancel, out: make(chan *epb.BrowserFrame, 16), controlOut: make(chan *epb.BrowserFrame, 8)}
	worker.control.Lock()
	worker.attachments[viewer.id] = viewer
	ready := &epb.BrowserFrame{Kind: epb.BrowserFrame_READY, ProfileId: viewer.profile, InstanceId: viewer.instance, AttachmentId: viewer.id, ReadOnly: viewer.readOnly, CanControl: viewer.canControl && !viewer.readOnly, ControlRevision: worker.controlRevision, ControllerId: worker.controller}
	worker.control.Unlock()
	worker.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	defer func() {
		connection.Close()
		worker.control.Lock()
		delete(worker.attachments, viewer.id)
		if worker.controller == viewer.id {
			worker.controller, worker.controlRevision = "", browserruntime.NewID()
			worker.broadcastControlLocked()
		}
		worker.control.Unlock()
	}()
	outputDone := make(chan error, 1)
	go func() {
		// READY must precede both RFB bytes and any concurrent takeover. After
		// it, control notifications can bypass queued image data, but never
		// reorder or discard the stateful RFB byte stream itself.
		if err := stream.Send(ready); err != nil {
			outputDone <- err
			return
		}
		for {
			frame, err := viewer.nextOutput(ctx)
			if err == nil {
				err = stream.Send(frame)
			}
			if err != nil {
				outputDone <- err
				return
			}
		}
	}()
	dataDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, browserruntime.MaxWireFrame)
		for {
			count, err := connection.Read(buffer)
			if count > 0 {
				data := append([]byte(nil), buffer[:count]...)
				frame := &epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ProfileId: viewer.profile, InstanceId: viewer.instance, AttachmentId: viewer.id, Data: data}
				select {
				case viewer.out <- frame:
				case <-ctx.Done():
					dataDone <- ctx.Err()
					return
				}
			}
			if err != nil {
				dataDone <- err
				return
			}
		}
	}()
	inputDone := make(chan error, 1)
	go func() {
		inputDone <- worker.forwardInput(ctx, viewer, &frameReader{ctx: ctx, input: input, viewer: viewer})
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-outputDone:
		return err
	case err := <-dataDone:
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case err := <-inputDone:
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func (worker *Worker) forwardInput(ctx context.Context, viewer *attachment, reader *frameReader) error {
	// Only the None-security RFB 3.8 handshake of the fixed private TigerVNC
	// endpoint is accepted. Shared ClientInit prevents disconnecting viewers.
	handshakeContext, handshakeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer handshakeCancel()
	reader.ctx = handshakeContext
	handshake := []struct {
		length   int
		expected []byte
	}{{12, []byte("RFB 003.008\n")}, {1, []byte{1}}, {1, []byte{1}}}
	for _, step := range handshake {
		data := make([]byte, step.length)
		if _, err := io.ReadFull(reader, data); err != nil {
			return err
		}
		if !bytes.Equal(data, step.expected) {
			return connect.NewError(connect.CodeInvalidArgument, "unsupported RFB handshake")
		}
		if err := writeMessage(viewer.connection, data); err != nil {
			return err
		}
	}
	reader.ctx = ctx
	handshakeCancel()
	for {
		reader.messageRevision, reader.messageHasRevision = "", false
		message, mutating, err := clientMessage(reader)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return err
			}
			return connect.NewError(connect.CodeInvalidArgument, "invalid bounded RFB client frame: "+err.Error())
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !mutating {
			// Negotiation and update requests affect this viewer's RFB socket,
			// not the shared desktop. A stalled spectator must not block input
			// or takeover for every other attachment.
			err = writeMessage(viewer.connection, message)
		} else {
			// Keep authorization and delivery linearized with takeover. In
			// particular, never release this lock before sending old-owner input.
			worker.control.Lock()
			allowed := ctx.Err() == nil && !viewer.readOnly && viewer.canControl && worker.controller == viewer.id && reader.messageRevision == worker.controlRevision
			if allowed {
				err = writeMessage(viewer.connection, message)
			}
			worker.control.Unlock()
		}
		if err != nil {
			return err
		}
	}
}

// nextOutput prioritizes bounded control metadata at send boundaries. It cannot
// preempt a transport Send already in progress; slow transports still cancel.
func (viewer *attachment) nextOutput(ctx context.Context) (*epb.BrowserFrame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case frame := <-viewer.controlOut:
		return frame, nil
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case frame := <-viewer.controlOut:
		return frame, nil
	case frame := <-viewer.out:
		return frame, nil
	}
}

func writeMessage(connection net.Conn, data []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	for len(data) > 0 {
		count, err := connection.Write(data)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return connection.SetWriteDeadline(time.Time{})
}
