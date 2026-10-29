//go:build shiki_load_capacity

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Both actual failed diagnostic captures are replayed through the production
// diagnostic observer, including its pending/seen bookkeeping. Full captures
// use original frames, one block and bytewise chunks. Each actual failing
// candidate is also split at every byte; unrelated cuts add no new branch
// coverage because replyVT.write always advances the same one-byte parser.
func TestVTActualCapturesAndCandidateSplits(t *testing.T) {
	for _, fixture := range []struct {
		name             string
		replies, redraws int
	}{
		{"tmux-reply-tail.raw", 21, 0}, {"tmux-reply-redraw.raw", 195, 15},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + fixture.name)
			if err != nil {
				t.Fatal(err)
			}
			tracePath := t.TempDir()
			check := func(chunks [][]byte) {
				observer := &terminal{replies: make(chan time.Time, 1), errors: make(chan error, 1), pending: "cap-000000001", sequence: 1, tracePath: tracePath}
				decoder := newReplyVT(func(obs replyObservation) {
					observer.observeReply(obs)
					if err := terminalError(observer); err != nil {
						t.Fatalf("offset %d marker %s: %v", obs.Offset, obs.Marker, err)
					}
					select {
					case <-observer.replies:
						observer.sequence++
						observer.pending = fmt.Sprintf("cap-%09d", observer.sequence)
					default:
					}
				})
				defer decoder.close()
				for _, chunk := range chunks {
					decoder.write(chunk)
				}
				if observer.completed != fixture.replies || len(observer.redraws) != fixture.redraws {
					t.Fatalf("got %d replies/%d redraws, want %d/%d", observer.completed, len(observer.redraws), fixture.replies, fixture.redraws)
				}
			}
			check([][]byte{raw})
			frameData, err := os.ReadFile("testdata/" + strings.TrimSuffix(fixture.name, ".raw") + "-frames.json")
			if err != nil {
				t.Fatal(err)
			}
			var lengths []int
			if err := json.Unmarshal(frameData, &lengths); err != nil {
				t.Fatal(err)
			}
			var frames [][]byte
			offset := 0
			for _, length := range lengths {
				frames = append(frames, raw[offset:offset+length])
				offset += length
			}
			if offset != len(raw) {
				t.Fatal("frame lengths do not cover capture")
			}
			check(frames)
			candidateStart := bytes.LastIndex(raw, []byte("CAPACITY_REPLY:cap-000000021"))
			if fixture.redraws > 0 {
				candidateStart = len(raw) - lengths[len(lengths)-1]
			}
			for search := candidateStart; search < len(raw); {
				relative := bytes.Index(raw[search:], []byte(replyPrefix))
				if relative < 0 {
					break
				}
				start := search + relative
				end := start + bytes.IndexByte(raw[start:], '\n') + 1
				if end <= start {
					t.Fatal("captured candidate is incomplete")
				}
				for cut := start; cut <= end; cut++ {
					check([][]byte{raw[:cut], raw[cut:]})
				}
				search = end
			}
			chunks := make([][]byte, len(raw))
			for i := range raw {
				chunks[i] = raw[i : i+1]
			}
			check(chunks)
		})
	}
}
func TestVTRejectsDuplicateUnknownAndFalseVisibility(t *testing.T) {
	const reply = "CAPACITY_REPLY:cap-000000001"
	for _, tc := range []struct {
		name, raw string
		completed int
	}{
		{"new-row-duplicate", reply + "\r\n" + reply + "\x1b[K\r\n", 1},
		{"unknown", "CAPACITY_REPLY:cap-000000002\r\n", 0},
		{"erase-left", reply + "\x1b[1K\r\n", 0},
		{"erase-whole", reply + "\x1b[2K\r\n", 0},
		{"changed-rest-of-row", reply + "\r\n\x1b[1;40HX\x1b[H" + reply + "\x1b[K\r\n", 1},
		{"same-row-changed-style", reply + "\r\n\x1b[H\x1b[31m" + reply + "\r\n", 1},
		{"same-row-scroll", "\x1b[30;1H" + reply + "\r\x1b[1G" + reply + "\r\n", 0},
		{"partial-nonce-cursor-overwrite", reply[:len(reply)-1] + "\x1b[C\r\n", 0},
		{"wide-trailing-cells", reply + "界\r\n", 0},
		{"OSC-hidden-reply", "\x1b]0;" + reply + "\r\n\a", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &terminal{replies: make(chan time.Time, 1), errors: make(chan error, 1), pending: "cap-000000001", sequence: 1, tracePath: t.TempDir()}
			decoder := newReplyVT(observer.observeReply)
			defer decoder.close()
			for i := range tc.raw {
				decoder.write([]byte(tc.raw[i : i+1]))
			}
			if terminalError(observer) == nil || observer.completed != tc.completed {
				t.Fatalf("error %v completed %d", terminalError(observer), observer.completed)
			}
		})
	}
}
func TestVTNoReplyUntilCompleteTerminator(t *testing.T) {
	observer := &terminal{replies: make(chan time.Time, 1), errors: make(chan error, 1), pending: "cap-000000001", sequence: 1, tracePath: t.TempDir()}
	decoder := newReplyVT(observer.observeReply)
	defer decoder.close()
	decoder.write([]byte("CAPACITY_REPLY:cap-000000001\x1b[K\r"))
	if observer.completed != 0 {
		t.Fatal("partial response recorded")
	}
	decoder.write([]byte{'\n'})
	if observer.completed != 1 || terminalError(observer) != nil {
		t.Fatal("complete response rejected")
	}
}

func TestVTGeneratedInputIsDrainedAndJoined(t *testing.T) {
	done := make(chan struct{})
	go func() {
		d := newReplyVT(func(replyObservation) {})
		d.write([]byte("\x1b[6n\x1b[c\x1b[>c\x1b]11;?\x1b\\"))
		d.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("VT device reply blocked or drainer not joined")
	}
}
func TestVTObservationLimitFails(t *testing.T) {
	observer := &terminal{errors: make(chan error, 1), redraws: make([]terminalRedraw, maxTerminalObservations)}
	observer.observeReply(replyObservation{Marker: "cap-000000001"})
	if terminalError(observer) == nil {
		t.Fatal("hard observation bound did not fail")
	}
}
