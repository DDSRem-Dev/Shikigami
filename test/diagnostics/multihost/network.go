package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func (h *helper) socketProbe(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Path string `json:"path"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	relative, err := h.path(a.Path)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(h.root, relative)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{"exists": false, "connectable": false}, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, invalid()
	}
	probe, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(probe, "unix", path)
	if err == nil {
		_ = c.Close()
		return map[string]bool{"exists": true, "connectable": true}, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return map[string]bool{"exists": true, "connectable": false}, nil
	}
	return nil, err
}

type networkHandle struct {
	mu           sync.Mutex
	write        sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	listener     net.Listener
	conn         net.Conn
	connections  map[net.Conn]bool
	done         chan struct{}
	once         sync.Once
	slow, paused bool
	tail         string
	bytes        int64
	hash         hash.Hash
	err          error
	workers      sync.WaitGroup
}

func (n *networkHandle) close() error {
	n.once.Do(func() {
		n.cancel()
		if n.listener != nil {
			_ = n.listener.Close()
		}
		n.mu.Lock()
		for c := range n.connections {
			_ = c.Close()
		}
		if n.conn != nil {
			_ = n.conn.Close()
		}
		n.mu.Unlock()
	})
	select {
	case <-n.done:
		return nil
	case <-time.After(5 * time.Second):
		return context.DeadlineExceeded
	}
}
func loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && net.ParseIP(host).IsLoopback()
}
func addressOK(address string) bool {
	host, p, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil {
		return false
	}
	port, err := strconv.Atoi(p)
	return err == nil && port > 0 && port <= 65535
}
func (h *helper) networkOp(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Handle  string `json:"handle"`
		Target  string `json:"target"`
		Address string `json:"address"`
		Marker  string `json:"marker"`
		Text    string `json:"text"`
		Match   string `json:"match"`
		Slow    bool   `json:"slow"`
		Paused  bool   `json:"paused"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	if r.Op == "proxy.start" || r.Op == "echo.start" || r.Op == "tcp.open" {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.closed || len(h.network) >= 32 {
			return nil, invalid()
		}
		if a.Handle == "" {
			a.Handle = "net-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if h.network[a.Handle] != nil {
			return nil, invalid()
		}
		call, cancel := context.WithCancel(h.ctx)
		n := &networkHandle{ctx: call, cancel: cancel, connections: map[net.Conn]bool{}, done: make(chan struct{}), hash: sha256.New()}
		if r.Op == "tcp.open" {
			if !addressOK(a.Address) || !loopback(a.Address) || len(a.Marker) > 64<<10 {
				cancel()
				return nil, invalid()
			}
			n.conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", a.Address)
			if err != nil {
				cancel()
				return nil, err
			}
			h.network[a.Handle] = n
			go n.readTCP()
			if a.Marker != "" {
				n.write.Lock()
				_ = n.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				_, err = io.WriteString(n.conn, a.Marker)
				n.write.Unlock()
			}
			return map[string]string{"handle": a.Handle}, err
		}
		if r.Op == "proxy.start" && (!addressOK(a.Target) || !loopback(a.Target)) {
			cancel()
			return nil, invalid()
		}
		n.listener, err = net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			cancel()
			return nil, err
		}
		h.network[a.Handle] = n
		go n.accept(a.Target, r.Op == "echo.start")
		return map[string]any{"handle": a.Handle, "address": n.listener.Addr().String(), "port": n.listener.Addr().(*net.TCPAddr).Port}, nil
	}
	h.mu.Lock()
	n := h.network[a.Handle]
	h.mu.Unlock()
	if n == nil {
		return nil, invalid()
	}
	switch r.Op {
	case "proxy.close", "tcp.close":
		return map[string]bool{"closed": true}, n.close()
	case "proxy.mode":
		n.mu.Lock()
		n.slow = a.Slow
		n.paused = a.Paused
		n.mu.Unlock()
		return map[string]bool{"slow": a.Slow, "paused": a.Paused}, nil
	case "tcp.send":
		if len(a.Text) > 64<<10 {
			return nil, invalid()
		}
		if a.Text == "" {
			a.Text = a.Marker
		}
		if n.conn == nil {
			return nil, invalid()
		}
		n.write.Lock()
		defer n.write.Unlock()
		_ = n.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err = n.conn.Write([]byte(a.Text))
		return map[string]bool{"sent": err == nil}, err
	case "tcp.state":
		n.mu.Lock()
		defer n.mu.Unlock()
		match := a.Match
		if match == "" {
			match = a.Marker
		}
		count := 0
		if match != "" {
			count = strings.Count(n.tail, match)
		}
		return map[string]any{"bytes": n.bytes, "sha256": hex.EncodeToString(n.hash.Sum(nil)), "matched": count > 0, "match_count": count, "closed": n.err != nil, "error_code": errorCode(n.err)}, nil
	}
	return nil, invalid()
}
func (n *networkHandle) readTCP() {
	defer close(n.done)
	buffer := make([]byte, 32768)
	for {
		count, err := n.conn.Read(buffer)
		n.mu.Lock()
		n.bytes += int64(count)
		_, _ = n.hash.Write(buffer[:count])
		n.tail += string(buffer[:count])
		if len(n.tail) > 256<<10 {
			n.tail = n.tail[len(n.tail)-(256<<10):]
		}
		if err != nil {
			n.err = err
			n.mu.Unlock()
			return
		}
		n.mu.Unlock()
	}
}
func (n *networkHandle) accept(target string, echo bool) {
	defer close(n.done)
	defer n.workers.Wait()
	for {
		c, err := n.listener.Accept()
		if err != nil {
			return
		}
		n.mu.Lock()
		if n.ctx.Err() != nil || len(n.connections) >= 64 {
			n.mu.Unlock()
			c.Close()
			continue
		}
		n.connections[c] = true
		n.mu.Unlock()
		n.workers.Add(1)
		go func() {
			defer n.workers.Done()
			defer c.Close()
			defer func() { n.mu.Lock(); delete(n.connections, c); n.mu.Unlock() }()
			if echo {
				scanner := bufio.NewScanner(c)
				scanner.Buffer(make([]byte, 4096), 64<<10)
				for scanner.Scan() {
					if _, err := io.WriteString(c, "ECHO:"+scanner.Text()+"\n"); err != nil {
						return
					}
				}
				return
			}
			up, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(n.ctx, "tcp", target)
			if err != nil {
				return
			}
			defer up.Close()
			n.mu.Lock()
			if n.ctx.Err() != nil {
				n.mu.Unlock()
				return
			}
			n.connections[up] = true
			n.mu.Unlock()
			defer func() { n.mu.Lock(); delete(n.connections, up); n.mu.Unlock() }()
			done := make(chan struct{})
			go func() {
				n.copy(up, c)
				if tcp, ok := up.(*net.TCPConn); ok {
					_ = tcp.CloseWrite()
				}
				close(done)
			}()
			n.copy(c, up)
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
			_ = up.SetReadDeadline(time.Now())
			_ = c.SetReadDeadline(time.Now())
			<-done
		}()
	}
}
func (n *networkHandle) copy(dst, src net.Conn) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := src.Read(buffer)
		if count > 0 {
			for {
				n.mu.Lock()
				paused, slow := n.paused, n.slow
				n.mu.Unlock()
				if n.ctx.Err() != nil {
					return
				}
				if !paused {
					if slow {
						select {
						case <-n.ctx.Done():
							return
						case <-time.After(25 * time.Millisecond):
						}
					}
					break
				}
				select {
				case <-n.ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
			if _, writeErr := io.Copy(dst, strings.NewReader(string(buffer[:count]))); writeErr != nil {
				return
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
	}
}
