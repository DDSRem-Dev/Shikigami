package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/encoding/protojson"
)

type terminal struct {
	mu                               sync.Mutex
	send                             sync.Mutex
	stream                           rpc.ShikiDaemonTerminalStreamClientStream
	cancel                           context.CancelFunc
	done                             chan struct{}
	attachment, mode, revision, tail string
	events                           map[string]bool
	bytes                            int64
	hash                             hash.Hash
	err                              error
	once                             sync.Once
}

func (t *terminal) close() error {
	t.once.Do(func() { t.cancel(); _ = t.stream.Close() })
	select {
	case <-t.done:
		return nil
	case <-time.After(5 * time.Second):
		return context.DeadlineExceeded
	}
}
func (t *terminal) receive() {
	defer close(t.done)
	for {
		frame, err := t.stream.Receive()
		t.mu.Lock()
		if err != nil {
			t.err = err
			t.mu.Unlock()
			return
		}
		data := frame.GetData()
		t.bytes += int64(len(data))
		_, _ = t.hash.Write(data)
		t.tail += string(data)
		if len(t.tail) > 256<<10 {
			t.tail = t.tail[len(t.tail)-(256<<10):]
		}
		if ev := frame.GetEvent(); ev != nil {
			t.events[ev.Type.String()] = true
			if mode := ev.Data["mode"]; mode != "" {
				t.mode = mode
			}
			if rev := ev.Data["control_revision"]; rev != "" {
				t.revision = rev
			}
		}
		t.mu.Unlock()
	}
}
func (h *helper) terminalOp(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Handle     string `json:"handle"`
		Frontend   string `json:"frontend"`
		ServerID   string `json:"server_id"`
		SessionID  string `json:"session_id"`
		ReadOnly   bool   `json:"read_only"`
		DeviceName string `json:"device_name"`
		Text       string `json:"text"`
		Match      string `json:"match"`
	}](r)
	if err != nil || a.Handle == "" {
		return nil, invalid()
	}
	if r.Op == "terminal.open" {
		f, err := h.front(ctx, a.Frontend)
		if err != nil {
			return nil, err
		}
		call, cancel := context.WithCancel(h.ctx)
		timer := time.AfterFunc(5*time.Second, cancel)
		stream, err := f.Remote(a.ServerID).TerminalStream(call)
		if err != nil {
			timer.Stop()
			cancel()
			return nil, err
		}
		fail := func(err error) (any, error) { timer.Stop(); cancel(); _ = stream.Close(); return nil, err }
		if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: a.SessionID, ReadOnly: a.ReadOnly, DeviceName: a.DeviceName, Size: &pb.ResizeEvent{Rows: 30, Cols: 120}}}}); err != nil {
			return fail(err)
		}
		ready, err := stream.Receive()
		if err != nil {
			return fail(err)
		}
		if ready.GetEvent().GetType() != pb.SessionEvent_READY || ready.AttachmentId == "" {
			return fail(invalid())
		}
		timer.Stop()
		t := &terminal{stream: stream, cancel: cancel, done: make(chan struct{}), attachment: ready.AttachmentId, mode: ready.GetEvent().Data["mode"], revision: ready.GetEvent().Data["control_revision"], events: map[string]bool{"READY": true}, hash: sha256.New()}
		h.mu.Lock()
		if h.closed || h.terminals[a.Handle] != nil || len(h.terminals) >= 16 {
			h.mu.Unlock()
			return fail(invalid())
		}
		h.terminals[a.Handle] = t
		h.mu.Unlock()
		result := map[string]string{"handle": a.Handle, "attachment_id": t.attachment, "mode": t.mode, "control_revision": t.revision}
		go t.receive()
		return result, nil
	}
	h.mu.Lock()
	t := h.terminals[a.Handle]
	h.mu.Unlock()
	if t == nil {
		return nil, invalid()
	}
	switch r.Op {
	case "terminal.send":
		if len(a.Text) > 4096 {
			return nil, invalid()
		}
		t.send.Lock()
		defer t.send.Unlock()
		stop := context.AfterFunc(ctx, t.cancel)
		defer stop()
		return map[string]bool{"sent": true}, t.stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte(a.Text)}})
	case "terminal.close":
		return map[string]bool{"closed": true}, t.close()
	case "terminal.state":
		t.mu.Lock()
		defer t.mu.Unlock()
		events := []string{}
		for e := range t.events {
			events = append(events, e)
		}
		sort.Strings(events)
		count := 0
		if a.Match != "" {
			count = strings.Count(t.tail, a.Match)
		}
		return map[string]any{"attachment_id": t.attachment, "mode": t.mode, "control_revision": t.revision, "events": events, "matched": count > 0, "match_count": count, "closed": t.err != nil, "error_code": errorCode(t.err), "bytes": t.bytes, "sha256": hex.EncodeToString(t.hash.Sum(nil)), "retained_tail_bytes": len(t.tail)}, nil
	}
	return nil, invalid()
}
func (h *helper) fileStream(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Service          string `json:"service"`
		ExpectedDaemonID string `json:"expected_daemon_id"`
		Frontend         string `json:"frontend"`
		ServerID         string `json:"server_id"`
		ProjectID        string `json:"project_id"`
		Path             string `json:"path"`
		BaseRevision     string `json:"base_revision"`
		Content          string `json:"content"`
		MappingID        string `json:"mapping_id"`
		ConflictID       string `json:"conflict_id"`
		Generation       string `json:"generation"`
		Side             string `json:"side"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	if r.Op == "upload" {
		if a.Service != "file-direct" || len(a.Content) > 64<<10 {
			return nil, invalid()
		}
		remote, close, err := h.directFiles(ctx, a.Frontend, a.ServerID, a.ExpectedDaemonID)
		if err != nil {
			return nil, err
		}
		defer close()
		call, cancel := context.WithCancel(ctx)
		defer cancel()
		stream, err := remote.WriteFile(call)
		if err != nil {
			return nil, err
		}
		data := []byte(a.Content)
		err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: a.ProjectID, Path: a.Path, BaseRevision: a.BaseRevision, ContentHash: digest(data), Size: int64(len(data)), Mode: 0600}}})
		if err != nil {
			return nil, err
		}
		if len(data) > 0 {
			if err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: data}}); err != nil {
				return nil, err
			}
		}
		info, err := stream.CloseAndReceive()
		if err != nil {
			return nil, err
		}
		b, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(info)
		return json.RawMessage(b), err
	}
	f, err := h.front(ctx, a.Frontend)
	if err != nil {
		return nil, err
	}
	sides := map[string]ppb.ReadConflictFileRequest_Side{"local": ppb.ReadConflictFileRequest_LOCAL, "remote": ppb.ReadConflictFileRequest_REMOTE}
	side, ok := sides[a.Side]
	if !ok {
		return nil, invalid()
	}
	stream, err := f.Agent().ReadConflictFile(ctx, &ppb.ReadConflictFileRequest{MappingId: a.MappingID, ConflictId: a.ConflictID, ExpectedGeneration: a.Generation, Side: side})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	sum := sha256.New()
	var size int64
	for {
		frame, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		size += int64(len(frame.GetChunk()))
		if size > 32<<20 {
			return nil, invalid()
		}
		_, _ = sum.Write(frame.GetChunk())
	}
	return map[string]any{"size": size, "sha256": hex.EncodeToString(sum.Sum(nil))}, nil
}
