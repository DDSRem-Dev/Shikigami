package tmux

import (
	"os"
	"testing"
)

func TestDeadPaneMayOmitPIDWithoutLosingExitEvidence(t *testing.T) {
	// tmux 3.8 format_cb_pane_pid omits the PID once wp->fd == -1.
	// A retained dead pane still has exact tmux identities and exit evidence.
	for _, tc := range []struct {
		name, status, signal string
		abnormal             bool
	}{
		{"normal exit", "0", "", false},
		{"failed exit", "17", "", true},
		{"signal", "", "term", true},
		{"unknown exit", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane, err := parsePane("shiki-sess_retained|@1|%2||1|" + tc.status + "|$3|" + tc.signal)
			if err != nil {
				t.Fatal("dead pane PID is optional in tmux 3.8", err)
			}
			if !pane.Dead || pane.ExitPending || pane.PID != 0 || pane.TmuxSessionID != "$3" || pane.WindowID != "@1" || pane.PaneID != "%2" || pane.AbnormalExit() != tc.abnormal {
				t.Fatalf("dead pane lost identity/exit semantics: %+v", pane)
			}
			if tc.status == "" && pane.ExitStatus != nil {
				t.Fatal("missing exit status became a known result")
			}
			if tc.status == "17" && (pane.ExitStatus == nil || *pane.ExitStatus != 17) {
				t.Fatal("observed failed exit was lost")
			}
			if tc.signal == "term" && pane.ExitSignal != 15 {
				t.Fatal("observed signal was lost")
			}
		})
	}
}

func TestPendingExitCannotAuthorizeDestructiveActions(t *testing.T) {
	exit := 17
	if (Pane{Dead: true, ExitPending: true, ExitStatus: &exit}).AbnormalExit() {
		t.Fatal("pending exit accepted unconfirmed evidence for restart")
	}
	line := "shiki-sess_lifecycle|@1|%1||0|17|$1|term"
	pane, err := parsePane(line)
	if err != nil {
		t.Fatal(err)
	}
	client, trace := inventoryFixture(t, []inventoryReply{{"list-panes", line, false}, {"list-panes", line, false}})
	if err := client.RespawnDead(t.Context(), pane.SessionID, pane.PaneID, []string{"/bin/false"}); err == nil {
		t.Fatal("pending exit allowed automatic restart")
	}
	if deleted, err := client.DeleteDead(t.Context(), pane.SessionID, pane); err != nil || deleted {
		t.Fatal("pending exit entered the deletion path", deleted, err)
	}
	commands, err := os.ReadFile(trace)
	if err != nil || string(commands) != "list-panes\nlist-panes\n" {
		t.Fatal("pending exit launched a mutating tmux command")
	}
}

func TestClosedPaneBeforeWaitStatusRemainsUnknown(t *testing.T) {
	// tmux 3.8 closes wp->fd before waitpid sets PANE_STATUSREADY.
	// pane_pid is then empty while pane_dead still reports 0. This is not
	// a live process or proof of an abnormal exit, even if other fields
	// contain an unconfirmed result from a different observation.
	for _, tc := range []struct{ name, status, signal string }{
		{"waiting for status", "", ""},
		{"unconfirmed status", "17", ""},
		{"unconfirmed signal", "", "term"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane, err := parsePane("shiki-sess_closing|@1|%2||0|" + tc.status + "|$3|" + tc.signal)
			if err != nil {
				t.Fatal("closed pane awaiting status was rejected", err)
			}
			if !pane.Dead || !pane.ExitPending || pane.PID != 0 || pane.ExitStatus != nil || pane.ExitSignal != 0 || pane.ExitSignalUnknown || pane.AbnormalExit() {
				t.Fatalf("closed pane invented a live process or exit evidence: %+v", pane)
			}
			if pane.TmuxSessionID != "$3" || pane.WindowID != "@1" || pane.PaneID != "%2" {
				t.Fatal("closed pane lost its exact tmux identity")
			}
		})
	}
}

func TestMalformedPanePIDAndDeadFlagRemainInvalid(t *testing.T) {
	for _, line := range []string{
		"shiki-sess_live|@1|%2|not-a-pid|0||$3|",
		"shiki-sess_dead|@1|%2|not-a-pid|1|17|$3|",
		"shiki-sess_unknown|@1|%2||||$3|",
		"shiki-sess_unknown|@1|%2||invalid||$3|",
	} {
		if _, err := parsePane(line); err == nil {
			t.Fatal("malformed PID or dead flag was accepted")
		}
	}
}
