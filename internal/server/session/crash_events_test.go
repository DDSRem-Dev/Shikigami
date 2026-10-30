package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
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
			abnormal := command == "exit 7" || command == "kill -TERM $$"
			knownExit, unknownExit := false, false
			verifyCrash := func(frame *pb.TerminalOutput) {
				t.Helper()
				if !abnormal || knownExit {
					t.Fatalf("unexpected repeated/intentional crash: %v", frame)
				}
				if frame.SessionId != value.Id || frame.AttachmentId != sub.ID {
					t.Fatal("crash event lost attachment scope")
				}
				data := frame.GetEvent().Data
				if command == "exit 7" && (data["exit_code"] != "7" || data["exit_signal"] != "") {
					t.Fatal(frame)
				}
				if command == "kill -TERM $$" && (data["exit_signal"] != "15" || data["exit_code"] != "") {
					t.Fatal(frame)
				}
				knownExit = true
				t.Logf("verified sequenced crash from tmux cause: %v", data)
			}
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
						unknownExit = true
						if event.Data["exit_code"] != "" || event.Data["exit_signal"] != "" {
							t.Fatal("unknown observation invented an exit cause")
						}
						observed, err := m.tmux.Inspect(ctx, value.Id)
						if err != nil || !observed.Dead {
							t.Fatalf("unknown event without a verified dead pane: %+v %v", observed, err)
						}
						if observed.ExitStatus != nil || observed.ExitSignal != 0 {
							continue
						} // later observation can upgrade it
						t.Log("exit cause is currently unknown; a later observed cause may upgrade this event")
						break
					}
					if frame.GetEvent().GetType() != pb.SessionEvent_CLAUDE_CRASHED {
						continue
					}
					verifyCrash(frame)
					break
				}
				logs, err := m.GetLogs(ctx, &pb.SessionLogsRequest{SessionId: value.Id})
				if err != nil || logs.State != "available" || !strings.Contains(logs.Text, "RETAINED_CRASH_DIAGNOSTIC") {
					t.Fatalf("crash diagnostics: %v %v", logs, err)
				}
			}
			// A previously unknown cause may become known on a later sample.
			// Only that first crash is allowed; repeated samples, normal exits
			// and explicit Stop must never create another crash event.
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
				if frame.Sequence <= last {
					t.Fatal("exit observation broke attachment ordering")
				}
				last = frame.Sequence
				if frame.GetEvent().GetType() == pb.SessionEvent_CLAUDE_CRASHED {
					if !unknownExit {
						t.Fatalf("unexpected crash without a pending unknown observation: %v", frame)
					}
					verifyCrash(frame)
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
