package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inventoryReply struct {
	command, output string
	failed          bool
}

// Script the exact subprocess diagnostics observed around a last-session
// shutdown. This covers the race without killing or attaching to a real server;
// the real detach/redraw/delete test exercises tmux itself separately.
func TestInventoryConfirmsAbsenceAcrossServerShutdown(t *testing.T) {
	noTarget := inventoryReply{"list-panes", "no current target", true}
	exited := inventoryReply{"list-sessions", "server exited unexpectedly", true}
	missing := inventoryReply{"list-panes", "no server running on /test/owned-socket", true}
	live := inventoryReply{"list-panes", "shiki-sess_lifecycle|@1|%1|123|0||$1|", false}
	for _, test := range []struct {
		name    string
		replies []inventoryReply
		absent  bool
		errText string
	}{
		{"confirm missing after auxiliary exit", []inventoryReply{noTarget, exited, missing}, true, ""},
		{"recover live inventory after auxiliary exit", []inventoryReply{noTarget, exited, live}, false, ""},
		{"persistent auxiliary exit remains error", []inventoryReply{noTarget, exited, noTarget, exited}, false, "server exited unexpectedly"},
		{"permission failure after exit remains error", []inventoryReply{noTarget, exited, {"list-panes", "permission denied", true}}, false, "permission denied"},
		{"empty live inventory confirms absence", []inventoryReply{noTarget, {"list-sessions", "", false}}, true, ""},
		{"missing socket during confirmation", []inventoryReply{noTarget, {"list-sessions", "error connecting to /test/owned-socket (No such file or directory)", true}}, true, ""},
		{"live sessions with missing target remains error", []inventoryReply{noTarget, {"list-sessions", "shiki-sess_lifecycle", false}}, false, "no current target"},
		{"primary exit still confirms absence", []inventoryReply{{"list-panes", "server exited unexpectedly", true}, missing}, true, ""},
		{"permission failure is not retried", []inventoryReply{{"list-panes", "permission denied", true}}, false, "permission denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, trace := inventoryFixture(t, test.replies)
			pane, err := client.Inspect(t.Context(), "sess_lifecycle")
			switch {
			case test.errText != "":
				if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("query failure became absence or lost its cause: %v", err)
				}
			case test.absent:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("confirmed missing session: %v", err)
				}
			default:
				if err != nil || pane.PID != 123 || pane.SessionID != "sess_lifecycle" {
					t.Fatalf("surviving session was lost: %+v %v", pane, err)
				}
			}
			got, readErr := os.ReadFile(trace)
			var want strings.Builder
			for _, reply := range test.replies {
				fmt.Fprintln(&want, reply.command)
			}
			if readErr != nil || string(got) != want.String() {
				t.Fatalf("unexpected/unbounded inventory commands: %q want %q: %v", got, want.String(), readErr)
			}
		})
	}
}

func TestInventoryQueryHonorsCanceledContext(t *testing.T) {
	client, trace := inventoryFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled inventory did not preserve caller error", err)
	}
	if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled query launched a subprocess", err)
	}
}

func inventoryFixture(t *testing.T, replies []inventoryReply) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	path, trace, counter := filepath.Join(dir, "tmux-inventory"), filepath.Join(dir, "commands"), filepath.Join(dir, "counter")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	var script strings.Builder
	fmt.Fprintf(&script, "#!/bin/sh\nset -eu\nn=0\nif [ -f %s ]; then read -r n < %s; fi\nprintf '%%s\\n' \"$6\" >> %s\nprintf '%%s\\n' \"$((n + 1))\" > %s\ncase \"$n\" in\n", quote(counter), quote(counter), quote(trace), quote(counter))
	for index, reply := range replies {
		code := 0
		if reply.failed {
			code = 1
		}
		fmt.Fprintf(&script, "%d) [ \"$6\" = %s ] || exit 99\nprintf '%%s\\n' %s\nexit %d ;;\n", index, quote(reply.command), quote(reply.output), code)
	}
	script.WriteString("*) echo 'unexpected inventory command' >&2; exit 99 ;;\nesac\n")
	if err := os.WriteFile(path, []byte(script.String()), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	client.socket = "fixture-only"
	return client, trace
}
