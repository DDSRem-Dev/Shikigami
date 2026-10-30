// multihost is an explicitly invoked acceptance helper, not a product command.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
)

type request struct {
	ID   json.RawMessage `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}
type response struct {
	ID        json.RawMessage `json:"id"`
	OK        bool            `json:"ok"`
	Result    any             `json:"result,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
}
type helper struct {
	root, config, cli, agent, daemon, daemonConfig string
	ctx                                            context.Context
	mu                                             sync.Mutex
	fronts                                         map[string]*frontend.Client
	processes                                      map[string]*ownedProcess
	terminals                                      map[string]*terminal
	network                                        map[string]*networkHandle
	closed                                         bool
}

func main() {
	h := &helper{fronts: map[string]*frontend.Client{}, processes: map[string]*ownedProcess{}, terminals: map[string]*terminal{}, network: map[string]*networkHandle{}}
	flag.StringVar(&h.root, "root", "", "owned acceptance root")
	flag.StringVar(&h.config, "config", "", "client configuration")
	flag.StringVar(&h.config, "client-config", "", "client configuration")
	flag.StringVar(&h.cli, "cli", "", "shiki binary")
	flag.StringVar(&h.agent, "agent", "", "agent binary")
	flag.StringVar(&h.daemon, "daemon", "", "optional daemon binary")
	flag.StringVar(&h.daemonConfig, "daemon-config", "", "optional daemon configuration")
	flag.Parse()
	if os.Getuid() == 0 || os.Getuid() != os.Geteuid() {
		fmt.Fprintln(os.Stderr, "ordinary matching UID required")
		os.Exit(2)
	}
	var err error
	h.root, err = filepath.EvalSymlinks(h.root)
	if err != nil || !filepath.IsAbs(h.root) || h.root == "/" {
		fmt.Fprintln(os.Stderr, "invalid private root")
		os.Exit(2)
	}
	info, err := os.Stat(h.root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		fmt.Fprintln(os.Stderr, "root must be private and UID-owned")
		os.Exit(2)
	}
	for _, p := range []string{h.config, h.cli, h.agent, h.daemon, h.daemonConfig} {
		if p != "" {
			if _, err = h.path(p); err != nil {
				fmt.Fprintln(os.Stderr, "input outside private root")
				os.Exit(2)
			}
		}
	}
	if err = h.validateConfiguration(); err != nil {
		fmt.Fprintln(os.Stderr, "configuration escapes acceptance scope")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h.ctx, stop = context.WithTimeout(ctx, 30*time.Minute)
	defer stop()
	var output sync.Mutex
	write := func(r response) { output.Lock(); defer output.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(r) }
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var workers sync.WaitGroup
	semaphore := make(chan struct{}, 16)
	// Closing stdin unblocks the scanner on TERM or the bounded overall deadline.
	go func() { <-h.ctx.Done(); _ = os.Stdin.Close() }()
	for scanner.Scan() {
		var r request
		if json.Unmarshal(scanner.Bytes(), &r) != nil || len(r.ID) == 0 {
			write(response{ID: json.RawMessage("null"), ErrorCode: "invalid_argument"})
			continue
		}
		select {
		case semaphore <- struct{}{}:
		case <-h.ctx.Done():
			break
		}
		if h.ctx.Err() != nil {
			break
		}
		workers.Add(1)
		go func(r request) {
			defer workers.Done()
			defer func() { <-semaphore }()
			ctx, cancel := context.WithTimeout(h.ctx, 90*time.Second)
			defer cancel()
			result, err := h.dispatch(ctx, r)
			code := errorCode(err)
			if err != nil && ctx.Err() == nil && (r.Op == "cli" || r.Op == "daemon.cli") {
				if v, ok := result.(map[string]any); ok && v["exit_code"] != 0 {
					code = "cli_exit"
				}
			}
			write(response{ID: r.ID, OK: err == nil, Result: result, ErrorCode: code})
		}(r)
	}
	stop()
	workers.Wait()
	if err = h.cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "owned cleanup incomplete")
		os.Exit(1)
	}
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return connect.CodeOf(err).String()
}
func args[T any](r request) (T, error) {
	var v T
	if len(r.Args) == 0 {
		return v, nil
	}
	err := json.Unmarshal(r.Args, &v)
	return v, err
}
func invalid() error {
	return connect.NewError(connect.CodeInvalidArgument, "invalid acceptance request")
}
func (h *helper) dispatch(ctx context.Context, r request) (any, error) {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed && r.Op != "cleanup" && r.Op != "ping" {
		return nil, invalid()
	}
	switch r.Op {
	case "ping":
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(exe)
		if err != nil {
			return nil, err
		}
		return map[string]any{"uid": os.Getuid(), "os": runtime.GOOS, "arch": runtime.GOARCH, "root": h.root, "sha256": digest(data)}, nil
	case "cli", "daemon.cli":
		return h.command(ctx, r)
	case "rpc":
		return h.rpc(ctx, r)
	case "upload", "conflict.preview":
		return h.fileStream(ctx, r)
	case "frontend.open", "frontend.close":
		return h.frontendOp(ctx, r)
	case "file.write", "file.stat", "file.remove", "file.list":
		return h.fileOp(ctx, r)
	case "sync.snapshot":
		return h.syncSnapshot(ctx, r)
	case "socket.probe":
		return h.socketProbe(ctx, r)
	case "process.start", "process.stop", "process.kill", "process.inventory", "retired.check":
		return h.processOp(ctx, r)
	case "terminal.open", "terminal.send", "terminal.state", "terminal.close":
		return h.terminalOp(ctx, r)
	case "proxy.start", "proxy.mode", "proxy.close", "echo.start", "tcp.open", "tcp.send", "tcp.state", "tcp.close":
		return h.networkOp(ctx, r)
	case "tui.capture", "terminal.ui":
		return h.tui(ctx, r)
	case "cleanup":
		err := h.cleanup()
		return map[string]bool{"closed": true, "passed": err == nil}, err
	default:
		return nil, invalid()
	}
}
func (h *helper) cleanup() error {
	h.mu.Lock()
	h.closed = true
	var ts []*terminal
	for _, t := range h.terminals {
		ts = append(ts, t)
	}
	var ns []*networkHandle
	for _, n := range h.network {
		ns = append(ns, n)
	}
	var fs []*frontend.Client
	for _, f := range h.fronts {
		fs = append(fs, f)
	}
	h.mu.Unlock()
	var failures []error
	for _, t := range ts {
		failures = append(failures, t.close())
	}
	for _, n := range ns {
		failures = append(failures, n.close())
	}
	for _, f := range fs {
		failures = append(failures, f.Close())
	}
	for _, role := range []string{"agent", "daemon"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		failures = append(failures, h.stopProcess(ctx, role, false))
		cancel()
	}
	return errors.Join(failures...)
}
