package tmux

import (
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
			if !pane.Dead || pane.PID != 0 || pane.TmuxSessionID != "$3" || pane.WindowID != "@1" || pane.PaneID != "%2" || pane.AbnormalExit() != tc.abnormal {
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

func TestMissingLivePIDAndMalformedDeadPIDRemainInvalid(t *testing.T) {
	for _, line := range []string{
		"shiki-sess_live|@1|%2||0||$3|",
		"shiki-sess_dead|@1|%2|not-a-pid|1|17|$3|",
	} {
		if _, err := parsePane(line); err == nil {
			t.Fatal("missing live or malformed dead PID was accepted")
		}
	}
}
