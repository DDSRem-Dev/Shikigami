package tmux

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxLogBytes = 128 << 10

type Logs struct {
	Text              string
	State, ReasonCode string
	Truncated         bool
	ExitCode          *int32
}

// tailCapture retains the newest bytes even when tmux exceeds the output bound.
type tailCapture struct {
	data      []byte
	limit     int
	truncated bool
}

func (w *tailCapture) Write(data []byte) (int, error) {
	n := len(data)
	if n >= w.limit {
		w.truncated = w.truncated || len(w.data)+n > w.limit
		w.data = append(w.data[:0], data[n-w.limit:]...)
	} else {
		if extra := len(w.data) + n - w.limit; extra > 0 {
			copy(w.data, w.data[extra:])
			w.data = w.data[:len(w.data)-extra]
			w.truncated = true
		}
		w.data = append(w.data, data...)
	}
	return n, nil
}

// CaptureLogs reads the retained main-pane scrollback plus visible grid. It
// changes no terminal state and makes no promise about discarded/alternate
// screen output, previous processes, persistence or byte-exact replay.
func (c *Client) CaptureLogs(ctx context.Context, id, expectedPane, expectedWindow string, maxLines uint32) (Logs, error) {
	if maxLines == 0 || maxLines > 2000 {
		return Logs{}, errors.New("log line limit must be 1..2000")
	}
	unavailable := func(reason string) (Logs, error) {
		if ctx.Err() != nil {
			return Logs{}, ctx.Err()
		}
		return Logs{State: "unavailable", ReasonCode: reason}, nil
	}
	before, err := c.Inspect(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Logs{State: "not_retained", ReasonCode: "TMUX_PANE_NOT_RETAINED"}, nil
	}
	if err != nil {
		return unavailable("TMUX_UNAVAILABLE")
	}
	if before.PaneID != expectedPane || before.WindowID != expectedWindow {
		return unavailable("PANE_IDENTITY_CHANGED")
	}
	history, err := c.run(ctx, "display-message", "-p", "-t", before.PaneID, "#{history_size}")
	if err != nil {
		return unavailable("TMUX_UNAVAILABLE")
	}
	historyLines, err := strconv.ParseUint(strings.TrimSpace(string(history)), 10, 32)
	if err != nil {
		return unavailable("TMUX_HISTORY_UNAVAILABLE")
	}
	command := c.command(ctx, "capture-pane", "-p", "-t", before.PaneID, "-S", "-"+strconv.FormatUint(uint64(maxLines), 10))
	output := &tailCapture{limit: MaxLogBytes, truncated: historyLines > uint64(maxLines)}
	diagnostic := &boundedCapture{limit: 4096}
	command.Stdout, command.Stderr = output, diagnostic
	if err := command.Run(); err != nil {
		return unavailable("TMUX_CAPTURE_UNAVAILABLE")
	}
	after, err := c.Inspect(ctx, id)
	if err != nil {
		return unavailable("TMUX_UNAVAILABLE")
	}
	if after.PaneID != before.PaneID || after.WindowID != before.WindowID || after.TmuxSessionID != before.TmuxSessionID || after.PID != before.PID || after.Dead != before.Dead || after.ExitSignal != before.ExitSignal || after.ExitSignalUnknown != before.ExitSignalUnknown || !sameExit(before.ExitStatus, after.ExitStatus) {
		return unavailable("PANE_CHANGED_DURING_CAPTURE")
	}
	text := strings.ToValidUTF8(string(output.data), "�")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	if len(text) > MaxLogBytes {
		text = text[len(text)-MaxLogBytes:]
		for len(text) > 0 && !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
		output.truncated = true
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > int(maxLines) {
		lines = lines[len(lines)-int(maxLines):]
		output.truncated = true
	}
	result := Logs{Text: strings.Join(lines, "\n"), State: "available", Truncated: output.truncated}
	if strings.TrimSpace(result.Text) == "" {
		result.State, result.ReasonCode = "not_retained", "NO_RETAINED_OUTPUT"
	}
	if after.Dead && after.ExitSignal == 0 && !after.ExitSignalUnknown && after.ExitStatus != nil {
		value := int32(*after.ExitStatus)
		result.ExitCode = &value
	}
	return result, nil
}

func sameExit(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
