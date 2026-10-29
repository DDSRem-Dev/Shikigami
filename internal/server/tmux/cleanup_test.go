package tmux

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeleteDeadServerGuardAndFailureResult(t *testing.T) {
	client := testClient(t)
	for _, mode := range []string{"live", "identity", "extra-window-at-guard", "kill-error", "dead"} {
		t.Run(mode, func(t *testing.T) {
			id := "sess_cleanup_" + rand.Text()
			argv := []string{"/bin/true"}
			if mode == "live" {
				argv = []string{"/bin/sleep", "30"}
			}
			pane, err := client.Create(t.Context(), id, t.TempDir(), argv)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Delete(context.Background(), id)
			if mode != "live" {
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					pane, err = client.Inspect(t.Context(), id)
					if err == nil && pane.Dead {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !pane.Dead {
					t.Fatal("expected retained dead pane")
				}
			}
			expected := pane
			if mode == "identity" {
				expected.PaneID = "%999999"
			}
			use := *client
			if mode == "extra-window-at-guard" || mode == "kill-error" {
				wrapper := filepath.Join(t.TempDir(), "tmux-wrapper")
				action := "echo SHIKI_CLEANED; exit 1"
				if mode == "extra-window-at-guard" {
					action = "'" + strings.ReplaceAll(client.binary, "'", "'\\''") + "' -L '" + client.socket + "' new-window -d -t '" + pane.TmuxSessionID + "' /bin/sleep 30"
				}
				script := "#!/bin/sh\ncase \" $* \" in *' if-shell '*) " + action + ";; esac\nexec '" + strings.ReplaceAll(client.binary, "'", "'\\''") + "' \"$@\"\n"
				if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				use.binary = wrapper
			}
			deleted, err := use.DeleteDead(t.Context(), id, expected)
			if mode == "dead" {
				if err != nil || !deleted {
					t.Fatalf("dead cleanup failed: %v %v", deleted, err)
				}
			} else {
				if deleted {
					t.Fatal("unsafe guard accepted deletion")
				}
				if mode == "kill-error" && err == nil {
					t.Fatal("early output hid command failure")
				}
				panes, listErr := client.List(t.Context())
				if listErr != nil {
					t.Fatal(listErr)
				}
				exists := false
				for _, p := range panes {
					exists = exists || p.SessionID == id
				}
				if !exists {
					t.Fatal("protected session removed")
				}
				// Extra windows make Inspect intentionally reject ambiguity; remove this
				// exact test-created tmux session with the trusted ID for test cleanup.
				if mode == "extra-window-at-guard" {
					if _, err := client.run(t.Context(), "kill-session", "-t", pane.TmuxSessionID); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
