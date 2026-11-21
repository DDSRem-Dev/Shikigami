//go:build linux

package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOrdinaryChromeWindowRequiresCurrentPIDAndOfficialClass(t *testing.T) {
	for _, test := range []struct {
		name, properties string
		ready            bool
	}{
		{"ready", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\n", true},
		{"actual-profile-resource", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome (/private/profile/chrome)\", \"Google-chrome\"\n", true},
		{"escaped-resource", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome (/private/a\\\"b\\\\c/chrome)\", \"Google-chrome\"\n", true},
		{"wrong-pid", "_NET_WM_PID(CARDINAL) = 124\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\n", false},
		{"wrong-class", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"xterm\", \"XTerm\"\n", false},
		{"missing-class", "_NET_WM_PID(CARDINAL) = 123\n", false},
		{"duplicate-pid", "_NET_WM_PID(CARDINAL) = 123\n_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\n", false},
		{"duplicate-class", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\n", false},
		{"unexpected-property", "_NET_WM_PID(CARDINAL) = 123\nWM_CLASS(STRING) = \"google-chrome\", \"Google-chrome\"\n_UNEXPECTED = other\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := 0
			ready, err := probeChromeWindow(t.Context(), 123, func(_ context.Context, args ...string) ([]byte, error) {
				queries++
				switch strings.Join(args, " ") {
				case "-root _NET_CLIENT_LIST":
					return []byte("_NET_CLIENT_LIST(WINDOW): window id # 0x400003\n"), nil
				case "-id 0x400003 _NET_WM_PID WM_CLASS":
					return []byte(test.properties), nil
				default:
					t.Fatalf("unexpected/private-page property query: %q", args)
					return nil, errors.New("unexpected query")
				}
			})
			if err != nil || ready != test.ready || queries != 2 {
				t.Fatalf("window evidence incorrectly classified: %v %v (%d queries)", ready, err, queries)
			}
		})
	}
}

func TestOrdinaryWindowProbeBoundsAndMissingDependency(t *testing.T) {
	for _, value := range []string{
		"_NET_CLIENT_LIST(WINDOW): window id # 0x1;other",
		"_NET_CLIENT_LIST(WINDOW): window id # " + strings.Repeat("0x1,", maxReadyWindows) + "0x2",
		strings.Repeat("x", maxWindowProperties+1),
	} {
		if _, err := managedWindowIDs([]byte(value)); err == nil {
			t.Fatal("accepted malformed or unbounded window list")
		}
	}
	for _, value := range []string{"_NET_CLIENT_LIST:  no such atom on any window.", "_NET_CLIENT_LIST:  not found.", "_NET_CLIENT_LIST(WINDOW): window id #"} {
		ids, err := managedWindowIDs([]byte(value))
		if err != nil || len(ids) != 0 {
			t.Fatal("missing initial window should remain pending", ids, err)
		}
	}
	var output windowOutput
	data := []byte(strings.Repeat("x", maxWindowProperties+1))
	if n, err := output.Write(data); n != len(data) || err != nil || !output.overflow || output.Len() != maxWindowProperties {
		t.Fatal("property capture is not bounded")
	}
	query, err := newWindowQuery(nil, func(path string) error {
		if path != "/usr/bin/xprop" {
			t.Fatalf("untrusted probe executable: %s", path)
		}
		return errors.New("private installation details")
	})
	if query != nil || err == nil || !strings.Contains(err.Error(), "administrator browser setup") || strings.Contains(err.Error(), "private installation") {
		t.Fatal("missing dependency was not safely explained", err)
	}
}

func TestOrdinaryWindowReadinessExitAndCancellationCannotBecomeReady(t *testing.T) {
	process := exitedTestChild(t, childChrome, "exit 0")
	err := awaitWindowProbe(t.Context(), []*child{process}, func(context.Context) (bool, error) {
		t.Fatal("queried windows after Chrome exited")
		return true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "chrome exited normally") {
		t.Fatal("exited Chrome became ready", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	err = awaitWindowProbe(ctx, nil, func(context.Context) (bool, error) {
		cancel()
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled observation became ready", err)
	}
	deadline, done := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer done()
	err = awaitWindowProbe(deadline, nil, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing window ignored total deadline", err)
	}
	if err := awaitWindowProbe(t.Context(), nil, func(context.Context) (bool, error) { return true, nil }); err != nil {
		t.Fatal("verified window did not become ready", err)
	}
}
