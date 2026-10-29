//go:build shiki_load_capacity

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// This is the complete1957-byte synthetic PTY capture from the failed actual
// terminal-only diagnostic, including20 earlier accepted replies and all VT
// framing. Every possible two-chunk split and one-byte chunks use the same line
// framing and marker decoder as the running diagnostic.
func TestCapturedTmuxReplyEverySplit(t *testing.T) {
	raw, err := os.ReadFile("testdata/tmux-reply-tail.raw")
	if err != nil {
		t.Fatal(err)
	}
	check := func(chunks [][]byte) {
		t.Helper()
		var tail []byte
		count := 0
		for _, chunk := range chunks {
			tail = append(tail, chunk...)
			for {
				start, end, ok := findReplyLine(tail)
				if !ok {
					break
				}
				marker, err := replyMarker(tail[start:end])
				if err != nil {
					t.Fatal(err)
				}
				count++
				want := fmt.Sprintf("cap-%09d", count)
				if marker != want {
					t.Fatalf("reply%d got %q want %q", count, marker, want)
				}
				tail = tail[end:]
			}
		}
		if count != 21 {
			t.Fatalf("decoded%d replies, want21", count)
		}
	}
	for cut := 0; cut <= len(raw); cut++ {
		check([][]byte{raw[:cut], raw[cut:]})
	}
	chunks := make([][]byte, len(raw))
	for i := range raw {
		chunks[i] = raw[i : i+1]
	}
	check(chunks)
}
func TestReplyTailControls(t *testing.T) {
	for _, tail := range []string{"", "\x1b[K", "\x1b[0K"} {
		line := []byte("CAPACITY_REPLY:cap-000000021" + tail + "\r\n")
		marker, err := replyMarker(line)
		if err != nil || marker != "cap-000000021" {
			t.Fatalf("tail%q: %q %v", tail, marker, err)
		}
	}
	for _, tail := range []string{"\x1b[1K", "\x1b[2K", "\x1b[0m", "suffix", "\x1b[", "\x1b[Kextra"} {
		if _, err := replyMarker([]byte("CAPACITY_REPLY:cap-000000021" + tail + "\r\n")); err == nil {
			t.Fatalf("unsupported/erasing tail %q accepted", tail)
		}
	}
	if _, err := replyMarker([]byte("CAPACITY_REPLY:cap-000000021")); err == nil {
		t.Fatal("truncated reply accepted")
	}
}

type capturedReader struct {
	chunks [][]byte
	stop   chan struct{}
	once   sync.Once
}

func (r *capturedReader) Read(p []byte) (int, error) {
	if len(r.chunks) > 0 {
		chunk := r.chunks[0]
		r.chunks = r.chunks[1:]
		return copy(p, chunk), nil
	}
	<-r.stop
	return 0, io.EOF
}
func TestTrueDuplicateAndUnknownStillFail(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		input     []byte
		completed int
	}{{"duplicate", []byte("CAPACITY_REPLY:cap-000000021\r\nCAPACITY_REPLY:cap-000000021\x1b[K\r\n"), 1}, {"unknown", []byte("CAPACITY_REPLY:cap-000000022\x1b[K\r\n"), 0}, {"erased", []byte("CAPACITY_REPLY:cap-000000021\x1b[2K\r\n"), 0}} {
		t.Run(scenario.name, func(t *testing.T) {
			r := &capturedReader{chunks: [][]byte{scenario.input}, stop: make(chan struct{})}
			observer := &terminal{done: make(chan struct{}), replies: make(chan time.Time, 1), errors: make(chan error, 1), ready: make(chan struct{}), pending: "cap-000000021", sequence: 21, tracePath: t.TempDir()}
			go observer.readStream(r)
			defer func() {
				observer.mu.Lock()
				observer.closing = true
				observer.mu.Unlock()
				r.once.Do(func() { close(r.stop) })
				select {
				case <-observer.done:
				case <-time.After(time.Second):
					t.Error("reader cleanup failed")
				}
			}()
			select {
			case <-observer.errors:
			case <-time.After(time.Second):
				t.Fatal("invalid reply not rejected")
			}
			observer.mu.Lock()
			completed := observer.completed
			observer.mu.Unlock()
			if completed != scenario.completed || terminalError(observer) == nil {
				t.Fatalf("completed%d or error latch incorrect", completed)
			}
		})
	}
}
