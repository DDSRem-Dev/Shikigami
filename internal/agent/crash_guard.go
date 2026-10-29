package agent

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
)

const (
	crashWindow      = 5 * time.Minute
	crashLimit       = 3
	crashLoopReason  = "AGENT_CRASH_LOOP"
	crashStateReason = "AGENT_CRASH_STATE_UNAVAILABLE"
	crashClockReason = "AGENT_CRASH_CLOCK_ROLLBACK"
)

// All access is serialized by the agent instance lock, including manual
// recovery. Frontends and service managers do not modify this record.
type crashRecord struct {
	Schema      int     `json:"schema"`
	Build       string  `json:"build"`
	ObservedAt  int64   `json:"observed_at"`
	ActiveSince int64   `json:"active_since,omitempty"`
	Failures    []int64 `json:"failures,omitempty"`
	Blocked     bool    `json:"blocked,omitempty"`
}

type crashGuard struct {
	directory string
	record    crashRecord
	active    bool
}

func beginCrashGuard(directory, build string, recoverLoop bool, now time.Time) (*crashGuard, string) {
	guard := &crashGuard{directory: directory}
	data, err := readCrashRecord(filepath.Join(directory, "startup.json"))
	if err == nil {
		if len(data) > 4096 || strictjson.Decode(data, &guard.record) != nil || !validCrashRecord(guard.record) {
			return nil, crashStateReason
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, crashStateReason
	}
	stamp := now.UnixNano()
	if stamp <= 0 || stamp < guard.record.ObservedAt {
		return nil, crashClockReason
	}
	if guard.record.Build != build || recoverLoop || guard.record.Schema == 0 {
		guard.record = crashRecord{Schema: 1, Build: build, ObservedAt: stamp}
	}
	if guard.record.Blocked {
		return guard, crashLoopReason
	}
	// A previously long-running process breaks the rapid-crash sequence. Death
	// time itself cannot be reconstructed after SIGKILL; count the unexpected
	// end at observation, never revive failures older than the bounded window.
	if guard.record.ActiveSince != 0 {
		if stamp-guard.record.ActiveSince >= int64(crashWindow) {
			guard.record.Failures = nil
		}
		guard.record.Failures = recentCrashes(guard.record.Failures, stamp)
		guard.record.Failures = append(guard.record.Failures, stamp)
	} else {
		guard.record.Failures = recentCrashes(guard.record.Failures, stamp)
	}
	guard.record.ObservedAt = stamp
	guard.record.ActiveSince = 0
	if len(guard.record.Failures) >= crashLimit {
		guard.record.Blocked = true
	} else {
		guard.record.ActiveSince = stamp
		guard.active = true
	}
	if err := guard.save(); err != nil {
		return nil, crashStateReason
	}
	if guard.record.Blocked {
		return guard, crashLoopReason
	}
	return guard, ""
}

func readCrashRecord(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		return nil, errors.New("unsafe startup record")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if len(data) > 4096 {
		return nil, errors.New("startup record exceeds limit")
	}
	return data, err
}

func validCrashRecord(record crashRecord) bool {
	if record.Schema != 1 || record.Build == "" || len(record.Build) > 256 || record.ObservedAt <= 0 || record.ActiveSince < 0 || record.ActiveSince > record.ObservedAt || len(record.Failures) > crashLimit || (record.Blocked && (record.ActiveSince != 0 || len(record.Failures) != crashLimit)) {
		return false
	}
	previous := int64(0)
	for _, stamp := range record.Failures {
		if stamp <= 0 || stamp < previous || stamp > record.ObservedAt {
			return false
		}
		previous = stamp
	}
	return true
}

func recentCrashes(failures []int64, now int64) []int64 {
	var result []int64
	for _, stamp := range failures {
		if now-stamp < int64(crashWindow) {
			result = append(result, stamp)
		}
	}
	return result
}

func (g *crashGuard) finish(runErr error, now time.Time) error {
	if !g.active {
		return nil // Stopping diagnostic mode cannot clear a latched crash loop.
	}
	stamp := now.UnixNano()
	if stamp < g.record.ObservedAt {
		return errors.New(crashClockReason)
	}
	if runErr == nil {
		g.record.Failures = nil
	} else {
		if stamp-g.record.ActiveSince >= int64(crashWindow) {
			g.record.Failures = nil
		}
		g.record.Failures = append(recentCrashes(g.record.Failures, stamp), stamp)
		g.record.Blocked = len(g.record.Failures) >= crashLimit
	}
	g.record.ActiveSince = 0
	g.record.ObservedAt = stamp
	g.active = false
	return g.save()
}

func (g *crashGuard) save() error {
	data, err := json.Marshal(g.record)
	if err != nil {
		return err
	}
	return storage.AtomicWrite(g.directory, "startup.json", data)
}
