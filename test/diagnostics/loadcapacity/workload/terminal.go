//go:build shiki_load_capacity

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type terminalSample struct {
	Sent, Received      int64
	Milliseconds        float64
	Stage               int
	Loaded              bool
	CoveredStreams      []string
	Scheduled           int64
	SkippedTicks        int64
	SentGapMilliseconds float64
	Marker              string
}
type terminal struct {
	file            *os.File
	cmd             *exec.Cmd
	done            chan struct{}
	mu              sync.Mutex
	pending         string
	sequence        int
	replies         chan time.Time
	errors          chan error
	ready           chan struct{}
	previousSent    int64
	completed       int
	tracePath       string
	frames          []terminalFrame
	traceBytes      int
	totalOffset     int64
	frameSequence   int
	diagnosticSaved bool
	closing         bool
	latchedError    error
	errorCount      int
	redraws         []terminalRedraw
	seenReplies     map[string]bool
}
type terminalFrame struct {
	Sequence   int
	At, Offset int64
	Bytes      []byte
}

func openTerminal(e *environment) *terminal {
	cmd := exec.Command(filepath.Join(e.bin, "shiki"), "--config", e.configuration, "attach", e.session.Id)
	cmd.Env = e.env
	file := take(pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 120}))
	guard := true
	fd := -1
	defer func() {
		original := recover()
		if guard {
			func() {
				defer func() {
					if r := recover(); r != nil {
						e.report.Errors = append(e.report.Errors, "terminal initialization cleanup: "+fmt.Sprint(r))
					}
				}()
				_ = file.Close()
				if fd >= 0 {
					_ = unix.Close(fd)
				}
				stopProcess(cmd)
			}()
		}
		if original != nil {
			panic(original)
		}
	}()
	e.remember([]processIdentity{identity(int32(cmd.Process.Pid))})
	fd = take(unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0))
	need(unix.SetNonblock(fd, true))
	need(file.Close())
	t := &terminal{file: os.NewFile(uintptr(fd), "capacity-cli-pty"), cmd: cmd, done: make(chan struct{}), replies: make(chan time.Time, 1), errors: make(chan error, 1), ready: make(chan struct{})}
	t.tracePath = e.output
	go t.read()
	guard = false
	fd = -1
	e.deferClose(t.close)
	select {
	case <-t.ready:
	case err := <-t.errors:
		panic(err)
	case <-time.After(15 * time.Second):
		panic("CLI attachment did not show fixture readiness")
	}
	return t
}
func (t *terminal) fail(err error) {
	t.mu.Lock()
	if t.latchedError == nil {
		t.latchedError = err
	}
	t.errorCount++
	t.mu.Unlock()
	select {
	case t.errors <- err:
	default:
	}
}
func (t *terminal) read() {
	t.readStream(t.file)
}
func findReplyLine(tail []byte) (int, int, bool) {
	start := bytes.Index(tail, []byte("CAPACITY_REPLY:cap-"))
	if start < 0 {
		return 0, 0, false
	}
	end := bytes.IndexByte(tail[start:], '\n')
	if end < 0 {
		return 0, 0, false
	}
	return start, start + end + 1, true
}
func replyMarker(line []byte) (string, error) {
	const prefix = "CAPACITY_REPLY:"
	if !bytes.HasPrefix(line, []byte(prefix)) || !bytes.HasSuffix(line, []byte{'\n'}) {
		return "", errors.New("incomplete fixture reply line")
	}
	body := strings.TrimSuffix(string(line[len(prefix):len(line)-1]), "\r")
	if len(body) < 13 || !strings.HasPrefix(body, "cap-") {
		return "", errors.New("invalid fixture marker")
	}
	marker := body[:13]
	for _, digit := range marker[4:] {
		if digit < '0' || digit > '9' {
			return marker, errors.New("invalid fixture marker digits")
		}
	}
	// tmux can erase the unused cells to the right of the just-written nonce.
	// EL0 leaves those nonce cells intact. EL1/EL2, other controls and arbitrary
	// suffix text are deliberately unsupported, not stripped or ignored.
	switch body[13:] {
	case "", "\x1b[K", "\x1b[0K":
		return marker, nil
	default:
		return marker, errors.New("unsupported or destructive terminal reply tail")
	}
}

const maxTerminalObservations = 100000

type terminalRedraw struct {
	Marker string
	Offset int64
	Row    int
	At     int64
}

func (t *terminal) observeReply(obs replyObservation) {
	t.mu.Lock()
	expected := t.pending
	if len(t.seenReplies) >= maxTerminalObservations || len(t.redraws) >= maxTerminalObservations {
		t.mu.Unlock()
		t.fail(errors.New("terminal observation hard limit exceeded"))
		return
	}
	accepted := obs.Err == nil && obs.Marker == expected && !obs.AlreadyVisible && !t.seenReplies[obs.Marker]
	// sequence is monotonic and every completed marker must have matched the
	// sole pending request. Old markers are accepted only with cell evidence.
	old := t.seenReplies[obs.Marker]
	redraw := obs.Err == nil && old && obs.Redraw
	if accepted {
		t.pending = ""
		t.completed++
		if t.seenReplies == nil {
			t.seenReplies = make(map[string]bool)
		}
		t.seenReplies[obs.Marker] = true
	}
	sent, completed := t.sequence, t.completed
	if redraw {
		t.redraws = append(t.redraws, terminalRedraw{obs.Marker, obs.Offset, obs.Row, time.Now().UnixNano()})
	}
	t.mu.Unlock()
	if redraw {
		return
	}
	if !accepted {
		if !t.diagnosticSaved {
			t.diagnosticSaved = true
			save(filepath.Join(t.tracePath, "terminal-observer-error.json"), map[string]any{"actual_marker": obs.Marker, "expected_marker": expected, "actual_line_hex": hex.EncodeToString(obs.Raw), "line_offset": obs.Offset, "total_offset": t.totalOffset, "retained_bytes": t.traceBytes, "discarded_bytes": t.totalOffset - int64(t.traceBytes), "truncated": t.totalOffset > int64(t.traceBytes), "frames": t.frames, "sent_markers": sent, "completed_markers": completed, "observed_at": time.Now().UnixNano(), "already_visible": obs.AlreadyVisible, "redraw_cells_unchanged": obs.Redraw, "row": obs.Row})
		}
		if obs.Err != nil {
			t.fail(fmt.Errorf("terminal reply decoding: %w", obs.Err))
		} else {
			t.fail(fmt.Errorf("unexpected, duplicate, or previously visible terminal reply marker"))
		}
		return
	}
	select {
	case t.replies <- time.Now():
	default:
		t.fail(fmt.Errorf("bounded terminal reply queue overflow"))
	}
}
func (t *terminal) readStream(input io.Reader) {
	defer close(t.done)
	decoder := newReplyVT(t.observeReply)
	defer decoder.close()
	buffer := make([]byte, 8192)
	var readiness []byte
	ready := false
	for {
		n, err := input.Read(buffer)
		if n > 0 {
			t.frameSequence++
			t.frames = append(t.frames, terminalFrame{t.frameSequence, time.Now().UnixNano(), t.totalOffset, append([]byte(nil), buffer[:n]...)})
			t.totalOffset += int64(n)
			t.traceBytes += n
			for t.traceBytes > 64<<10 || len(t.frames) > 1024 {
				t.traceBytes -= len(t.frames[0].Bytes)
				t.frames = t.frames[1:]
			}
			if !ready {
				readiness = append(readiness, buffer[:n]...)
				if bytes.Contains(readiness, []byte("CAPACITY_READY")) {
					close(t.ready)
					ready = true
				}
				if len(readiness) > 1024 {
					readiness = append([]byte(nil), readiness[len(readiness)-1024:]...)
				}
			}
			decoder.write(buffer[:n])
		}
		if err != nil {
			t.mu.Lock()
			closing := t.closing
			t.mu.Unlock()
			if !closing {
				t.fail(fmt.Errorf("CLI PTY read ended: %w", err))
			}
			return
		}
	}
}
func (t *terminal) sample(ctx context.Context, stage int) terminalSample {
	t.mu.Lock()
	t.sequence++
	marker := fmt.Sprintf("cap-%09d", t.sequence)
	ensure(t.pending == "", "terminal sender overlap")
	t.pending = marker
	t.mu.Unlock()
	started := time.Now()
	previous := t.previousSent
	t.previousSent = started.UnixNano()
	need(t.file.SetWriteDeadline(started.Add(5 * time.Second)))
	_, err := io.WriteString(t.file, marker+"\n")
	need(err)
	select {
	case received := <-t.replies:
		value := terminalSample{Sent: started.UnixNano(), Received: received.UnixNano(), Milliseconds: float64(received.Sub(started)) / float64(time.Millisecond), Stage: stage, Marker: marker}
		if previous > 0 {
			value.SentGapMilliseconds = float64(value.Sent-previous) / float64(time.Millisecond)
		}
		return value
	case err := <-t.errors:
		panic(err)
	case <-ctx.Done():
		panic(ctx.Err())
	case <-time.After(5 * time.Second):
		panic("terminal marker timed out")
	}
}
func (t *terminal) close() {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	_, err := t.file.Write([]byte{0x11})
	if err != nil {
		_ = t.cmd.Process.Signal(syscall.SIGTERM)
	}
	var waitErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				waitErr = fmt.Errorf("terminal Wait: %v", r)
			}
		}()
		waitProcess(t.cmd)
	}()
	_ = t.file.Close()
	select {
	case <-t.done:
		save(filepath.Join(t.tracePath, "terminal-trace.json"), map[string]any{"frames": t.frames, "total_offset": t.totalOffset, "retained_bytes": t.traceBytes, "discarded_bytes": t.totalOffset - int64(t.traceBytes), "truncated": t.totalOffset > int64(t.traceBytes), "presentation_redraws": t.redraws})
	case <-time.After(5 * time.Second):
		waitErr = errors.Join(waitErr, errors.New("terminal reader not joined"))
	}
	need(errors.Join(waitErr, terminalError(t)))
}
func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitProcess(cmd)
}
func waitProcess(cmd *exec.Cmd) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		need(err)
		return
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		panic("private child required forced kill: " + filepath.Base(cmd.Path))
	}
}
func (e *environment) idle(stage int) []terminalSample {
	var samples []terminalSample
	defer func() { save(filepath.Join(e.output, fmt.Sprintf("idle-%d.json", stage)), samples) }()
	e.terminal.collect(e.ctx, nil, stage, 40, &samples)
	need(e.ctx.Err())
	ensure(len(samples) == 40, "idle sampling did not complete forty replies")
	return samples
}
func (t *terminal) collect(ctx context.Context, stop <-chan struct{}, stage, limit int, samples *[]terminalSample) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for limit == 0 || len(*samples) < limit {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case tick := <-ticker.C:
			value := t.sample(ctx, stage)
			value.Scheduled = tick.UnixNano()
			value.SkippedTicks = int64(time.Since(tick) / (25 * time.Millisecond))
			*samples = append(*samples, value)
			select {
			case <-ticker.C:
			default:
			}
		}
	}
}
func terminalError(t *terminal) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latchedError
}
