package session

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestObservedExitEventsWithoutAutomaticRestart(t *testing.T) {
	for _, action := range []string{"exit 7", "kill -TERM $$", "exit 0", "intentional-stop"} {
		t.Run(action, func(t *testing.T) {
			command := action
			if action == "intentional-stop" {
				command = "exit 8"
			}
			fifo := filepath.Join(t.TempDir(), "exit-trigger")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			m := restartFixture(t, []string{"/bin/sh", "-c", "printf 'WAITING_FOR_TEST\\n'; read trigger < \"$1\"; printf 'RETAINED_CRASH_DIAGNOSTIC\\n'; " + command, "shiki", fifo}, RestartPolicy{})
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			value, err := m.Create(ctx, &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "crash-event", RequestId: "crash-event"})
			if err != nil {
				t.Fatal(err)
			}
			bridge, err := m.Terminal(value.Id)
			if err != nil {
				t.Fatal(err)
			}
			sub, err := bridge.Open(ctx, &pb.TerminalOpen{SessionId: value.Id, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}, true)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			var last uint64
			var output strings.Builder
			for !strings.Contains(output.String(), "WAITING_FOR_TEST") {
				frame, err := sub.Next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				last = frame.Sequence
				output.Write(frame.GetData())
			}
			if action == "intentional-stop" {
				if _, err := m.StopClaude(ctx, &pb.SessionActionRequest{SessionId: value.Id}); err != nil {
					t.Fatal(err)
				}
			} else {
				// Release the process through a FIFO only after attachment redraw.
				// Exit observation is independent of terminal input timing.
				trigger, err := os.OpenFile(fifo, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := trigger.WriteString("exit-now\n")
				closeErr := trigger.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatal(writeErr, closeErr)
				}
			}
			abnormal := action == "exit 7" || action == "kill -TERM $$"
			knownExit := false
			if abnormal {
				for {
					frame, err := sub.Next(ctx)
					if err != nil {
						probe, stop := context.WithTimeout(context.Background(), time.Second)
						pane, paneErr := m.tmux.Inspect(probe, value.Id)
						stop()
						state, stateErr := m.store.View()
						var persisted pb.Session
						if stateErr == nil {
							stateErr = state.Get("sessions", value.Id, &persisted)
						}
						diagnostic, finish := context.WithTimeout(context.Background(), time.Second)
						capture, captureErr := exec.CommandContext(diagnostic, "tmux", "-L", "shiki-"+strconv.Itoa(os.Getuid()), "capture-pane", "-p", "-t", pane.PaneID).CombinedOutput()
						finish()
						t.Logf("crash wait: pane=%+v paneErr=%v session=%v stateErr=%v screen=%q captureErr=%v", pane, paneErr, &persisted, stateErr, capture, captureErr)
						t.Fatal(err)
					}
					if frame.Sequence <= last {
						t.Fatal("crash event broke attachment ordering")
					}
					last = frame.Sequence
					if event := frame.GetEvent(); event != nil && event.Type == pb.SessionEvent_CLAUDE_STOPPED && event.Data["reason_code"] == "EXIT_REASON_UNKNOWN" {
						observed, err := m.tmux.Inspect(ctx, value.Id)
						if err != nil || !observed.Dead {
							t.Fatalf("unknown event without a verified dead pane: %+v %v", observed, err)
						}
						if observed.ExitStatus != nil || observed.ExitSignal != 0 {
							continue
						} // later observation can upgrade it
						if event.Data["exit_code"] != "" || event.Data["exit_signal"] != "" {
							t.Fatal("unknown observation invented an exit cause")
						}
						t.Log("tmux retained no exit cause; verified the explicit unknown outcome, no fabricated crash")
						break
					}
					if frame.GetEvent().GetType() != pb.SessionEvent_CLAUDE_CRASHED {
						continue
					}
					knownExit = true
					t.Logf("verified sequenced crash from tmux cause: %v", frame.GetEvent().Data)
					if frame.SessionId != value.Id || frame.AttachmentId != sub.ID {
						t.Fatal("crash event lost attachment scope")
					}
					if action == "exit 7" && frame.GetEvent().Data["exit_code"] != "7" {
						t.Fatal(frame)
					}
					if action == "kill -TERM $$" && (frame.GetEvent().Data["exit_signal"] != "15" || frame.GetEvent().Data["exit_code"] != "") {
						t.Fatal(frame)
					}
					break
				}
				logs, err := m.GetLogs(ctx, &pb.SessionLogsRequest{SessionId: value.Id})
				if err != nil || logs.State != "available" || !strings.Contains(logs.Text, "RETAINED_CRASH_DIAGNOSTIC") {
					t.Fatalf("crash diagnostics: %v %v", logs, err)
				}
			}
			// Drive observation repeatedly: neither a repeated dead-pane sample,
			// a normal exit, nor explicit Stop may create another crash event.
			for range 3 {
				m.operations.Lock()
				err = m.restartStep(ctx, map[string]stableProcess{})
				m.operations.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			quiet, stop := context.WithTimeout(ctx, 400*time.Millisecond)
			defer stop()
			for {
				frame, err := sub.Next(quiet)
				if err != nil {
					if quiet.Err() != nil {
						break
					}
					t.Fatal(err)
				}
				if frame.GetEvent().GetType() == pb.SessionEvent_CLAUDE_CRASHED {
					t.Fatalf("unexpected repeated/intentional crash: %v", frame)
				}
			}
			pane, err := m.tmux.Inspect(ctx, value.Id)
			if err != nil {
				t.Fatal(err)
			}
			if abnormal && !pane.Dead {
				t.Fatal("disabled auto restart respawned Claude")
			}
			if knownExit && action == "exit 7" && (pane.ExitStatus == nil || *pane.ExitStatus != 7) {
				t.Fatal(fmt.Sprint(pane))
			}
		})
	}
}
