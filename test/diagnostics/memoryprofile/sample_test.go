//go:build memoryprofile

package memoryprofile_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

type memoryPoint struct {
	RSS          uint64 `json:"rss"`
	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapObjects  uint64 `json:"heap_objects"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapReleased uint64 `json:"heap_released"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Mallocs      uint64 `json:"mallocs"`
	Frees        uint64 `json:"frees"`
	Sys          uint64 `json:"go_sys"`
	NumGC        uint32 `json:"num_gc"`
	Goroutines   int    `json:"goroutines"`
	FDs          int32  `json:"fds"`
}
type stateCount struct {
	Bytes    int64          `json:"bytes"`
	Requests int            `json:"requests"`
	Buckets  map[string]int `json:"buckets"`
}
type sample struct {
	ProfilePolicy   string `json:"profile_policy"`
	Role, Label     string
	PID             int
	At              time.Time
	Before, AfterGC memoryPoint
	States          map[string]stateCount
	History         map[string]historyCount
}

func profilePolicy() (string, error) {
	policy := os.Getenv("SHIKI_MEMORY_PROFILE_POLICY")
	if policy == "" {
		policy = "each-boundary"
	}
	if policy != "each-boundary" && policy != "after-close" {
		return "", fmt.Errorf("unknown diagnostic profile policy %q", policy)
	}
	return policy, nil
}

type historyCount struct {
	Files int
	Bytes int64
}

func memory(ctx context.Context) (memoryPoint, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	p, err := process.NewProcessWithContext(ctx, int32(os.Getpid()))
	if err != nil {
		return memoryPoint{}, err
	}
	rss, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return memoryPoint{}, err
	}
	fds, err := p.NumFDsWithContext(ctx)
	if err != nil {
		return memoryPoint{}, err
	}
	return memoryPoint{RSS: rss.RSS, HeapAlloc: m.HeapAlloc, HeapObjects: m.HeapObjects, HeapInuse: m.HeapInuse, HeapIdle: m.HeapIdle, HeapReleased: m.HeapReleased, TotalAlloc: m.TotalAlloc, Mallocs: m.Mallocs, Frees: m.Frees, Sys: m.Sys, NumGC: m.NumGC, Goroutines: runtime.NumGoroutine(), FDs: fds}, nil
}

func collect(ctx context.Context, root, role, label string) error {
	policy, err := profilePolicy()
	if err != nil {
		return err
	}
	before, err := memory(ctx)
	if err != nil {
		return err
	}
	// Identical explicit sampling for both builds. Two collections also advance
	// the heap profiler's delayed free accounting. This perturbs the workload;
	// it is a diagnostic boundary, not a throughput or natural-GC measurement.
	runtime.GC()
	runtime.GC()
	after, err := memory(ctx)
	if err != nil {
		return err
	}
	result := sample{ProfilePolicy: policy, Role: role, Label: label, PID: os.Getpid(), At: time.Now(), Before: before, AfterGC: after, States: map[string]stateCount{}, History: map[string]historyCount{}}
	stateRoot := filepath.Join(root, role, "state")
	if name, side, ok := strings.Cut(role, "-"); ok {
		if side == "client" {
			stateRoot = filepath.Join(root, "agent", "state", "plugin-state", name)
		} else {
			stateRoot = filepath.Join(root, "daemon", "state", "plugin-data", name)
		}
	}
	err = filepath.WalkDir(stateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "history" {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(stateRoot, filepath.Dir(path))
			count := result.History[relative]
			count.Files++
			count.Bytes += info.Size()
			result.History[relative] = count
		}
		if !(entry.Name() == "state.json" || entry.Name() == "state.previous.json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var state struct {
			Requests map[string]json.RawMessage            `json:"requests"`
			Buckets  map[string]map[string]json.RawMessage `json:"buckets"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
		counts := stateCount{Bytes: int64(len(data)), Requests: len(state.Requests), Buckets: map[string]int{}}
		for name, values := range state.Buckets {
			counts.Buckets[name] = len(values)
		}
		relative, _ := filepath.Rel(stateRoot, path)
		result.States[relative] = counts
		return nil
	})
	if err != nil {
		return err
	}
	// Keep memory sampling, both GCs, and state walking identical. Only profile
	// serialization moves past all measured windows and the host's closed
	// AfterGC point. Workers exit before this point and have no closed profile.
	profiles := []string{"heap", "allocs", "goroutine"}
	if policy == "after-close" && !(label == "closed" && (role == "agent" || role == "daemon")) {
		profiles = nil
	}
	for _, name := range profiles {
		path := filepath.Join(root, role+"."+label+"."+name+".pprof")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = pprof.Lookup(name).WriteTo(file, 0)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return saveJSON(filepath.Join(root, role+"."+label+".json"), result)
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validLabel(label string) bool {
	return strings.Contains("|baseline|idle-before|load-after|idle-after|closed|file-baseline|file-loaded|file-idle|tcp-baseline|tcp-loaded|tcp-idle|", "|"+label+"|") && label != "" || windowLabel.MatchString(label)
}

var windowLabel = regexp.MustCompile(`^window-0[0-5]-(loaded|idle)$`)
