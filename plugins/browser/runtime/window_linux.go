//go:build linux

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const xpropBinary = "/usr/bin/xprop"
const maxWindowProperties = 16 << 10
const maxReadyWindows = 128

var windowIDPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{1,8}$`)
var windowPIDPattern = regexp.MustCompile(`^_NET_WM_PID\(CARDINAL\) = ([0-9]+)$`)
var windowClassPattern = regexp.MustCompile(`^WM_CLASS\(STRING\) = "(?:[^"\\]|\\.)*", "([a-zA-Z0-9_-]+)"$`)

type windowOutput struct {
	bytes.Buffer
	overflow bool
}

func (output *windowOutput) Write(data []byte) (int, error) {
	length := len(data)
	remaining := maxWindowProperties - output.Len()
	if length > remaining {
		output.overflow = true
		data = data[:remaining]
	}
	_, _ = output.Buffer.Write(data)
	return length, nil
}

type windowQuery func(context.Context, ...string) ([]byte, error)

func newWindowQuery(environment []string, verify func(string) error) (windowQuery, error) {
	if err := verify(xpropBinary); err != nil {
		return nil, errors.New("ordinary browser readiness requires the trusted x11-utils package; rerun administrator browser setup")
	}
	environment = replaceEnvironment(append([]string(nil), environment...), "LC_ALL", "C")
	return func(parent context.Context, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		process := exec.CommandContext(ctx, xpropBinary, args...)
		process.Env = environment
		var output windowOutput
		process.Stdout, process.Stderr = &output, io.Discard
		err := process.Run()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if output.overflow {
			return nil, errors.New("private X11 property response exceeds the readiness limit")
		}
		if err != nil {
			return nil, errors.New("private X11 property query failed")
		}
		return output.Bytes(), nil
	}, nil
}

func managedWindowIDs(data []byte) ([]string, error) {
	if len(data) > maxWindowProperties {
		return nil, errors.New("private X11 window list exceeds the readiness limit")
	}
	text := strings.TrimSpace(string(data))
	if rest, ok := strings.CutPrefix(text, "_NET_CLIENT_LIST:"); ok {
		if absent := strings.TrimSpace(rest); absent == "not found." || absent == "no such atom on any window." {
			return nil, nil
		}
	}
	list, ok := strings.CutPrefix(text, "_NET_CLIENT_LIST(WINDOW): window id #")
	if !ok {
		return nil, errors.New("private X11 window list has an unexpected format")
	}
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	values := strings.Split(list, ",")
	if len(values) > maxReadyWindows {
		return nil, errors.New("private X11 window count exceeds the readiness limit")
	}
	for index, value := range values {
		value = strings.TrimSpace(value)
		if !windowIDPattern.MatchString(value) {
			return nil, errors.New("private X11 window identifier is invalid")
		}
		values[index] = value
	}
	return values, nil
}

func matchesChromeWindow(data []byte, pid int) bool {
	if len(data) > maxWindowProperties || pid <= 0 {
		return false
	}
	pidMatches, classMatches := false, false
	pidCount, classCount := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if match := windowPIDPattern.FindStringSubmatch(line); match != nil {
			pidCount++
			observed, err := strconv.Atoi(match[1])
			pidMatches = err == nil && observed == pid
			continue
		}
		if match := windowClassPattern.FindStringSubmatch(line); match != nil {
			classCount++
			// res_name includes Chrome's profile path and is not an identity.
			// Match only res_class and the independently checked browser PID.
			classMatches = match[1] == "Google-chrome"
			continue
		}
		return false
	}
	return pidCount == 1 && classCount == 1 && pidMatches && classMatches
}

func probeChromeWindow(ctx context.Context, pid int, query windowQuery) (bool, error) {
	data, err := query(ctx, "-root", "_NET_CLIENT_LIST")
	if err != nil {
		return false, err
	}
	windows, err := managedWindowIDs(data)
	if err != nil {
		return false, err
	}
	for _, window := range windows {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		data, err := query(ctx, "-id", window, "_NET_WM_PID", "WM_CLASS")
		if err != nil {
			// A window can disappear between the root listing and property read.
			// Never infer readiness from a failed or partial observation.
			continue
		}
		if matchesChromeWindow(data, pid) {
			return true, nil
		}
	}
	return false, ctx.Err()
}

func awaitChromeWindow(ctx context.Context, environment []string, pid int, children []*child) error {
	query, err := newWindowQuery(environment, verifyExecutable)
	if err != nil {
		return err
	}
	return awaitWindowProbe(ctx, children, func(ctx context.Context) (bool, error) {
		return probeChromeWindow(ctx, pid, query)
	})
}

func awaitWindowProbe(ctx context.Context, children []*child, probe func(context.Context) (bool, error)) error {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	check := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		for _, process := range children {
			select {
			case <-process.done:
				return process.exit
			default:
			}
		}
		return nil
	}
	for {
		if err := check(); err != nil {
			return err
		}
		ready, err := probe(ctx)
		if stopped := check(); stopped != nil {
			return stopped
		}
		if err != nil {
			return fmt.Errorf("ordinary Chrome window readiness failed: %w", err)
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ordinary Chrome window did not become ready: %w", ctx.Err())
		case <-tick.C:
		}
	}
}
