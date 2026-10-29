//go:build shiki_load_capacity

package main

import (
	"bytes"
	"fmt"
	"io"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

const replyPrefix = "CAPACITY_REPLY:"

// replyVT observes the synthetic fixture in the same 120x30 display as the
// actual CLI PTY. It never treats a previously seen nonce alone as a redraw.
// A redraw must rewrite the complete raw reply at the same existing cells,
// preserve the whole row (including styles/links), and not scroll on LF.
type replyVT struct {
	screen        *vt.Emulator
	candidate     []byte
	before        []uv.Cell
	x, y          int
	offset, start int64
	emit          func(replyObservation)
	drained       chan struct{}
}
type replyObservation struct {
	Marker                 string
	Raw                    []byte
	Offset                 int64
	Row                    int
	AlreadyVisible, Redraw bool
	Err                    error
}

func newReplyVT(emit func(replyObservation)) *replyVT {
	d := &replyVT{screen: vt.NewEmulator(120, 30), emit: emit, drained: make(chan struct{})}
	// VT device replies are synthetic input, never real CLI input. Drain the
	// private pipe so actual DSR/DA/OSC queries cannot block screen observation.
	go func() { _, _ = io.Copy(io.Discard, d.screen); close(d.drained) }()
	return d
}
func (d *replyVT) close() {
	// Close the pipe first, join its reader, then close the emulator. Its Read
	// checks a plain closed flag, so concurrent Emulator.Close would race.
	need(d.screen.InputPipe().(io.Closer).Close())
	select {
	case <-d.drained:
	case <-time.After(5 * time.Second):
		panic("VT input reader not joined")
	}
	need(d.screen.Close())
}
func (d *replyVT) row(y int) []uv.Cell {
	cells := make([]uv.Cell, 120)
	for x := range cells {
		if c := d.screen.CellAt(x, y); c != nil {
			cells[x] = *c
		} else {
			cells[x] = uv.EmptyCell
		}
	}
	return cells
}
func equalCells(a, b []uv.Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(&b[i]) {
			return false
		}
	}
	return true
}
func rowReply(cells []uv.Cell, text string) bool {
	if len(text) > len(cells) {
		return false
	}
	for i, c := range cells {
		want := " "
		if i < len(text) {
			want = string(text[i])
		}
		if c.Content != want || c.Width != 1 {
			return false
		}
	}
	return true
}
func (d *replyVT) write(data []byte) {
	for _, b := range data {
		// The candidate prefix is assembled across arbitrary reader/frame splits.
		// Capture before its first byte, never after old cells could be overwritten.
		if len(d.candidate) == 0 && b == 'C' {
			pos := d.screen.CursorPosition()
			d.x, d.y = pos.X, pos.Y
			d.before = d.row(pos.Y)
			d.start = d.offset
			d.candidate = []byte{'C'}
		} else if len(d.candidate) > 0 {
			d.candidate = append(d.candidate, b)
		}
		var event *replyObservation
		var beforeLF []uv.Cell
		if len(d.candidate) > 0 {
			if len(d.candidate) <= len(replyPrefix) && !bytes.HasPrefix([]byte(replyPrefix), d.candidate) {
				d.candidate = nil
			} else if b == '\n' {
				raw := append([]byte(nil), d.candidate...)
				marker, err := replyMarker(raw)
				obs := replyObservation{Marker: marker, Raw: raw, Offset: d.start, Row: d.y, Err: err}
				if err == nil {
					text := replyPrefix + marker
					current := d.row(d.y)
					obs.AlreadyVisible = d.x == 0 && rowReply(d.before, text)
					if d.x != 0 || !rowReply(current, text) || d.screen.CursorPosition().Y != d.y {
						obs.Err = fmt.Errorf("complete raw reply is not visible at its original cells")
					} else {
						obs.Redraw = obs.AlreadyVisible && equalCells(d.before, current)
						if obs.Redraw {
							// LF may scroll a terminal scroll-region; compare every cell across
							// LF, not merely an assumed bottom row or the raw cursor movement.
							beforeLF = make([]uv.Cell, 0, 3600)
							for y := 0; y < 30; y++ {
								beforeLF = append(beforeLF, d.row(y)...)
							}
						}
					}
				}
				event = &obs
				d.candidate = nil
			} else if len(d.candidate) > 1024 {
				event = &replyObservation{Raw: append([]byte(nil), d.candidate...), Offset: d.start, Row: d.y, Err: fmt.Errorf("fixture reply exceeds bounded line")}
				d.candidate = nil
			}
		}
		_, err := d.screen.Write([]byte{b})
		if err != nil {
			panic(err)
		}
		d.offset++
		if event != nil {
			if event.Redraw {
				for y := 0; y < 30; y++ {
					if !equalCells(beforeLF[y*120:(y+1)*120], d.row(y)) {
						event.Redraw = false
						break
					}
				}
			}
			d.emit(*event)
		}
	}
}
