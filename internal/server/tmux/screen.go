package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxScreenBytes = 128 << 10

type Screen struct {
	Text       string
	Cols, Rows uint32
	Truncated  bool
}
type boundedCapture struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *boundedCapture) Len() int       { return w.buffer.Len() }
func (w *boundedCapture) String() string { return w.buffer.String() }

func (w *boundedCapture) Write(data []byte) (int, error) {
	n := len(data)
	keep := min(n, max(0, w.limit-w.Len()))
	_, _ = w.buffer.Write(data[:keep])
	if keep < n {
		w.truncated = true
	}
	return n, nil
}
func (c *Client) dimensions(ctx context.Context, pane string) (uint32, uint32, error) {
	data, err := c.run(ctx, "display-message", "-p", "-t", pane, "#{pane_width}|#{pane_height}")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid tmux screen dimensions")
	}
	cols, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil || cols == 0 {
		return 0, 0, errors.New("invalid tmux screen width")
	}
	rows, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || rows == 0 {
		return 0, 0, errors.New("invalid tmux screen height")
	}
	return uint32(cols), uint32(rows), nil
}

// CaptureScreen reads only the current visible grid. It never supplies -S/-E
// history ranges, changes pane size, sends input, or attaches a controller.
func (c *Client) CaptureScreen(ctx context.Context, id, expectedPane, expectedWindow string) (Screen, error) {
	before, err := c.Inspect(ctx, id)
	if err != nil {
		return Screen{}, err
	}
	if before.PaneID != expectedPane || before.WindowID != expectedWindow {
		return Screen{}, errors.New("managed screen identity changed")
	}
	cols, rows, err := c.dimensions(ctx, before.PaneID)
	if err != nil {
		return Screen{}, err
	}
	command := c.command(ctx, "capture-pane", "-p", "-t", before.PaneID)
	output := &boundedCapture{limit: MaxScreenBytes}
	diagnostic := &boundedCapture{limit: 4096}
	command.Stdout, command.Stderr = output, diagnostic
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Screen{}, ctx.Err()
		}
		return Screen{}, fmt.Errorf("tmux current screen capture failed: %w", err)
	}
	after, err := c.Inspect(ctx, id)
	if err != nil {
		return Screen{}, err
	}
	nextCols, nextRows, err := c.dimensions(ctx, before.PaneID)
	if err != nil {
		return Screen{}, err
	}
	if after.PaneID != before.PaneID || after.WindowID != before.WindowID || after.TmuxSessionID != before.TmuxSessionID || after.PID != before.PID || nextCols != cols || nextRows != rows {
		return Screen{}, errors.New("managed screen changed during capture; retry")
	}
	data := strings.ToValidUTF8(output.String(), "�")
	if len(data) > MaxScreenBytes {
		data = data[:MaxScreenBytes]
		for !utf8.ValidString(data) {
			data = data[:len(data)-1]
		}
		output.truncated = true
	}
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, data)
	return Screen{Text: text, Cols: cols, Rows: rows, Truncated: output.truncated}, nil
}
