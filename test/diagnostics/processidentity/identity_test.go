package processidentity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	process "github.com/shirou/gopsutil/v4/process"
)

func ownedSleep(t *testing.T) *exec.Cmd {
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
func reap(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
}
func inspectTest(t *testing.T, pid int) Identity {
	t.Helper()
	value, err := (Inspector{}).Inspect(t.Context(), int32(pid))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func nativeProcess(ctx context.Context, pid int32) (processHandle, error) {
	return process.NewProcessWithContext(ctx, pid)
}

type failingProcess struct {
	processHandle
	exeError    error
	uids        []uint32
	birthDelta  int64
	statusError error
}

func (p failingProcess) StatusWithContext(ctx context.Context) ([]string, error) {
	if p.statusError != nil {
		return nil, p.statusError
	}
	return p.processHandle.StatusWithContext(ctx)
}
func (p failingProcess) ExeWithContext(ctx context.Context) (string, error) {
	if p.exeError != nil {
		return "", p.exeError
	}
	return p.processHandle.ExeWithContext(ctx)
}
func (p failingProcess) UidsWithContext(ctx context.Context) ([]uint32, error) {
	if p.uids != nil {
		return p.uids, nil
	}
	return p.processHandle.UidsWithContext(ctx)
}
func (p failingProcess) CreateTimeWithContext(ctx context.Context) (int64, error) {
	v, e := p.processHandle.CreateTimeWithContext(ctx)
	return v + p.birthDelta, e
}

func TestFindChildSkipsReapedEnumerationEntry(t *testing.T) {
	parent := inspectTest(t, os.Getpid())
	worker := ownedSleep(t)
	want := inspectTest(t, worker.Process.Pid)
	// Keep a distinct executable alive until after Children() returned its PID.
	other := exec.Command("sh", "-c", "read line")
	stdin, err := other.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if other.ProcessState == nil {
			_ = other.Process.Kill()
			_ = other.Wait()
		}
	})
	var seen bool
	inspector := Inspector{Observe: func(e Event) {
		if e.PID == int32(other.Process.Pid) && e.Phase == "candidate" {
			seen = true
			reap(t, other)
		}
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	got, err := inspector.FindChild(ctx, parent, want.Executable, want.SHA256)
	if err != nil || got != want || !seen {
		t.Fatalf("reaped unrelated child blocked discovery: got=%+v err=%v observed=%v", got, err, seen)
	}
}

func TestFindChildRetriesCandidateDisappearingDuringInspection(t *testing.T) {
	for _, phase := range []string{"executable", "recheck", "select"} {
		t.Run(phase, func(t *testing.T) {
			parent := inspectTest(t, os.Getpid())
			first := ownedSleep(t)
			want := inspectTest(t, first.Process.Pid)
			var once sync.Once
			var replacement *exec.Cmd
			var events []Event
			inspector := Inspector{Observe: func(e Event) {
				events = append(events, e)
				if e.PID == int32(first.Process.Pid) && e.Phase == phase {
					once.Do(func() { reap(t, first); replacement = ownedSleep(t) })
				}
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			got, err := inspector.FindChild(ctx, parent, want.Executable, want.SHA256)
			if err != nil || replacement == nil || got.PID != int32(replacement.Process.Pid) || got.PID == want.PID {
				t.Fatalf("replacement identity: %+v %v", got, err)
			}
			found := false
			for _, e := range events {
				if e.PID == want.PID && e.Error != "" {
					found = true
				}
			}
			if !found {
				t.Fatal("disappeared candidate evidence missing")
			}
		})
	}
}

func TestFindChildDoesNotHideUnknownOrChangedIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure error
		uids    []uint32
		birth   bool
		badHash bool
	}{
		{name: "permission denied", failure: os.ErrPermission},
		{name: "missing executable while process remains alive", failure: os.ErrNotExist},
		{name: "ambiguous Darwin executable error while alive", failure: errors.New("unknown error: proc_pidpath returned 0")},
		{name: "unknown executable failure", failure: errors.New("unknown process inspection failure")},
		{name: "wrong UID", uids: []uint32{uint32(os.Getuid() + 1)}},
		{name: "birth changed while observing", birth: true},
		{name: "wrong executable hash", badHash: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := inspectTest(t, os.Getpid())
			worker := ownedSleep(t)
			want := inspectTest(t, worker.Process.Pid)
			opens := 0
			inspector := Inspector{open: func(ctx context.Context, pid int32) (processHandle, error) {
				p, err := nativeProcess(ctx, pid)
				if err != nil || pid != want.PID {
					return p, err
				}
				opens++
				wrapped := failingProcess{processHandle: p, exeError: test.failure, uids: test.uids}
				if test.birth && opens > 1 {
					wrapped.birthDelta = 1
				}
				return wrapped, nil
			}}
			hash := want.SHA256
			if test.badHash {
				hash = fmt.Sprintf("%x", sha256.Sum256([]byte("wrong artifact")))
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := inspector.FindChild(ctx, parent, want.Executable, hash)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrDisappeared) {
				t.Fatalf("unknown/mismatched identity retried or accepted: %v", err)
			}
			if test.failure == os.ErrPermission && !errors.Is(err, os.ErrPermission) {
				t.Fatalf("permission error lost: %v", err)
			}
		})
	}
}

func TestFindChildRejectsAmbiguityAndBoundsMissingWorker(t *testing.T) {
	parent := inspectTest(t, os.Getpid())
	first := ownedSleep(t)
	want := inspectTest(t, first.Process.Pid)
	second := ownedSleep(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := (Inspector{}).FindChild(ctx, parent, want.Executable, want.SHA256); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("two owned candidates not rejected immediately: %v", err)
	}
	reap(t, first)
	reap(t, second)
	short, stop := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer stop()
	if _, err := (Inspector{}).FindChild(short, parent, want.Executable, want.SHA256); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing candidate not bounded: %v", err)
	}
	if _, err := (Inspector{}).FindChild(context.Background(), parent, want.Executable, want.SHA256); err == nil {
		t.Fatal("accepted unbounded discovery")
	}
	wrong := parent
	wrong.Created++
	if _, err := (Inspector{}).FindChild(ctx, wrong, want.Executable, want.SHA256); err == nil {
		t.Fatal("accepted changed parent birth")
	}
}

func TestAliveDistinguishesExitFromInspectionFailure(t *testing.T) {
	worker := ownedSleep(t)
	want := inspectTest(t, worker.Process.Pid)
	inspector := Inspector{open: func(context.Context, int32) (processHandle, error) { return nil, os.ErrPermission }}
	if alive, err := inspector.Alive(t.Context(), want); alive || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("permission error became retirement: %v %v", alive, err)
	}
	statusError := exec.Command("sh", "-c", "exit 1").Run()
	if statusError == nil {
		t.Fatal("expected ps-style process status error")
	}
	liveStatus := Inspector{open: func(ctx context.Context, pid int32) (processHandle, error) {
		p, err := nativeProcess(ctx, pid)
		if err != nil {
			return p, err
		}
		return failingProcess{processHandle: p, statusError: statusError}, nil
	}}
	if alive, err := liveStatus.Alive(t.Context(), want); alive || err == nil {
		t.Fatalf("live process ps failure became retirement: %v %v", alive, err)
	}
	reap(t, worker)
	if alive, err := (Inspector{}).Alive(t.Context(), want); alive || err != nil {
		t.Fatalf("reaped owned process: %v %v", alive, err)
	}
}

func TestInspectRecordsCanonicalExecutableHashAndBirth(t *testing.T) {
	worker := ownedSleep(t)
	value := inspectTest(t, worker.Process.Pid)
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if value.PID != int32(worker.Process.Pid) || value.Parent != int32(os.Getpid()) || value.UID != uint32(os.Getuid()) || value.Created <= 0 || value.Executable != path || value.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("incomplete owned identity: %+v", value)
	}
}
