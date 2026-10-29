package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCrashGuardRequiresExplicitRecoveryAfterThreeUnexpectedEnds(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	for attempt := 0; attempt < 3; attempt++ {
		guard, reason := beginCrashGuard(directory, "build-a", false, now.Add(time.Duration(attempt)*time.Second))
		if reason != "" || guard == nil || !guard.active {
			t.Fatalf("attempt %d prematurely blocked: %s", attempt, reason)
		}
		// No finish: simulate the durable state left by process death.
	}
	blocked, reason := beginCrashGuard(directory, "build-a", false, now.Add(3*time.Second))
	if reason != crashLoopReason || blocked.active || len(blocked.record.Failures) != 3 {
		t.Fatalf("three unexpected ends not latched: %#v, %s", blocked, reason)
	}
	if err := blocked.finish(nil, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, reason = beginCrashGuard(directory, "build-a", false, now.Add(time.Hour)); reason != crashLoopReason {
		t.Fatalf("normal diagnostic stop or elapsed time bypassed latch: %s", reason)
	}
	recovered, reason := beginCrashGuard(directory, "build-a", true, now.Add(time.Hour))
	if reason != "" || !recovered.active || len(recovered.record.Failures) != 0 {
		t.Fatalf("explicit recovery did not reset sequence: %#v, %s", recovered, reason)
	}
	if err := recovered.finish(nil, now.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(directory, "startup.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("startup evidence must remain private", err)
	}
}

func TestCrashGuardSeparatesCleanExitBuildAndTimeWindow(t *testing.T) {
	for _, scenario := range []string{"clean", "new-build", "long-running", "old-window", "returned-failure"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			now := time.Now()
			first, reason := beginCrashGuard(directory, "build-a", false, now)
			if reason != "" {
				t.Fatal(reason)
			}
			if scenario == "returned-failure" {
				if err := first.finish(errors.New("startup failed"), now.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			second, reason := beginCrashGuard(directory, "build-a", false, now.Add(2*time.Second))
			if reason != "" || len(second.record.Failures) != 1 {
				t.Fatalf("unexpected end not counted exactly once: %#v %s", second, reason)
			}
			build, next, expected := "build-a", now.Add(3*time.Second), 0
			switch scenario {
			case "clean":
				if err := second.finish(nil, now.Add(2500*time.Millisecond)); err != nil {
					t.Fatal(err)
				}
			case "new-build":
				build = "build-b"
			case "long-running":
				next, expected = now.Add(time.Hour), 1
			case "old-window":
				// The failed attempt ended earlier; waiting does not make it
				// a new crash when a later process begins.
				if err := second.finish(errors.New("failed"), now.Add(2500*time.Millisecond)); err != nil {
					t.Fatal(err)
				}
				next = now.Add(time.Hour)
			case "returned-failure":
				expected = 2
			}
			third, reason := beginCrashGuard(directory, build, false, next)
			if reason != "" || len(third.record.Failures) != expected {
				t.Fatalf("wrong sequence after %s: %#v %s", scenario, third, reason)
			}
		})
	}
}

func TestCrashGuardCorruptionAndClockRollbackFailClosed(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	guard, reason := beginCrashGuard(directory, "build-a", false, now)
	if reason != "" {
		t.Fatal(reason)
	}
	before, err := os.ReadFile(filepath.Join(directory, "startup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, reason := beginCrashGuard(directory, "build-b", true, now.Add(-time.Second)); reason != crashClockReason {
		t.Fatal("clock rollback accepted", reason)
	}
	if err := guard.finish(nil, now.Add(-time.Second)); err == nil {
		t.Fatal("clock rollback cleared an active record")
	}
	after, err := os.ReadFile(filepath.Join(directory, "startup.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("clock rollback changed evidence", err)
	}
	for _, invalid := range []string{`{"schema":1}`, `{"schema":1,"schema":1}`, `truncated`} {
		if err := os.WriteFile(filepath.Join(directory, "startup.json"), []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, reason := beginCrashGuard(directory, "build-b", true, now.Add(time.Second)); reason != crashStateReason {
			t.Fatal("explicit recovery blessed invalid evidence", reason)
		}
		data, err := os.ReadFile(filepath.Join(directory, "startup.json"))
		if err != nil || string(data) != invalid {
			t.Fatal("corrupt evidence overwritten", err)
		}
	}
}

func TestCrashGuardRejectsUnsafeOrUnboundedEvidence(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "public", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "startup.json")
			target := filepath.Join(directory, "preserve")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "public":
				err = os.WriteFile(path, []byte("{}"), 0600)
				if err == nil {
					err = os.Chmod(path, 0644)
				}
			case "oversized":
				err = os.WriteFile(path, []byte(strings.Repeat(" ", 4097)), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan string, 1)
			go func() {
				_, reason := beginCrashGuard(directory, "build-a", true, time.Now())
				done <- reason
			}()
			select {
			case reason := <-done:
				if reason != crashStateReason {
					t.Fatalf("accepted %s: %s", kind, reason)
				}
			case <-time.After(time.Second):
				if kind == "fifo" {
					peer, _ := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
					if peer != nil {
						peer.Close()
					}
				}
				t.Fatal("startup evidence read blocked")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "untouched" {
				t.Fatal("unsafe startup evidence changed another file", err)
			}
		})
	}
}
