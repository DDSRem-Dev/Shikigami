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
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/types/known/structpb"
)

type windowFiles struct {
	f       *dataFixture
	local   string
	mapping *ppb.Mapping
	scope   *ppb.Scope
	updates int
	current [32]byte
}

func startWindowFiles(t *testing.T, f *dataFixture) *windowFiles {
	t.Helper()
	f.manage(t, "sync", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_ENABLE, nil)
	f.manage(t, "sync", apb.PluginTarget_LOCAL, pb.ManagePluginRequest_ENABLE, nil)
	local := filepath.Join(f.root, "local-files")
	if err := os.Mkdir(local, 0700); err != nil {
		t.Fatal(err)
	}
	initial := bytes.Repeat([]byte("baseline"), 32<<10)
	for _, root := range []string{local, f.project.Path} {
		if err := os.WriteFile(filepath.Join(root, "fixed.bin"), initial, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mapping, err := f.client.Agent().PutMapping(f.ctx, &apb.PutMappingRequest{RequestId: "windows-mapping", Mapping: &ppb.Mapping{ServerId: f.serverID, ProjectId: f.project.Id, LocalRoot: local, SyncEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	w := &windowFiles{f: f, local: local, mapping: mapping, scope: &ppb.Scope{ServerId: f.serverID, ProjectId: f.project.Id, MappingId: mapping.Id}}
	w.settle(t, local, sha256.Sum256(initial))
	w.current = sha256.Sum256(initial)
	return w
}

func (w *windowFiles) settle(t *testing.T, destination string, want [32]byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.f.ctx, 10*time.Second)
	defer cancel()
	if err := await(ctx, func() error {
		data, err := os.ReadFile(filepath.Join(destination, "fixed.bin"))
		if err != nil {
			return err
		}
		if len(data) != 256<<10 || sha256.Sum256(data) != want {
			return errors.New("fixed file length/hash mismatch")
		}
		state, err := w.f.client.Agent().GetSyncState(ctx, w.scope)
		if err != nil {
			return err
		}
		if state.State != "active" || state.PendingFiles != 0 || state.ConflictCount != 0 {
			return fmt.Errorf("sync %s pending=%d conflicts=%d", state.State, state.PendingFiles, state.ConflictCount)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (w *windowFiles) update(t *testing.T) {
	t.Helper()
	sequence := w.updates
	payload := bytes.Repeat([]byte{byte(sequence), 0xff, 0, byte(sequence + 1)}, 64<<10)
	source, destination := w.local, w.f.project.Path
	if sequence%2 != 0 {
		source, destination = destination, source
	}
	if err := os.WriteFile(filepath.Join(source, "fixed.bin"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	w.settle(t, destination, sha256.Sum256(payload))
	w.current = sha256.Sum256(payload)
	w.updates++
}

func (w *windowFiles) checkCurrent(t *testing.T) {
	t.Helper()
	// Read both local fixture files without issuing remote file RPCs. The
	// exact last verified payload must remain unchanged in idle-control.
	for _, root := range []string{w.local, w.f.project.Path} {
		data, err := os.ReadFile(filepath.Join(root, "fixed.bin"))
		if err != nil || len(data) != 256<<10 || sha256.Sum256(data) != w.current {
			t.Fatal("file changed outside fixed workload", root, err)
		}
	}
	state, err := w.f.client.Agent().GetSyncState(w.f.ctx, w.scope)
	if err != nil || state.GetState() != "active" || state.GetPendingFiles() != 0 || state.GetConflictCount() != 0 {
		t.Fatal("mapping not quiescent", state, err)
	}
}

func (w *windowFiles) close(t *testing.T) {
	t.Helper()
	w.mapping.SyncEnabled = false
	updated, err := w.f.client.Agent().PutMapping(w.f.ctx, &apb.PutMappingRequest{RequestId: "windows-pause", ExpectedRevision: w.mapping.Revision, Mapping: w.mapping})
	if err != nil {
		t.Fatal(err)
	}
	w.mapping = updated
	ctx, cancel := context.WithTimeout(w.f.ctx, 10*time.Second)
	defer cancel()
	if err := await(ctx, func() error {
		state, err := w.f.client.Agent().GetSyncState(ctx, w.scope)
		if err != nil {
			return err
		}
		if state.State != "paused" {
			return errors.New("mapping not paused")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.f.client.Agent().RemoveMapping(ctx, &apb.RemoveMappingRequest{MappingId: w.mapping.Id, ExpectedRevision: w.mapping.Revision, RequestId: "windows-remove"}); err != nil {
		t.Fatal(err)
	}
}

type windowTCP struct {
	f         *dataFixture
	listener  net.Listener
	forward   *ppb.ForwardState
	accepted  atomic.Int64
	results   chan error
	done      chan struct{}
	completed int
	maximum   int
}

func startWindowTCP(t *testing.T, f *dataFixture, maximum int) *windowTCP {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := &windowTCP{f: f, listener: listener, results: make(chan error, 2), done: make(chan struct{}), maximum: maximum}
	go func() {
		defer close(w.done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			sequence := int(w.accepted.Add(1) - 1)
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			data, err := io.ReadAll(io.LimitReader(conn, tcpBytes+1))
			if err == nil && (sequence >= maximum || len(data) != tcpBytes || sha256.Sum256(data) != sha256.Sum256(tcpPayload(sequence))) {
				err = errors.New("target payload/sequence mismatch or replay")
			}
			if err == nil {
				_, err = io.Copy(conn, bytes.NewReader(append(data, []byte(responseTail)...)))
			}
			conn.Close()
			select {
			case w.results <- err:
			case <-f.ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { listener.Close(); <-w.done })
	port := listener.Addr().(*net.TCPAddr).Port
	values, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(port)}, "idle_timeout_seconds": 30.0})
	if err != nil {
		t.Fatal(err)
	}
	f.manage(t, "port", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_SET_CONFIG, values)
	f.manage(t, "port", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_ENABLE, nil)
	f.manage(t, "port", apb.PluginTarget_LOCAL, pb.ManagePluginRequest_ENABLE, nil)
	w.forward, err = f.client.Agent().CreateForward(f.ctx, &apb.CreateForwardRequest{RequestId: "windows-forward", Spec: &ppb.ForwardSpec{ServerId: f.serverID, RemoteAddress: "127.0.0.1", RemotePort: uint32(port), LocalAddress: "127.0.0.1"}})
	if err != nil || w.forward.GetState() != "listening" || w.forward.GetBoundPort() == 0 {
		t.Fatal("forward", w.forward, err)
	}
	w.settle(t)
	return w
}

func (w *windowTCP) settle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.f.ctx, 10*time.Second)
	defer cancel()
	if err := await(ctx, func() error {
		list, err := w.f.client.Agent().ListForwards(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		for _, state := range list.Forwards {
			if state.GetSpec().GetId() == w.forward.Spec.Id && state.State == "listening" && state.BoundPort == w.forward.BoundPort && state.Connections == 0 {
				return nil
			}
		}
		return errors.New("forward connections not reclaimed")
	}); err != nil {
		t.Fatal(err)
	}
}

func (w *windowTCP) exchange(t *testing.T) {
	t.Helper()
	if err := tcpExchange(w.f.ctx, w.forward.BoundPort, tcpPayload(w.completed)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-w.results:
		if err != nil {
			t.Fatal(err)
		}
	case <-w.f.ctx.Done():
		t.Fatal(w.f.ctx.Err())
	}
	w.completed++
	w.settle(t)
	if got := w.accepted.Load(); got != int64(w.completed) {
		t.Fatal("extra/replayed target connection", got)
	}
}

func (w *windowTCP) close(t *testing.T) {
	t.Helper()
	if _, err := w.f.client.Agent().StopForward(w.f.ctx, &apb.StopForwardRequest{ForwardId: w.forward.Spec.Id, ExpectedRevision: w.forward.Spec.Revision, RequestId: "windows-stop-forward"}); err != nil {
		t.Fatal(err)
	}
	w.listener.Close()
	<-w.done
	if w.accepted.Load() != int64(w.maximum) || w.completed != w.maximum {
		t.Fatal("incorrect fixed TCP work", w.accepted.Load(), w.completed)
	}
	select {
	case err := <-w.results:
		t.Fatal("extra target result", err)
	default:
	}
}
