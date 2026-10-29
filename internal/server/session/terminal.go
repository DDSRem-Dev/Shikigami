// Package session manages runtime resources separately from persistent session
// configuration. Terminal bridges do not own the underlying tmux session.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// Terminal is an ordinary tmux attachment backed by a cancellable PTY. Writes
// must be bounded; the tmux implementation enforces a five-second deadline.
type Terminal interface {
	io.ReadWriteCloser
	Resize(rows, cols uint32) error
}
type AttachFunc func(context.Context, string, uint32, uint32) (Terminal, error)

// attachedTerminal releases the startup context when its PTY is replaced or
// detached, rather than retaining one canceled-context child per redraw.
type attachedTerminal struct {
	Terminal
	cancel context.CancelFunc
}

func (t *attachedTerminal) Close() error { t.cancel(); return t.Terminal.Close() }

type TerminalOptions struct {
	QueueSize         int
	MaxAttachments    int
	MinRedrawInterval time.Duration
}

type Bridge struct {
	activity func() // valid user attach/input only; never output

	ctx    context.Context
	cancel context.CancelFunc
	// op serializes PTY replacement and lifecycle operations; mu protects the
	// output fanout and control state. Network Send never holds either lock.
	op                   sync.Mutex
	mu                   sync.Mutex
	id                   string
	attach               AttachFunc
	options              TerminalOptions
	pty                  Terminal
	readerDone           chan struct{}
	epoch                uint64
	rows, cols           uint32
	controller, revision string
	clients              map[string]*Subscription
	lastRedraw           time.Time
	closed               bool
}

type takeover struct{ revision string }

type Subscription struct {
	ID         string
	bridge     *Bridge
	ctx        context.Context
	cancel     context.CancelCauseFunc
	queue      chan *pb.TerminalOutput
	sequence   uint64 // bridge.mu
	writable   bool
	requests   map[string]takeover // bounded to this attachment's lifetime
	stopParent func() bool
}

func NewBridge(parent context.Context, id string, attach AttachFunc, opts TerminalOptions) *Bridge {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 256
	}
	if opts.MaxAttachments <= 0 {
		opts.MaxAttachments = 64
	}
	if opts.MinRedrawInterval <= 0 {
		opts.MinRedrawInterval = 100 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(parent)
	return &Bridge{ctx: ctx, cancel: cancel, id: id, attach: attach, options: opts,
		rows: 24, cols: 80, revision: rand.Text(), clients: map[string]*Subscription{}}
}

func rpcError(code connect.Code, message string) error { return connect.NewError(code, message) }

func (b *Bridge) Open(ctx context.Context, open *pb.TerminalOpen, mayWrite bool) (*Subscription, error) {
	if open == nil || open.SessionId != b.id || open.Size == nil || !validSize(open.Size.Rows, open.Size.Cols) {
		return nil, rpcError(connect.CodeInvalidArgument, "invalid terminal open or dimensions")
	}
	b.op.Lock()
	defer b.op.Unlock()
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil || ctx.Err() != nil {
		b.mu.Unlock()
		return nil, rpcError(connect.CodeCanceled, "terminal closed")
	}
	if len(b.clients) >= b.options.MaxAttachments || time.Since(b.lastRedraw) < b.options.MinRedrawInterval {
		b.mu.Unlock()
		return nil, rpcError(connect.CodeResourceExhausted, "terminal attachment limit; retry later")
	}
	old, oldDone := b.pty, b.readerDone
	b.pty = nil
	b.epoch++
	epoch := b.epoch
	b.mu.Unlock()
	if old != nil {
		_ = old.Close()
		<-oldDone
	}

	b.mu.Lock()
	sctx, cancel := context.WithCancelCause(b.ctx)
	s := &Subscription{ID: "att_" + rand.Text(), bridge: b, ctx: sctx, cancel: cancel,
		queue: make(chan *pb.TerminalOutput, b.options.QueueSize), writable: mayWrite && !open.ReadOnly, requests: map[string]takeover{}}
	if b.controller == "" && s.writable {
		b.controller, b.revision = s.ID, rand.Text()
		b.rows, b.cols = open.Size.Rows, open.Size.Cols
	}
	b.clients[s.ID] = s
	b.eventLocked(s, pb.SessionEvent_READY)
	// Existing observers also learn that a newly connected writer acquired an
	// empty control slot. All subscribers reset before the fresh attach output.
	for _, other := range b.clients {
		if other != s {
			b.eventLocked(other, pb.SessionEvent_CONTROL_CHANGED)
		}
		b.eventLocked(other, pb.SessionEvent_RESET)
	}
	rows, cols := b.rows, b.cols
	b.mu.Unlock()
	attachCtx, cancelAttach := context.WithCancel(b.ctx)
	stopOpening := context.AfterFunc(ctx, cancelAttach)
	terminal, err := b.attach(attachCtx, b.id, rows, cols)
	stopOpening()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		cancelAttach()
		if terminal != nil {
			_ = terminal.Close()
		}
		b.mu.Lock()
		for _, subscriber := range b.clients {
			subscriber.cancel(rpcError(connect.CodeUnavailable, "tmux attach failed"))
		}
		delete(b.clients, s.ID)
		if b.controller == s.ID {
			b.controller, b.revision = "", rand.Text()
		}
		b.mu.Unlock()
		return nil, err
	}
	terminal = &attachedTerminal{Terminal: terminal, cancel: cancelAttach}
	b.mu.Lock()
	b.pty, b.readerDone, b.lastRedraw = terminal, make(chan struct{}), time.Now()
	done := b.readerDone
	b.mu.Unlock()
	s.stopParent = context.AfterFunc(ctx, func() { s.cancel(ctx.Err()) })
	context.AfterFunc(sctx, func() { b.Detach(s.ID) })
	go b.read(terminal, epoch, done)
	if b.activity != nil {
		b.activity()
	}
	return s, nil
}

func validSize(rows, cols uint32) bool { return rows > 0 && cols > 0 && rows <= 1000 && cols <= 1000 }

func (b *Bridge) read(terminal Terminal, epoch uint64, done chan struct{}) {
	defer close(done)
	buf := make([]byte, 32<<10)
	for {
		n, err := terminal.Read(buf)
		b.mu.Lock()
		if epoch != b.epoch {
			b.mu.Unlock()
			return
		}
		if n > 0 {
			for _, subscriber := range b.clients {
				b.enqueueLocked(subscriber, &pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: append([]byte(nil), buf[:n]...)}})
			}
		}
		if err != nil {
			for _, subscriber := range b.clients {
				subscriber.cancel(rpcError(connect.CodeUnavailable, "terminal attachment ended; reconnect to redraw"))
			}
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}

func (b *Bridge) enqueueLocked(s *Subscription, output *pb.TerminalOutput) {
	if s.ctx.Err() != nil {
		return
	}
	s.sequence++
	output.SessionId, output.AttachmentId, output.Sequence, output.Timestamp = b.id, s.ID, s.sequence, time.Now().Unix()
	select {
	case s.queue <- output:
	default:
		s.cancel(rpcError(connect.CodeResourceExhausted, "slow terminal consumer; reconnect to redraw"))
	}
}

func (b *Bridge) eventLocked(s *Subscription, kind pb.SessionEvent_Type) {
	mode := "observer"
	if s.ID == b.controller {
		mode = "controller"
	}
	b.enqueueLocked(s, &pb.TerminalOutput{Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: kind,
		Data: map[string]string{"mode": mode, "controller_attachment_id": b.controller, "control_revision": b.revision}}}})
}

// Next is called by exactly one RPC sender. It never blocks the PTY reader.
func (s *Subscription) Next(ctx context.Context) (*pb.TerminalOutput, error) {
	if s.ctx.Err() != nil {
		return nil, context.Cause(s.ctx)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	case msg := <-s.queue:
		return msg, nil
	}
}

func (s *Subscription) Close() { s.bridge.Detach(s.ID) }

func (s *Subscription) Done() <-chan struct{} { return s.ctx.Done() }
func (s *Subscription) Err() error            { return context.Cause(s.ctx) }

func (b *Bridge) Input(id string, input *pb.TerminalInput) error {
	b.op.Lock()
	defer b.op.Unlock()
	b.mu.Lock()
	unlock := sync.OnceFunc(b.mu.Unlock)
	defer unlock()
	s := b.clients[id]
	if s == nil || s.ctx.Err() != nil {
		return rpcError(connect.CodeNotFound, "attachment not found")
	}
	if !s.writable || b.controller != id {
		return rpcError(connect.CodePermissionDenied, "only the controller may write or resize")
	}
	if b.pty == nil {
		return rpcError(connect.CodeUnavailable, "terminal is reconnecting")
	}
	if input == nil {
		return rpcError(connect.CodeInvalidArgument, "terminal input required")
	}
	var data []byte
	switch payload := input.Payload.(type) {
	case *pb.TerminalInput_Data:
		if len(payload.Data) > 256<<10 {
			return rpcError(connect.CodeResourceExhausted, "terminal input exceeds block limit")
		}
		data = payload.Data
	case *pb.TerminalInput_Resize:
		if payload.Resize == nil || !validSize(payload.Resize.Rows, payload.Resize.Cols) {
			return rpcError(connect.CodeInvalidArgument, "invalid dimensions")
		}
		terminal := b.pty
		unlock()
		if err := terminal.Resize(payload.Resize.Rows, payload.Resize.Cols); err != nil {
			return err
		}
		b.mu.Lock()
		b.rows, b.cols = payload.Resize.Rows, payload.Resize.Cols
		b.mu.Unlock()
		if b.activity != nil {
			b.activity()
		}
		return nil
	case *pb.TerminalInput_Control:
		if payload.Control == nil {
			return rpcError(connect.CodeInvalidArgument, "control required")
		}
		switch payload.Control.Type {
		case pb.ControlCommand_INTERRUPT:
			data = []byte{3}
		case pb.ControlCommand_QUIT:
			data = []byte{28}
		case pb.ControlCommand_SUSPEND:
			data = []byte{26}
		default:
			return rpcError(connect.CodeInvalidArgument, "unknown terminal control")
		}
	default:
		return rpcError(connect.CodeInvalidArgument, "open is only valid as the first frame")
	}
	terminal := b.pty
	// Keep op so control transfer cannot overtake this input, but release the
	// fanout mutex. Reading output must continue while PTY input is backpressured.
	unlock()
	n, err := terminal.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil && len(data) > 0 && b.activity != nil {
		b.activity()
	}
	return err
}

func (b *Bridge) TakeControl(id, expectedRevision, requestID string, mayControl bool) error {
	if !mayControl {
		return rpcError(connect.CodePermissionDenied, "control permission required")
	}
	if requestID == "" || len(requestID) > 128 {
		return rpcError(connect.CodeInvalidArgument, "request_id required, maximum 128 bytes")
	}
	b.op.Lock()
	defer b.op.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.clients[id]
	if s == nil || s.ctx.Err() != nil {
		return rpcError(connect.CodeNotFound, "attachment not found")
	}
	if !s.writable {
		return rpcError(connect.CodePermissionDenied, "read-only attachment cannot take control")
	}
	if result, exists := s.requests[requestID]; exists {
		if result.revision != expectedRevision {
			return rpcError(connect.CodeInvalidArgument, "request_id reused with different parameters")
		}
		return nil
	}
	if expectedRevision != b.revision {
		return rpcError(connect.CodeAborted, "control revision changed; refresh before retrying")
	}
	if len(s.requests) >= 1024 {
		return rpcError(connect.CodeResourceExhausted, "control request limit; reconnect")
	}
	b.controller, b.revision = id, rand.Text()
	s.requests[requestID] = takeover{revision: expectedRevision}
	for _, subscriber := range b.clients {
		b.eventLocked(subscriber, pb.SessionEvent_CONTROL_CHANGED)
	}
	return nil
}

func (b *Bridge) State() (count int, controller, revision string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients), b.controller, b.revision
}

func (b *Bridge) Detach(id string) {
	b.op.Lock()
	defer b.op.Unlock()
	b.mu.Lock()
	s := b.clients[id]
	if s == nil {
		b.mu.Unlock()
		return
	}
	delete(b.clients, id)
	if s.stopParent != nil {
		s.stopParent()
	}
	s.cancel(rpcError(connect.CodeCanceled, "attachment detached"))
	if b.controller == id {
		b.controller, b.revision = "", rand.Text()
		for _, other := range b.clients {
			b.eventLocked(other, pb.SessionEvent_CONTROL_CHANGED)
		}
	}
	var terminal Terminal
	var done chan struct{}
	if len(b.clients) == 0 {
		terminal, done, b.pty = b.pty, b.readerDone, nil
		b.epoch++
	}
	b.mu.Unlock()
	if terminal != nil {
		_ = terminal.Close()
		<-done
	}
}

func (b *Bridge) Close() error {
	// Wake an in-progress Attach before waiting for the lifecycle lock it owns.
	b.cancel()
	b.op.Lock()
	defer b.op.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	for _, s := range b.clients {
		if s.stopParent != nil {
			s.stopParent()
		}
		s.cancel(errors.New("terminal bridge stopped"))
	}
	b.clients = map[string]*Subscription{}
	b.controller, b.revision = "", rand.Text()
	terminal, done := b.pty, b.readerDone
	b.pty = nil
	b.epoch++
	b.mu.Unlock()
	if terminal != nil {
		err := terminal.Close()
		<-done
		return err
	}
	return nil
}

// PublishEvent shares the single sequenced, bounded output queue with terminal
// bytes. It never writes to the RPC stream from the process observer goroutine.
func (b *Bridge) PublishEvent(event *pb.SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, subscriber := range b.clients {
		b.enqueueLocked(subscriber, &pb.TerminalOutput{Payload: &pb.TerminalOutput_Event{Event: event}})
	}
}
