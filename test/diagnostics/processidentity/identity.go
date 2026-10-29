// Package processidentity observes owned processes for acceptance diagnostics.
// It never signals a process or treats an unknown observation as an exit.
package processidentity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"

	process "github.com/shirou/gopsutil/v4/process"
)

var ErrDisappeared = errors.New("process disappeared during observation")

type Identity struct {
	PID        int32  `json:"pid"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
	UID        uint32 `json:"uid"`
	Parent     int32  `json:"parent_pid"`
	SHA256     string `json:"sha256"`
}

type Event struct {
	PID      int32     `json:"pid"`
	Phase    string    `json:"phase"`
	Identity *Identity `json:"identity,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type processHandle interface {
	CreateTimeWithContext(context.Context) (int64, error)
	ExeWithContext(context.Context) (string, error)
	UidsWithContext(context.Context) ([]uint32, error)
	PpidWithContext(context.Context) (int32, error)
	StatusWithContext(context.Context) ([]string, error)
}

// Inspector emits bounded, structured metadata to the owning fixture's journal.
// Each invocation is read-only; callers supply the existing acceptance deadline.
// The unexported process factory is only replaced by deterministic error tests.
type Inspector struct {
	Observe func(Event)
	open    func(context.Context, int32) (processHandle, error)
}

func (i Inspector) emit(pid int32, phase string, identity *Identity, err error) {
	if i.Observe != nil {
		e := Event{PID: pid, Phase: phase, Identity: identity}
		if err != nil {
			e.Error = err.Error()
		}
		i.Observe(e)
	}
}
func (i Inspector) process(ctx context.Context, pid int32) (processHandle, error) {
	if i.open != nil {
		return i.open(ctx, pid)
	}
	return process.NewProcessWithContext(ctx, pid)
}
func absentError(err error) bool {
	return errors.Is(err, process.ErrorProcessNotRunning) || errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrNotExist)
}

// A missing /proc executable alone does not prove exit: it may be a live
// process with an unreadable/deleted image. Independently check its generation
// and state. Permission and every other unknown error are never retried.
func (i Inspector) classify(ctx context.Context, pid int32, born int64, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// gopsutil's Darwin proc_pidpath wrapper discards errno on a zero return.
	// This exact platform result is ambiguous, never absence on its own.
	var exited *exec.ExitError
	// Darwin gopsutil Status/Ppid invoke ps; a process exiting before ps runs
	// yields exit1 rather than ESRCH. It too requires independent confirmation.
	ambiguousDarwinExit := runtime.GOOS == "darwin" && err != nil && (err.Error() == "unknown error: proc_pidpath returned 0" || (errors.As(err, &exited) && exited.ExitCode() == 1))
	if !absentError(err) && !ambiguousDarwinExit {
		return err
	}
	p, check := i.process(ctx, pid)
	if absentError(check) {
		return fmt.Errorf("%w: %v", ErrDisappeared, err)
	}
	if check != nil {
		return fmt.Errorf("confirm disappearance: %w", check)
	}
	created, check := p.CreateTimeWithContext(ctx)
	if absentError(check) {
		return fmt.Errorf("%w: %v", ErrDisappeared, err)
	}
	if check != nil {
		return fmt.Errorf("confirm birth: %w", check)
	}
	if born != 0 && created != born {
		return errors.New("process identity changed while confirming disappearance")
	}
	states, check := p.StatusWithContext(ctx)
	if absentError(check) || (check == nil && slices.Contains(states, process.Zombie)) {
		return fmt.Errorf("%w: %v", ErrDisappeared, err)
	}
	if check != nil {
		return fmt.Errorf("confirm status: %w", check)
	}
	return fmt.Errorf("live process has incomplete observation: %w", err)
}

func (i Inspector) Inspect(ctx context.Context, pid int32) (value Identity, result error) {
	value.PID = pid
	phase := "open"
	defer func() { i.emit(pid, phase+"_result", &value, result) }()
	step := func(next string) { phase = next; i.emit(pid, next, &value, nil) }
	step("open")
	p, err := i.process(ctx, pid)
	if err != nil {
		return value, i.classify(ctx, pid, 0, err)
	}
	step("birth")
	value.Created, err = p.CreateTimeWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, 0, err)
	}
	step("uids")
	uids, err := p.UidsWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	if len(uids) == 0 {
		return value, errors.New("process UID unavailable")
	}
	value.UID = uids[0]
	for _, uid := range uids {
		if uid != value.UID {
			return value, errors.New("process effective/real/saved UIDs differ")
		}
	}
	step("parent")
	value.Parent, err = p.PpidWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	step("executable")
	value.Executable, err = p.ExeWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	value.Executable, err = filepath.EvalSymlinks(value.Executable)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	step("hash")
	data, err := os.ReadFile(value.Executable)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	value.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	step("recheck")
	// A new gopsutil Process avoids its cached creation timestamp.
	fresh, err := i.process(ctx, pid)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	created, err := fresh.CreateTimeWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	if created != value.Created {
		return value, errors.New("process birth changed while observing")
	}
	executable, err := fresh.ExeWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	currentUIDs, err := fresh.UidsWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	parent, err := fresh.PpidWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	if executable != value.Executable || !slices.Equal(currentUIDs, uids) || parent != value.Parent {
		return value, errors.New("process executable, UID or parent changed while observing")
	}
	states, err := fresh.StatusWithContext(ctx)
	if err != nil {
		return value, i.classify(ctx, pid, value.Created, err)
	}
	if slices.Contains(states, process.Zombie) {
		return value, ErrDisappeared
	}
	return value, nil
}

// Alive is intentionally cheap enough for the 5ms retirement observer. Birth
// mismatch means the original generation is absent, but inspection errors do
// not count as retirement. Full identity/hash validation is done at admission.
func (i Inspector) Alive(ctx context.Context, expected Identity) (bool, error) {
	p, err := i.process(ctx, expected.PID)
	if err != nil {
		err = i.classify(ctx, expected.PID, expected.Created, err)
		if errors.Is(err, ErrDisappeared) {
			return false, nil
		}
		return false, err
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		err = i.classify(ctx, expected.PID, expected.Created, err)
		if errors.Is(err, ErrDisappeared) {
			return false, nil
		}
		return false, err
	}
	if created != expected.Created {
		return false, nil
	}
	states, err := p.StatusWithContext(ctx)
	if err != nil {
		err = i.classify(ctx, expected.PID, expected.Created, err)
		if errors.Is(err, ErrDisappeared) {
			return false, nil
		}
		return false, err
	}
	return !slices.Contains(states, process.Zombie), nil
}

// FindChild returns the fully observed unique child, never a bare PID followed
// by a second, unprotected identity lookup. Disappeared candidates retry only
// inside the caller's deadline; other errors and ambiguous matches fail closed.
func (i Inspector) FindChild(ctx context.Context, parent Identity, executable, hash string) (Identity, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Identity{}, errors.New("process discovery requires a finite deadline")
	}
	want, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Identity{}, err
	}
	if hash == "" {
		return Identity{}, errors.New("expected executable hash is required")
	}
	observed, err := i.Inspect(ctx, parent.PID)
	if err != nil {
		return Identity{}, fmt.Errorf("parent observation: %w", err)
	}
	if observed != parent {
		return Identity{}, errors.New("parent identity changed")
	}
	for {
		if err := ctx.Err(); err != nil {
			return Identity{}, err
		}
		alive, err := i.Alive(ctx, parent)
		if err != nil {
			return Identity{}, fmt.Errorf("parent liveness: %w", err)
		}
		if !alive {
			return Identity{}, errors.New("parent generation disappeared")
		}
		p, err := process.NewProcessWithContext(ctx, parent.PID)
		if err != nil {
			return Identity{}, err
		}
		children, err := p.ChildrenWithContext(ctx)
		if err != nil && !errors.Is(err, process.ErrorNoChildren) {
			return Identity{}, fmt.Errorf("enumerate children: %w", err)
		}
		var matching []Identity
		for _, child := range children {
			i.emit(child.Pid, "candidate", nil, nil)
			value, err := i.Inspect(ctx, child.Pid)
			if errors.Is(err, ErrDisappeared) {
				continue
			}
			if err != nil {
				return Identity{}, fmt.Errorf("candidate %d: %w", child.Pid, err)
			}
			if value.Parent != parent.PID {
				return Identity{}, fmt.Errorf("candidate %d parent changed", child.Pid)
			}
			if value.Executable != want {
				continue
			}
			if value.UID != parent.UID || value.SHA256 != hash {
				return Identity{}, fmt.Errorf("candidate %d UID/hash mismatch", child.Pid)
			}
			matching = append(matching, value)
		}
		if len(matching) > 1 {
			return Identity{}, fmt.Errorf("multiple matching owned workers: %v", matching)
		}
		if len(matching) == 1 {
			i.emit(matching[0].PID, "select", &matching[0], nil)
			value, err := i.Inspect(ctx, matching[0].PID)
			if err != nil && !errors.Is(err, ErrDisappeared) {
				return Identity{}, err
			}
			if err == nil {
				if value != matching[0] {
					return Identity{}, errors.New("candidate identity changed before selection")
				}
				observed, err := i.Inspect(ctx, parent.PID)
				if err != nil {
					return Identity{}, fmt.Errorf("parent recheck: %w", err)
				}
				if observed != parent {
					return Identity{}, errors.New("parent identity changed before selection")
				}
				return value, nil
			}
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Identity{}, ctx.Err()
		case <-timer.C:
		}
	}
}
