//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	observer "github.com/DDSRem-Dev/Shikigami/test/diagnostics/processidentity"
	processinfo "github.com/shirou/gopsutil/v4/process"
)

const defaultRotationTTL = 12 * time.Hour
const defaultRotationObservation = 12*time.Hour + 30*time.Minute
const defaultRotationRecovery = 2 * time.Minute

type rotationProcessIdentity struct {
	Role       string `json:"role"`
	PID        int    `json:"pid"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
	UID        uint32 `json:"uid"`
	Parent     int32  `json:"parent_pid"`
	SHA256     string `json:"sha256"`
}
type rotationEpoch struct {
	Role, Side, Plugin, Revision           string
	Lower, Ready, LastPresent, FirstAbsent time.Time
	Original, Current                      rotationProcessIdentity
	Socket                                 string
	Replacements                           int
	Unavailable                            bool
	Observer                               *rotationProcessObserver `json:"-"`
}

// A retirement is observed as an interval (last present, first absent]. No
// claim of exact simultaneous retirement is inferred from concurrent enable.
func rotationRetirementAllowed(e rotationEpoch, observed time.Time) error {
	if e.LastPresent.Before(e.Lower.Add(defaultRotationTTL)) {
		return errors.New("retirement interval does not prove liveness through earliest possible expiry")
	}
	if observed.Before(e.Lower.Add(defaultRotationTTL)) {
		return errors.New("worker retired before the earliest possible natural expiry")
	}
	if observed.After(e.Ready.Add(defaultRotationTTL + defaultRotationRecovery)) {
		return errors.New("worker missed bounded natural-expiry recovery window")
	}
	if e.Replacements != 0 {
		return errors.New("unexpected second replacement")
	}
	return nil
}
func rotationNextEpoch(e rotationEpoch, next rotationProcessIdentity, observed time.Time) error {
	if err := rotationRetirementAllowed(e, observed); err != nil {
		return err
	}
	if next.PID == e.Original.PID || next.Created <= e.Original.Created || next.Role != e.Role || next.Executable != e.Original.Executable || next.UID != e.Original.UID || next.Parent != e.Original.Parent || next.SHA256 != e.Original.SHA256 {
		return errors.New("replacement did not establish a fresh same-role process epoch")
	}
	return nil
}
func rotationObserved(value rotationProcessIdentity) observer.Identity {
	return observer.Identity{PID: int32(value.PID), Created: value.Created, Executable: value.Executable, UID: value.UID, Parent: value.Parent, SHA256: value.SHA256}
}
func (h *defaultRotationHarness) inspector(role string) observer.Inspector {
	return observer.Inspector{Observe: func(event observer.Event) {
		h.event("process_observation", map[string]any{"role": role, "observation": event})
	}}
}
func (h *defaultRotationHarness) identity(role string, pid int) rotationProcessIdentity {
	h.t.Helper()
	value, err := h.inspector(role).Inspect(h.ctx, int32(pid))
	if err != nil {
		h.t.Fatal("process identity", role, pid, err)
	}
	if value.UID != uint32(os.Getuid()) {
		h.t.Fatal("process is not owned by acceptance user", role, pid, value.UID)
	}
	return h.recordIdentity(role, value)
}
func (h *defaultRotationHarness) recordIdentity(role string, value observer.Identity) rotationProcessIdentity {
	h.t.Helper()
	identity := rotationProcessIdentity{Role: role, PID: int(value.PID), Created: value.Created, Executable: value.Executable, UID: value.UID, Parent: value.Parent, SHA256: value.SHA256}
	for _, existing := range h.identities {
		if existing == identity {
			return existing
		}
	}
	h.identities = append(h.identities, identity)
	if directory := os.Getenv("SHIKI_RUN_EVIDENCE_DIR"); directory != "" {
		data, err := json.MarshalIndent(map[string]any{"processes": h.identities}, "", "  ")
		if err != nil {
			h.t.Fatal(err)
		}
		temporary := filepath.Join(directory, "processes.json.tmp")
		if err := os.WriteFile(temporary, data, 0600); err != nil {
			h.t.Fatal(err)
		}
		if err := os.Rename(temporary, filepath.Join(directory, "processes.json")); err != nil {
			h.t.Fatal(err)
		}
	}
	h.event("process", identity)
	return identity
}
func (h *defaultRotationHarness) findWorker(side, id, hash string, deadline time.Time) rotationProcessIdentity {
	h.t.Helper()
	role := side + "-" + id
	var parent observer.Identity
	for _, known := range h.identities {
		if known.PID == h.parents[side] {
			parent = rotationObserved(known)
			break
		}
	}
	if parent.PID == 0 {
		h.t.Fatal("owned parent identity missing", side)
	}
	path := filepath.Join(h.bin, "shiki-plugin-"+id)
	if hash == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			h.t.Fatal(err)
		}
		hash = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	ctx, cancel := context.WithDeadline(h.ctx, deadline)
	defer cancel()
	next, err := h.inspector(role).FindChild(ctx, parent, path, hash)
	if err != nil {
		h.t.Fatal("worker discovery", role, err)
	}
	return h.recordIdentity(role, next)
}

func (h *defaultRotationHarness) sockets(side string) []string {
	h.t.Helper()
	paths, err := filepath.Glob(filepath.Join(h.runDir(side), "p-*", "core.sock"))
	if err != nil {
		h.t.Fatal(err)
	}
	return paths
}
func (h *defaultRotationHarness) enableEpoch(side, id string) *rotationEpoch {
	h.t.Helper()
	before := h.sockets(side)
	lower := time.Now()
	h.manage(side, id, pb.ManagePluginRequest_ENABLE, nil)
	h.await(side+" "+id+" ready", func(context.Context) error {
		s := h.plugin(side, id)
		if !s.Enabled || s.State != "ready" || s.Protocol == nil {
			return errors.New("not ready")
		}
		return nil
	})
	ready := time.Now()
	status := h.plugin(side, id)
	initial := h.findWorker(side, id, "", ready.Add(10*time.Second))
	e := &rotationEpoch{Role: side + "-" + id, Side: side, Plugin: id, Revision: status.Revision, Lower: lower, Ready: ready, LastPresent: ready}
	e.Original = initial
	e.Current = e.Original
	h.allPIDs = append(h.allPIDs, initial.PID)
	for _, path := range h.sockets(side) {
		if !slices.Contains(before, path) {
			if e.Socket != "" {
				h.t.Fatal("multiple sockets appeared while enabling one worker")
			}
			e.Socket = path
		}
	}
	if e.Socket == "" {
		h.t.Fatal("no fresh CoreAPI socket for enabled worker")
	}
	h.event("initial_epoch", e)
	return e
}

// Preserve the legacy helper contract used by unrelated launchd acceptance.
func rotationAlive(ctx context.Context, identity rotationProcessIdentity) bool {
	p, err := processinfo.NewProcessWithContext(ctx, int32(identity.PID))
	if err != nil {
		return false
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil || created != identity.Created {
		return false
	}
	statuses, err := p.StatusWithContext(ctx)
	return err == nil && !slices.Contains(statuses, processinfo.Zombie)
}
func rotationObservedAlive(ctx context.Context, identity rotationProcessIdentity) (bool, error) {
	return (observer.Inspector{}).Alive(ctx, rotationObserved(identity))
}

func (h *defaultRotationHarness) observeEpoch(e *rotationEpoch, preflight bool) {
	h.t.Helper()
	now := time.Now()
	alive, err := rotationObservedAlive(h.ctx, e.Current)
	if err != nil {
		h.t.Fatal("observe current process", e.Role, err)
	}
	status := h.plugin(e.Side, e.Plugin)
	if !status.Enabled || status.Revision != e.Revision {
		h.t.Fatal("rotation changed durable plugin intent", e.Role, status)
	}
	if status.State != "ready" {
		e.Unavailable = true
		if status.Protocol != nil {
			h.t.Fatal("unavailable plugin advertises protocol", status)
		}
	}
	if alive {
		e.LastPresent = now
		if e.Replacements == 0 && now.After(e.Ready.Add(defaultRotationTTL+defaultRotationRecovery)) {
			h.t.Fatal("old worker survived maximum natural-expiry window", e.Role)
		}
		return
	}
	if preflight {
		h.t.Fatal("preflight unexpectedly replaced a worker", e.Role)
	}
	if e.Observer != nil && e.Replacements == 0 {
		select {
		case <-e.Observer.done:
			observation := e.Observer.result
			if observation.Error != "" {
				h.event("retirement_observation_error", map[string]any{"role": e.Role, "observation": observation})
				h.t.Fatal("retirement observation", e.Role, observation.Error)
			}
			if observation.FirstAbsent.IsZero() {
				h.t.Fatal("retirement observer ended without a disappearance")
			}
			e.LastPresent = observation.LastPresent
			if e.FirstAbsent.IsZero() {
				e.FirstAbsent = observation.FirstAbsent
				h.event("retirement_sampling", map[string]any{"role": e.Role, "observation": observation})
				h.event("retirement_observed", e)
			}
		default:
			return
		}
	}
	if e.FirstAbsent.IsZero() {
		e.FirstAbsent = now
		h.event("retirement_observed", e)
	}
	if err := rotationRetirementAllowed(*e, now); err != nil {
		h.t.Fatal(e.Role, err)
	}
	if status.State != "ready" {
		return
	}
	if !errors.Is(syscall.Kill(e.Original.PID, 0), syscall.ESRCH) {
		return
	} // ready can precede OS reap
	if _, err := os.Lstat(e.Socket); !errors.Is(err, os.ErrNotExist) {
		return
	}
	next := h.findWorker(e.Side, e.Plugin, e.Original.SHA256, e.Ready.Add(defaultRotationTTL+defaultRotationRecovery))
	if err := rotationNextEpoch(*e, next, time.Now()); err != nil {
		h.t.Fatal(e.Role, err)
	}
	e.Current = next
	e.Replacements++
	h.allPIDs = append(h.allPIDs, next.PID)
	h.event("replacement_epoch", e)
}
func (h *defaultRotationHarness) fixedResources(fixed []rotationProcessIdentity, epochs []*rotationEpoch, baseline map[string]int32, goBaseline int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 8*time.Second)
	defer cancel()
	stable := 0
	current := append([]rotationProcessIdentity(nil), fixed...)
	for _, e := range epochs {
		current = append(current, e.Current)
	}
	var last error
	for ctx.Err() == nil {
		last = nil
		for _, identity := range current {
			alive, err := rotationObservedAlive(ctx, identity)
			if err != nil {
				h.t.Fatal("resource process observation", identity.Role, err)
			}
			if !alive {
				last = fmt.Errorf("identity disappeared: %+v", identity)
				break
			}
			p, err := processinfo.NewProcessWithContext(ctx, int32(identity.PID))
			if err != nil {
				last = err
				break
			}
			fds, err := p.NumFDsWithContext(ctx)
			if err != nil {
				last = err
				break
			}
			if fds > baseline[identity.Role]+4 {
				last = fmt.Errorf("%s FDs %d exceed original role baseline %d+4", identity.Role, fds, baseline[identity.Role])
				break
			}
		}
		if runtime.NumGoroutine() > goBaseline+6 {
			last = fmt.Errorf("test goroutines %d exceed %d+6", runtime.NumGoroutine(), goBaseline)
		}
		if last == nil {
			stable++
		} else {
			stable = 0
		}
		if stable == 3 {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	h.t.Fatal("fixed role resource baseline did not recover", last)
}
