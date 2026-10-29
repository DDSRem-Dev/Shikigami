//go:build shiki_load_capacity

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const headerSize = 64
const chunkSize = 64 << 10
const responseTail = "CAPACITY_AFTER_REMOTE_WRITE_EOF\n"

type expectation struct {
	Header              string
	Size                int64
	Chunk               []byte
	Hash                [32]byte
	Ready               chan struct{}
	TargetDone          chan error
	PayloadReady        chan struct{}
	FirstData, LastData int64
	seen                bool
}
type target struct {
	listener       net.Listener
	mu             sync.Mutex
	expected       map[string]*expectation
	connections    map[net.Conn]struct{}
	accepted, peak int
	acceptDone     chan struct{}
	wg             sync.WaitGroup
	errors         chan error
	closing        bool
}

func newTarget() *target {
	t := &target{listener: take(net.Listen("tcp4", "127.0.0.1:0")), connections: map[net.Conn]struct{}{}, acceptDone: make(chan struct{}), errors: make(chan error, 1)}
	go t.accept()
	return t
}
func (t *target) accept() {
	defer close(t.acceptDone)
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			t.mu.Lock()
			closing := t.closing
			t.mu.Unlock()
			if !closing {
				select {
				case t.errors <- fmt.Errorf("target accept: %w", err):
				default:
				}
			}
			return
		}
		t.mu.Lock()
		t.connections[conn] = struct{}{}
		t.accepted++
		t.peak = max(t.peak, len(t.connections))
		t.mu.Unlock()
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer func() { conn.Close(); t.mu.Lock(); delete(t.connections, conn); t.mu.Unlock() }()
			t.serve(conn)
		}()
	}
}
func (t *target) serve(conn net.Conn) {
	var entry *expectation
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("target: %v", r)
			if entry != nil {
				entry.TargetDone <- err
			}
			select {
			case t.errors <- err:
			default:
			}
		}
	}()
	need(conn.SetDeadline(time.Now().Add(120 * time.Second)))
	header := make([]byte, headerSize)
	_, err := io.ReadFull(conn, header)
	need(err)
	t.mu.Lock()
	entry = t.expected[string(header)]
	valid := entry != nil && !entry.seen
	if valid {
		entry.seen = true
	}
	t.mu.Unlock()
	ensure(valid, "unknown or repeated target header")
	close(entry.Ready)
	_, err = conn.Write(header)
	need(err)
	hash := sha256.New()
	buffer := make([]byte, chunkSize)
	remaining := entry.Size
	for remaining > 0 {
		n, readErr := conn.Read(buffer[:min(int64(len(buffer)), remaining)])
		ensure(n > 0, "target empty DATA read")
		now := time.Now().UnixNano()
		if entry.FirstData == 0 {
			entry.FirstData = now
		}
		entry.LastData = now
		_, err = hash.Write(buffer[:n])
		need(err)
		_, err = conn.Write(buffer[:n])
		need(err)
		remaining -= int64(n)
		if readErr != nil {
			ensure(errors.Is(readErr, io.EOF) && remaining == 0, "early target read failure")
			break
		}
	}
	ensure(bytes.Equal(hash.Sum(nil), entry.Hash[:]), "target payload hash mismatch")
	close(entry.PayloadReady)
	one := make([]byte, 1)
	n, err := conn.Read(one)
	ensure(n == 0 && errors.Is(err, io.EOF), "target requires exact writer half-close")
	ensure(bytes.Equal(hash.Sum(nil), entry.Hash[:]), "target payload hash mismatch")
	_, err = io.WriteString(conn, responseTail)
	need(err)
	entry.TargetDone <- nil
}
func (t *target) close() {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	need(t.listener.Close())
	<-t.acceptDone
	t.mu.Lock()
	for conn := range t.connections {
		_ = conn.Close()
	}
	t.mu.Unlock()
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		panic("target handlers did not join")
	}
}
func (t *target) counts() (int, int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.connections), t.accepted, t.peak
}
func (t *target) prepare(round, count int, size int64) []*expectation {
	t.mu.Lock()
	defer t.mu.Unlock()
	ensure(len(t.connections) == 0, "target not settled before round")
	t.expected = map[string]*expectation{}
	t.peak = 0
	entries := make([]*expectation, count)
	for i := range count {
		chunk := make([]byte, chunkSize)
		for j := range chunk {
			chunk[j] = byte(j*31 + i*17 + round)
		}
		hash := sha256.New()
		for left := size; left > 0; left -= min(left, int64(chunkSize)) {
			_, _ = hash.Write(chunk[:min(left, int64(chunkSize))])
		}
		entry := &expectation{Header: fmt.Sprintf("%-64s", fmt.Sprintf("capacity-%06d-%04d", round, i)), Size: size, Chunk: chunk, Ready: make(chan struct{}), TargetDone: make(chan error, 1), PayloadReady: make(chan struct{})}
		copy(entry.Hash[:], hash.Sum(nil))
		entries[i] = entry
		t.expected[entry.Header] = entry
	}
	return entries
}

type exchange struct {
	entry                  *expectation
	conn                   *net.TCPConn
	gate                   chan struct{}
	done                   chan error
	finished               chan struct{}
	elapsed                time.Duration
	PayloadEcho            chan struct{}
	SentDATA, ReceivedDATA dataInterval
	holdEOF                bool
	eofGate                chan struct{}
}

func connectExchange(ctx context.Context, address string, entry *expectation, holdEOF ...bool) *exchange {
	return connectWithGate(ctx, address, entry, nil, len(holdEOF) > 0 && holdEOF[0])
}
func connectWithGate(ctx context.Context, address string, entry *expectation, gate chan struct{}, holdEOF bool) *exchange {
	conn := take((&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", address)).(*net.TCPConn)
	x := &exchange{entry: entry, conn: conn, gate: make(chan struct{}), done: make(chan error, 1), finished: make(chan struct{}), PayloadEcho: make(chan struct{}), eofGate: make(chan struct{})}
	if gate != nil {
		x.gate = gate
	}
	if holdEOF {
		x.holdEOF = true
		close(x.gate)
	}
	go x.run(ctx)
	return x
}
func (x *exchange) run(ctx context.Context) {
	var final error
	var reader chan error
	defer func() {
		if r := recover(); r != nil {
			final = fmt.Errorf("client: %v", r)
		}
		_ = x.conn.Close()
		if reader != nil {
			final = errors.Join(final, <-reader)
		}
		x.done <- final
		close(x.finished)
	}()
	stop := context.AfterFunc(ctx, func() { _ = x.conn.Close() })
	defer stop()
	need(x.conn.SetDeadline(time.Now().Add(120 * time.Second)))
	_, err := io.WriteString(x.conn, x.entry.Header)
	need(err)
	readDone := make(chan error, 1)
	reader = readDone
	go func() {
		var result error
		defer func() {
			if r := recover(); r != nil {
				result = fmt.Errorf("response: %v", r)
			}
			readDone <- result
		}()
		header := make([]byte, headerSize)
		_, err := io.ReadFull(x.conn, header)
		need(err)
		ensure(string(header) == x.entry.Header, "echo header mismatch")
		hash := sha256.New()
		buffer := make([]byte, chunkSize)
		for remaining := x.entry.Size; remaining > 0; {
			n, readErr := x.conn.Read(buffer[:min(int64(len(buffer)), remaining)])
			if n > 0 {
				now := time.Now().UnixNano()
				if x.ReceivedDATA.First == 0 {
					x.ReceivedDATA.First = now
				}
				x.ReceivedDATA.Last = now
				_, err = hash.Write(buffer[:n])
				need(err)
				remaining -= int64(n)
			}
			need(readErr)
			ensure(n > 0, "empty echo DATA read")
		}
		ensure(bytes.Equal(hash.Sum(nil), x.entry.Hash[:]), "echo payload hash/size mismatch")
		close(x.PayloadEcho)
		tail := make([]byte, len(responseTail))
		_, err = io.ReadFull(x.conn, tail)
		need(err)
		ensure(string(tail) == responseTail, "post-halfclose tail mismatch")
		one := make([]byte, 1)
		nread, err := x.conn.Read(one)
		ensure(nread == 0 && errors.Is(err, io.EOF), "response final EOF missing")
	}()
	select {
	case <-x.gate:
	case <-ctx.Done():
		panic(ctx.Err())
	}
	started := time.Now()
	x.SentDATA.First = started.UnixNano()
	for left := x.entry.Size; left > 0; left -= min(left, int64(len(x.entry.Chunk))) {
		data := x.entry.Chunk[:min(left, int64(len(x.entry.Chunk)))]
		n, err := x.conn.Write(data)
		need(err)
		ensure(n == len(data), "short TCP writer")
		x.SentDATA.Last = time.Now().UnixNano()
	}
	if x.holdEOF {
		select {
		case <-x.eofGate:
		case <-ctx.Done():
			panic(ctx.Err())
		}
	}
	need(x.conn.CloseWrite())
	result := <-readDone
	reader = nil
	need(result)
	x.elapsed = time.Since(started)
}
func (x *exchange) release() {
	if x.holdEOF {
		close(x.eofGate)
	} else {
		close(x.gate)
	}
}
func waitReady(ctx context.Context, entries []*expectation, t *target) {
	for _, entry := range entries {
		select {
		case <-entry.Ready:
		case err := <-t.errors:
			panic(err)
		case <-ctx.Done():
			panic(ctx.Err())
		}
	}
}
func joinExchanges(ctx context.Context, exchanges []*exchange) {
	for _, x := range exchanges {
		select {
		case err := <-x.done:
			need(err)
		case <-ctx.Done():
			panic(ctx.Err())
		}
		select {
		case err := <-x.entry.TargetDone:
			need(err)
		case <-ctx.Done():
			panic(ctx.Err())
		}
	}
}
