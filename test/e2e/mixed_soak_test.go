//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/types/known/structpb"
)

var applicationTCPClients = flag.Int("shiki-soak-tcp-clients", 0, "opt-in simultaneous TCP streams during each terminal/sync cycle (0 disabled, 1..16)")

func mixedSoakClients(t *testing.T) int {
	t.Helper()
	if *applicationTCPClients < 0 || *applicationTCPClients > 16 || (*applicationTCPClients > 0 && applicationSoakDuration(t) == 0) {
		t.Fatal("shiki-soak-tcp-clients must be 0 or 1..16 with a nonzero soak duration")
	}
	return *applicationTCPClients
}

const mixedPayloadSize = 1 << 20
const mixedHeaderSize = 64
const mixedResponseTail = "SHIKI_MIXED_AFTER_HALF_CLOSE\n"

type mixedExpectation struct {
	hash  [32]byte
	seen  bool
	ready chan struct{}
	done  chan error
}

type mixedWorkload struct {
	client       *frontend.Client
	forward      *ppb.ForwardState
	listener     net.Listener
	acceptDone   chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	connections  map[net.Conn]struct{}
	expected     map[string]*mixedExpectation
	accepted     uint64
	errors       chan error
	clients      int
	latencies    soakLatencies
	payloadBytes uint64
	rounds       uint64
}

// The target is only an application fixture. All authorization, listeners,
// forwarding, HTTP2 and lifecycle behavior belongs to the four real workers
// and the same shipped agent/daemon used by the terminal and file workload.
func startMixedWorkload(t *testing.T, ctx context.Context, client *frontend.Client, serverID string) *mixedWorkload {
	t.Helper()
	count := mixedSoakClients(t)
	if count == 0 {
		return nil
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mixedWorkload{client: client, listener: listener, clients: count, acceptDone: make(chan struct{}), connections: map[net.Conn]struct{}{}, errors: make(chan error, 1)}
	go m.accept()
	// Register first so a later setup failure still reaps the target sockets.
	t.Cleanup(m.closeTarget)
	targetPort := listener.Addr().(*net.TCPAddr).Port
	remote := &apb.PluginTarget{Side: apb.PluginTarget_REMOTE, ServerId: serverID}
	local := &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
	values, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(targetPort)}, "idle_timeout_seconds": 30.0})
	if err != nil {
		t.Fatal(err)
	}
	m.manage(t, ctx, remote, pb.ManagePluginRequest_SET_CONFIG, values)
	m.manage(t, ctx, remote, pb.ManagePluginRequest_ENABLE, nil)
	m.manage(t, ctx, local, pb.ManagePluginRequest_ENABLE, nil)
	m.forward, err = client.Agent().CreateForward(ctx, &apb.CreateForwardRequest{RequestId: rand.Text(), Spec: &ppb.ForwardSpec{ServerId: serverID, RemoteAddress: "127.0.0.1", RemotePort: uint32(targetPort), LocalAddress: "127.0.0.1"}})
	if err != nil || m.forward.GetState() != "listening" || m.forward.GetBoundPort() == 0 {
		t.Fatal("mixed workload forward not listening", m.forward, err)
	}
	m.settled(t, ctx)
	return m
}

func (m *mixedWorkload) manage(t *testing.T, ctx context.Context, target *apb.PluginTarget, action pb.ManagePluginRequest_Action, values *structpb.Struct) {
	t.Helper()
	list, err := m.client.Agent().ListPlugins(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, status := range list.Plugins {
		if status.PluginId == "port" {
			revision = status.Revision
		}
	}
	if revision == "" {
		t.Fatal("port plugin has no managed revision")
	}
	accepted, err := m.client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: "port", Action: action, Config: values, ExpectedRevision: revision, RequestId: rand.Text()}})
	if err != nil {
		t.Fatal(err)
	}
	await(t, ctx, func(call context.Context) error {
		list, err := m.client.Agent().ListPlugins(call, target)
		if err != nil {
			return err
		}
		for _, status := range list.Plugins {
			if status.PluginId == "port" && status.Revision == accepted.Revision {
				if status.State == "failed" || status.State == "incompatible" {
					t.Fatal("mixed workload port failed", status)
				}
				if status.State == "ready" || (action == pb.ManagePluginRequest_SET_CONFIG && !status.Enabled && status.State == "stopped") {
					return nil
				}
			}
		}
		return errors.New("waiting for real port worker state")
	})
}

func (m *mixedWorkload) fail(err error) {
	select {
	case m.errors <- err:
	default:
	}
}

func (m *mixedWorkload) accept() {
	defer close(m.acceptDone)
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.connections[conn] = struct{}{}
		m.accepted++
		m.mu.Unlock()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				conn.Close()
				m.mu.Lock()
				delete(m.connections, conn)
				m.mu.Unlock()
			}()
			_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
			header := make([]byte, mixedHeaderSize)
			if _, err := io.ReadFull(conn, header); err != nil {
				m.fail(fmt.Errorf("target header: %w", err))
				return
			}
			m.mu.Lock()
			expected := m.expected[string(header)]
			valid := expected != nil && !expected.seen
			if valid {
				expected.seen = true
			}
			m.mu.Unlock()
			if !valid {
				m.fail(errors.New("target received an unknown or replayed connection"))
				return
			}
			close(expected.ready) // Real remote bytes, not local Dial completion.
			var result error
			defer func() { expected.done <- result }()
			hash := sha256.New()
			_, _ = hash.Write(header)
			if _, result = conn.Write(header); result != nil {
				return
			}
			var copied int64
			copied, result = io.CopyBuffer(io.MultiWriter(conn, hash), io.LimitReader(conn, mixedPayloadSize-mixedHeaderSize+1), make([]byte, 32<<10))
			if result != nil {
				return
			}
			if copied != mixedPayloadSize-mixedHeaderSize || !bytes.Equal(hash.Sum(nil), expected.hash[:]) {
				result = errors.New("target input length/hash mismatch or replay")
				return
			}
			_, result = io.WriteString(conn, mixedResponseTail)
		}()
	}
}

// begin requires each peer to have received its unique first frame before the
// caller starts file/terminal I/O. The remaining paced TCP writes and concurrent
// reads then overlap that work. Payloads are never retried.
func (m *mixedWorkload) begin(t *testing.T, ctx context.Context, cycle int) func() {
	t.Helper()
	if m == nil {
		return func() {}
	}
	call, cancel := context.WithTimeout(ctx, 12*time.Second)
	start := make(chan struct{})
	type result struct {
		elapsed time.Duration
		err     error
	}
	done := make(chan result, m.clients)
	m.mu.Lock()
	before := m.accepted
	m.expected = make(map[string]*mixedExpectation, m.clients)
	entries := make([]*mixedExpectation, m.clients)
	for i := range m.clients {
		header := fmt.Sprintf("%-64s", fmt.Sprintf("mixed-%08d-%02d", cycle, i))
		payload := append([]byte(header), bytes.Repeat([]byte{byte(cycle), byte(i), 0xff, 0x00}, mixedPayloadSize/4)...)
		payload = payload[:mixedPayloadSize]
		entry := &mixedExpectation{hash: sha256.Sum256(payload), ready: make(chan struct{}), done: make(chan error, 1)}
		entries[i], m.expected[header] = entry, entry
		go func() {
			began := time.Now()
			err := m.exchange(call, start, payload)
			done <- result{time.Since(began), err}
		}()
	}
	m.mu.Unlock()
	for _, entry := range entries {
		select {
		case <-entry.ready:
		case err := <-m.errors:
			cancel()
			for range m.clients {
				<-done
			}
			t.Fatal(err)
		case <-call.Done():
			cancel()
			for range m.clients {
				<-done
			}
			t.Fatal("mixed TCP admission did not finish", call.Err())
		}
	}
	close(start)
	return func() {
		defer cancel()
		var exchangeErr error
		for range m.clients {
			r := <-done
			if r.err != nil {
				exchangeErr = errors.Join(exchangeErr, r.err)
				cancel()
			} else {
				m.latencies.add(r.elapsed)
				m.payloadBytes += mixedPayloadSize
			}
		}
		if exchangeErr != nil {
			t.Fatal("mixed TCP exchange failed", exchangeErr)
		}
		for _, entry := range entries {
			select {
			case err := <-entry.done:
				if err != nil {
					t.Fatal("mixed target validation", err)
				}
			case <-call.Done():
				t.Fatal("mixed target did not finish", call.Err())
			}
		}
		m.settled(t, call)
		m.mu.Lock()
		accepted := m.accepted
		m.mu.Unlock()
		if accepted != before+uint64(m.clients) {
			t.Fatal("mixed workload created extra target connections", accepted-before)
		}
		m.rounds++
	}
}

func (m *mixedWorkload) exchange(ctx context.Context, start <-chan struct{}, payload []byte) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(m.forward.BoundPort))))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	readDone := make(chan error, 1)
	go func() {
		got, err := io.ReadAll(io.LimitReader(conn, int64(mixedPayloadSize+len(mixedResponseTail)+1)))
		if err == nil && (len(got) != mixedPayloadSize+len(mixedResponseTail) || sha256.Sum256(got[:min(len(got), mixedPayloadSize)]) != sha256.Sum256(payload) || !bytes.Equal(got[min(len(got), mixedPayloadSize):], []byte(mixedResponseTail))) {
			err = errors.New("mixed response length/hash/half-close tail mismatch")
		}
		readDone <- err
	}()
	// Always join the reader, including every send/error/cancellation path.
	joined := false
	defer func() {
		conn.Close()
		if !joined {
			<-readDone
		}
	}()
	if _, err = conn.Write(payload[:mixedHeaderSize]); err != nil {
		return err
	}
	select {
	case <-start:
	case <-ctx.Done():
		return ctx.Err()
	}
	for offset := mixedHeaderSize; offset < len(payload); {
		end := min(len(payload), offset+(32<<10))
		if _, err = conn.Write(payload[offset:end]); err != nil {
			return err
		}
		offset = end
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	if err = conn.(*net.TCPConn).CloseWrite(); err != nil {
		return err
	}
	err = <-readDone
	joined = true
	return err
}

func (m *mixedWorkload) settled(t *testing.T, ctx context.Context) {
	t.Helper()
	if m == nil {
		return
	}
	await(t, ctx, func(call context.Context) error {
		select {
		case err := <-m.errors:
			t.Fatal(err)
		default:
		}
		list, err := m.client.Agent().ListForwards(call, &pb.Empty{})
		if err != nil {
			return err
		}
		for _, state := range list.Forwards {
			if state.GetSpec().GetId() == m.forward.Spec.Id {
				if state.State != "listening" || state.BoundPort != m.forward.BoundPort {
					t.Fatal("mixed forward lost its listener", state)
				}
				m.mu.Lock()
				active := len(m.connections)
				m.mu.Unlock()
				if state.Connections == 0 && active == 0 {
					return nil
				}
			}
		}
		return errors.New("waiting for TCP/RPC reclamation")
	})
}

func (m *mixedWorkload) close() error {
	if m == nil {
		return nil
	}
	var stopErr error
	if m.forward != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_, stopErr = m.client.Agent().StopForward(ctx, &apb.StopForwardRequest{ForwardId: m.forward.Spec.Id, ExpectedRevision: m.forward.Spec.Revision, RequestId: rand.Text()})
		cancel()
		m.forward = nil
	}
	m.closeTarget()
	return stopErr
}

func (m *mixedWorkload) closeTarget() {
	_ = m.listener.Close()
	<-m.acceptDone
	m.mu.Lock()
	for conn := range m.connections {
		_ = conn.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// The existing generic E2E waiter polls at 50ms. Performance observations use
// the same bounded received text with a 1ms poll; its scheduling/observation
// overhead remains part of the reported latency, not a transport-only metric.
func mixedTerminalSample(t *testing.T, ctx context.Context, terminal *deviceTerminal, marker string) time.Duration {
	t.Helper()
	terminal.mu.Lock()
	terminal.text = ""
	terminal.mu.Unlock()
	started := time.Now()
	terminal.send(t, marker)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		terminal.mu.Lock()
		found, err := strings.Contains(terminal.text, "E2E_REPLY:"+marker), terminal.err
		terminal.mu.Unlock()
		if found {
			return time.Since(started)
		}
		if err != nil {
			t.Fatal("mixed workload terminal ended", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("mixed workload terminal deadline", ctx.Err())
		case <-ticker.C:
		}
	}
}

func mixedIdleTerminalSamples(t *testing.T, parent context.Context, cancel context.CancelFunc, terminal *deviceTerminal, phase string) *soakLatencies {
	t.Helper()
	result := &soakLatencies{minimum: time.Hour}
	for i := range 20 {
		func() {
			ctx, stop := context.WithTimeout(parent, 12*time.Second)
			watchdog := context.AfterFunc(ctx, cancel)
			defer func() { watchdog(); stop() }()
			result.add(mixedTerminalSample(t, ctx, terminal, fmt.Sprintf("mixed-idle-%s-%02d", phase, i)))
		}()
	}
	return result
}
