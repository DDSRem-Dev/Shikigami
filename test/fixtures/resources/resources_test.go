package resources

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/shirou/gopsutil/v4/process"
)

func ownedChild(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd
}

func childrenSnapshot(t *testing.T, pids ...int) []*process.Process {
	t.Helper()
	parent, err := process.NewProcessWithContext(t.Context(), int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	children, err := parent.ChildrenWithContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		found := false
		for _, child := range children {
			found = found || child.Pid == int32(pid)
		}
		if !found {
			t.Fatal("owned child missing from real parent enumeration", pid)
		}
	}
	return children
}

func childExecutable(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	p, err := process.NewProcessWithContext(t.Context(), int32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	path, err := p.ExeWithContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChildrenSkipsIndependentlyReapedEnumerationEntry(t *testing.T) {
	failures := []error{os.ErrNotExist, syscall.ESRCH, process.ErrorProcessNotRunning}
	if runtime.GOOS == "darwin" {
		failures = append(failures, errors.New("unknown error: proc_pidpath returned 0"))
	}
	for _, failure := range failures {
		t.Run(failure.Error(), func(t *testing.T) {
			worker, shortLived := ownedChild(t), ownedChild(t)
			want := childExecutable(t, worker)
			children := childrenSnapshot(t, worker.Process.Pid, shortLived.Process.Pid)
			observed := false
			lookup := func(ctx context.Context, p *process.Process) (string, error) {
				if p.Pid == int32(shortLived.Process.Pid) {
					observed = true
					if err := shortLived.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					_ = shortLived.Wait()
					return "", failure
				}
				return p.ExeWithContext(ctx)
			}
			got, err := matchingChildren(t.Context(), children, want, 1, lookup)
			if err != nil || !observed || len(got) != 1 || got[0] != worker.Process.Pid {
				t.Fatalf("reaped child blocked real worker selection: got=%v observed=%v err=%v", got, observed, err)
			}
		})
	}
}

func TestChildrenDoesNotHideLiveProcessInspectionFailures(t *testing.T) {
	worker := ownedChild(t)
	want := childExecutable(t, worker)
	children := childrenSnapshot(t, worker.Process.Pid)
	for _, failure := range []error{os.ErrPermission, os.ErrNotExist, syscall.ESRCH, process.ErrorProcessNotRunning,
		errors.New("unknown error: proc_pidpath returned 0"), errors.New("unknown executable failure")} {
		t.Run(failure.Error(), func(t *testing.T) {
			lookup := func(ctx context.Context, p *process.Process) (string, error) {
				if p.Pid == int32(worker.Process.Pid) {
					return "", failure
				}
				return p.ExeWithContext(ctx)
			}
			got, err := matchingChildren(t.Context(), children, want, 1, lookup)
			if len(got) != 0 || !errors.Is(err, failure) {
				t.Fatalf("live inspection error hidden: got=%v err=%v", got, err)
			}
		})
	}
}

func TestChildrenPreservesExpectedCountAndCallerCancellation(t *testing.T) {
	worker := ownedChild(t)
	want := childExecutable(t, worker)
	children := childrenSnapshot(t, worker.Process.Pid)
	lookup := func(ctx context.Context, p *process.Process) (string, error) { return p.ExeWithContext(ctx) }
	if _, err := matchingChildren(t.Context(), children, want, 2, lookup); err == nil || !strings.Contains(err.Error(), "expected 2 direct workers") {
		t.Fatal("missing worker count accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := matchingChildren(ctx, children, want, 1, func(context.Context, *process.Process) (string, error) {
		return "", os.ErrNotExist
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation treated as process exit", err)
	}
}
