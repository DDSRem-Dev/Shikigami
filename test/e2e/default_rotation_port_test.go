//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const rotationTail = "DEFAULT_ROTATION_EOF_TAIL"

type rotationTarget struct {
	listener     net.Listener
	mu           sync.Mutex
	active       map[net.Conn]bool
	expected     [32]byte
	accepted     uint64
	acknowledged uint64
	completed    uint64
	received     uint64
	digest       hash.Hash
	failure      error
	done         chan struct{}
	wg           sync.WaitGroup
}

func newRotationTarget(h *defaultRotationHarness) *rotationTarget {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	target := &rotationTarget{listener: listener, active: map[net.Conn]bool{}, digest: sha256.New(), done: make(chan struct{})}
	go func() {
		defer close(target.done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			target.mu.Lock()
			target.active[conn] = true
			target.accepted++
			valid := target.accepted == target.acknowledged+1
			expected := target.expected
			if !valid && target.failure == nil {
				target.failure = errors.New("unexpected/replayed target connection")
			}
			target.mu.Unlock()
			target.wg.Add(1)
			go func() {
				defer target.wg.Done()
				defer func() {
					conn.Close()
					target.mu.Lock()
					delete(target.active, conn)
					target.completed++
					target.mu.Unlock()
				}()
				if !valid {
					return
				}
				header := make([]byte, 32)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				if !bytes.Equal(header, expected[:]) {
					target.mu.Lock()
					target.failure = errors.New("unknown/replayed target header")
					target.mu.Unlock()
					return
				}
				buffer := make([]byte, 32<<10)
				data := header
				for {
					if len(data) > 0 {
						target.mu.Lock()
						target.received += uint64(len(data))
						target.digest.Write(data)
						target.mu.Unlock()
						if _, err := conn.Write(data); err != nil {
							return
						}
					}
					n, err := conn.Read(buffer)
					data = buffer[:n]
					if err != nil {
						if n > 0 {
							target.mu.Lock()
							target.received += uint64(n)
							target.digest.Write(data)
							target.mu.Unlock()
							_, _ = conn.Write(data)
						}
						if errors.Is(err, io.EOF) {
							_, _ = io.WriteString(conn, rotationTail)
						}
						return
					}
				}
			}()
		}
	}()
	h.t.Cleanup(func() {
		listener.Close()
		<-target.done
		target.mu.Lock()
		for conn := range target.active {
			conn.Close()
		}
		target.mu.Unlock()
		target.wg.Wait()
	})
	return target
}

type rotationPortWork struct {
	target          *rotationTarget
	active, stopped *ppb.ForwardSpec
	forward         *ppb.ForwardState
	stopRequest     string
	held            *net.TCPConn
	heldBytes       uint64
	heldDigest      hash.Hash
	heldAccepted    uint64
	observer        *rotationTCPObserver
	sequence        int
}

func (h *defaultRotationHarness) configureRotationPort(target *rotationTarget) {
	for _, side := range []string{"client", "server"} {
		values := map[string]any{"idle_timeout_seconds": 120.0}
		if side == "server" {
			values["allow_ports"] = []any{float64(target.listener.Addr().(*net.TCPAddr).Port)}
		}
		s, err := structpb.NewStruct(values)
		if err != nil {
			h.t.Fatal(err)
		}
		h.manage(side, "port", pb.ManagePluginRequest_SET_CONFIG, s)
	}
}
func (h *defaultRotationHarness) prepareRotationPort(target *rotationTarget) *rotationPortWork {
	create := func() *ppb.ForwardState {
		state, err := h.local.CreateForward(h.ctx, &apb.CreateForwardRequest{RequestId: h.id(), Spec: &ppb.ForwardSpec{ServerId: h.serverID, RemoteAddress: "127.0.0.1", RemotePort: uint32(target.listener.Addr().(*net.TCPAddr).Port), LocalAddress: "127.0.0.1", Enabled: true}})
		if err != nil || state.GetState() != "listening" || state.GetBoundPort() == 0 {
			h.t.Fatal("forward not listening", state, err)
		}
		return state
	}
	stopped := create()
	request := h.id()
	if _, err := h.local.StopForward(h.ctx, &apb.StopForwardRequest{ForwardId: stopped.Spec.Id, ExpectedRevision: stopped.Spec.Revision, RequestId: request}); err != nil {
		h.t.Fatal(err)
	}
	active := create()
	return &rotationPortWork{target: target, active: proto.Clone(active.Spec).(*ppb.ForwardSpec), stopped: proto.Clone(stopped.Spec).(*ppb.ForwardSpec), forward: active, stopRequest: request}
}
func (h *defaultRotationHarness) rotationPortSettled(p *rotationPortWork) {
	h.await("forward intent/listener recovery", func(ctx context.Context) error {
		list, err := h.local.ListForwards(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if len(list.Forwards) != 1 {
			return errors.New("enabled intent missing or explicitly stopped intent resurrected")
		}
		s := list.Forwards[0]
		if !proto.Equal(s.Spec, p.active) {
			h.t.Fatal("forward durable intent changed")
		}
		if s.State != "listening" || s.BoundPort == 0 || s.Connections != 0 {
			return errors.New("forward not settled")
		}
		p.target.mu.Lock()
		defer p.target.mu.Unlock()
		if p.target.failure != nil {
			h.t.Fatal(p.target.failure)
		}
		if len(p.target.active) != 0 {
			return errors.New("target still active")
		}
		p.forward = s
		return nil
	})
}
func (h *defaultRotationHarness) rotationDial(p *rotationPortWork) (*net.TCPConn, []byte) {
	h.rotationPortSettled(p)
	p.sequence++
	header := sha256.Sum256([]byte(fmt.Sprintf("rotation-application-connection-%d", p.sequence)))
	p.target.mu.Lock()
	p.target.expected = header
	p.target.acknowledged = p.target.accepted
	p.target.received = 0
	p.target.digest.Reset()
	p.target.mu.Unlock()
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", p.forward.BoundPort), 3*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	return conn.(*net.TCPConn), header[:]
}
func (h *defaultRotationHarness) rotationExchange(p *rotationPortWork) {
	h.t.Helper()
	conn, header := h.rotationDial(p)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	payload := append(header, bytes.Repeat([]byte{byte(p.sequence), 0, 0xff, 0x55}, (1<<20)/4)...)
	done := make(chan error, 1)
	go func() {
		_, err := conn.Write(payload)
		if err == nil {
			err = conn.CloseWrite()
		}
		done <- err
	}()
	got, readErr := io.ReadAll(io.LimitReader(conn, int64(len(payload)+len(rotationTail)+1)))
	conn.Close()
	writeErr := <-done
	if readErr != nil || writeErr != nil || len(got) != len(payload)+len(rotationTail) || !bytes.Equal(got[len(payload):], []byte(rotationTail)) || sha256.Sum256(got[:len(payload)]) != sha256.Sum256(payload) {
		h.t.Fatal("fresh TCP length/hash/half-close mismatch", readErr, writeErr)
	}
	h.rotationPortSettled(p)
	p.target.mu.Lock()
	defer p.target.mu.Unlock()
	if p.target.accepted != p.target.acknowledged+1 || p.target.received != uint64(len(payload)) || !bytes.Equal(p.target.digest.Sum(nil), sha256Digest(payload)) {
		h.t.Fatal("target input changed/replayed")
	}
}
func sha256Digest(data []byte) []byte { sum := sha256.Sum256(data); return sum[:] }
func (h *defaultRotationHarness) holdRotationConnection(p *rotationPortWork) {
	var payload []byte
	p.held, payload = h.rotationDial(p)
	p.heldDigest = sha256.New()
	p.heldBytes = 0
	h.t.Cleanup(func() { p.held.Close() })
	h.rotationHeartbeat(p, payload)
	p.target.mu.Lock()
	p.heldAccepted = p.target.accepted
	p.target.mu.Unlock()
	h.event("held_tcp_open", map[string]any{"accepted": p.heldAccepted, "bytes": p.heldBytes})
}
func (h *defaultRotationHarness) rotationHeartbeat(p *rotationPortWork, payload []byte) {
	h.t.Helper()
	p.held.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := p.held.Write(payload); err != nil {
		h.t.Fatal("held pre-expiry TCP write", err)
	}
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(p.held, echo); err != nil || !bytes.Equal(echo, payload) {
		h.t.Fatal("held TCP changed before expiry", err)
	}
	p.heldBytes += uint64(len(payload))
	p.heldDigest.Write(payload)
}
func (h *defaultRotationHarness) verifyRotatedConnection(p *rotationPortWork) {
	if p.observer == nil {
		h.t.Fatal("held connection was not independently observed before expiry")
	}
	result := p.observer.wait(h.ctx)
	if err := rotationTCPTerminationAllowed(result, p.observer.earliest, p.observer.latest); err != nil {
		h.t.Fatal(err)
	}
	p.held.Close()
	h.event("held_tcp_retirement_window", result)
	h.rotationPortSettled(p)
	p.target.mu.Lock()
	if p.target.accepted != p.heldAccepted || p.target.received != p.heldBytes || !bytes.Equal(p.target.digest.Sum(nil), p.heldDigest.Sum(nil)) {
		p.target.mu.Unlock()
		h.t.Fatal("held request replayed before an application reconnect")
	}
	p.target.mu.Unlock()
	h.event("held_tcp_terminated", map[string]any{"accepted": p.heldAccepted, "bytes": p.heldBytes, "no_replay": true})
	h.rotationExchange(p) // A new application dial, never an automatic payload retry.
}
func (h *defaultRotationHarness) durableRotation(w *defaultRotationSyncWork, p *rotationPortWork) {
	data, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "resources", "state.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	var state storage.Snapshot
	if err := json.Unmarshal(data, &state); err != nil {
		h.t.Fatal(err)
	}
	for _, want := range []*ppb.Mapping{w.active, w.paused} {
		got := new(ppb.Mapping)
		if err := state.Get("mappings", want.Id, got); err != nil || !proto.Equal(want, got) {
			h.t.Fatal("mapping intent changed", err)
		}
	}
	active := new(ppb.ForwardSpec)
	if err := state.Get("forwards", p.active.Id, active); err != nil || !proto.Equal(p.active, active) {
		h.t.Fatal("forward intent changed", err)
	}
	if _, exists := state.Buckets["forwards"][p.stopped.Id]; exists {
		h.t.Fatal("stopped forward resurrected")
	}
	receipt, exists := state.Requests["StopForward\x00"+p.stopRequest]
	if !exists || receipt.Status != "succeeded" || receipt.ResourceID != p.stopped.Id {
		h.t.Fatal("stop request receipt lost")
	}
}
