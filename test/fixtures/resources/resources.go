// Package resources measures bounded resource recovery in opt-in process tests.
// It deliberately does not infer subprocess goroutines from host measurements.
package resources

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

var cycles = flag.Int("shiki-resource-cycles", 0, "opt-in bounded resource recovery cycles (8..100)")

func Cycles(t *testing.T) int {
	t.Helper()
	if *cycles != 0 && (*cycles < 8 || *cycles > 100) {
		t.Fatal("shiki-resource-cycles must be 0 (disabled) or 8..100")
	}
	return *cycles
}

type Sample struct {
	FDs        map[int32]int32
	Created    map[int32]int64
	Goroutines int
}

func Capture(t *testing.T, pids ...int) Sample {
	t.Helper()
	s, err := sample(t.Context(), pids)
	if err != nil {
		t.Fatal("required OS resource measurement unavailable:", err)
	}
	return s
}

func sample(ctx context.Context, pids []int) (Sample, error) {
	s := Sample{FDs: map[int32]int32{}, Created: map[int32]int64{}}
	for _, pid := range pids {
		p, err := process.NewProcessWithContext(ctx, int32(pid))
		if err != nil {
			return s, err
		}
		created, err := p.CreateTimeWithContext(ctx)
		if err != nil {
			return s, err
		}
		fds, err := p.NumFDsWithContext(ctx)
		if err != nil {
			return s, err
		}
		s.FDs[p.Pid], s.Created[p.Pid] = fds, created
	}
	s.Goroutines = runtime.NumGoroutine()
	return s, nil
}

// Restored requires three consecutive samples below a fixed allowance after
// warmup. Allowances never grow with the number of exercised operations.
func Restored(t *testing.T, label string, baseline Sample, fdAllowance, goAllowance int) {
	t.Helper()
	pids := make([]int, 0, len(baseline.FDs))
	for pid := range baseline.FDs {
		pids = append(pids, int(pid))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	stable := 0
	var last Sample
	var lastErr error
	for {
		last, lastErr = sample(ctx, pids)
		okay := lastErr == nil && last.Goroutines <= baseline.Goroutines+goAllowance
		for pid, count := range baseline.FDs {
			if last.Created[pid] != baseline.Created[pid] || last.FDs[pid] > count+int32(fdAllowance) {
				okay = false
			}
		}
		if okay {
			stable++
		} else {
			stable = 0
		}
		if stable == 3 {
			t.Logf("resource checkpoint %s: baseline fd=%v goroutines=%d; settled fd=%v goroutines=%d; allowances fd=%d goroutines=%d", label, baseline.FDs, baseline.Goroutines, last.FDs, last.Goroutines, fdAllowance, goAllowance)
			return
		}
		select {
		case <-ctx.Done():
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			t.Fatalf("%s did not recover resources: baseline=%+v last=%+v error=%v\n%s", label, baseline, last, lastErr, stack)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Children selects actual direct subprocesses by their resolved executable,
// never an unrelated process found by a global executable-name search.
func Children(t *testing.T, executable string, expected int) []int {
	t.Helper()
	parent, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	children, err := parent.ChildrenWithContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	var result []int
	for _, child := range children {
		path, err := child.ExeWithContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err == nil && path == want {
			result = append(result, int(child.Pid))
		}
	}
	if len(result) != expected {
		t.Fatal(fmt.Sprintf("expected %d direct workers for %s, got %v", expected, executable, result))
	}
	return result
}

// Reaped rejects both live processes and zombies after the owning supervisor
// closes; an API's stopped status alone is not proof of child reclamation.
func Reaped(t *testing.T, pids ...int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		remaining := []int{}
		for _, pid := range pids {
			if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
				remaining = append(remaining, pid)
			}
		}
		if len(remaining) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("closed supervisor retained live or unreaped child PIDs: %v", remaining)
		case <-time.After(25 * time.Millisecond):
		}
	}
}
