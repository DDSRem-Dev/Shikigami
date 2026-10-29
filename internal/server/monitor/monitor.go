// Package monitor samples OS resources without persisting runtime identities.
package monitor

import (
	"context"
	"os"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// Target comes from a fresh, unambiguous tmux inventory matched to a committed
// session. Identity includes the exact tmux session, window and pane IDs.
type Target struct {
	SessionID, Identity, Directory string
	PID                            int32
}
type Source interface {
	SampleTargets(context.Context, string) ([]Target, error)
	DiskDirectories(context.Context, string) ([]string, error)
}
type counter struct {
	at      time.Time
	seconds float64
	created int64
}
type Monitor struct {
	source    Source
	stateDir  string
	gate      chan struct{}
	system    counter
	processes map[int32]counter
}

func New(source Source, stateDir string) *Monitor {
	m := &Monitor{source: source, stateDir: stateDir, processes: map[int32]counter{}, gate: make(chan struct{}, 1)}
	m.gate <- struct{}{}
	return m
}

func Interval(request *pb.StatsRequest) (time.Duration, error) {
	seconds := request.GetIntervalSeconds()
	if seconds == 0 {
		seconds = 5
	}
	if seconds > 60 {
		return 0, connect.NewError(connect.CodeInvalidArgument, "stats interval must be 1–60 seconds")
	}
	return time.Duration(seconds) * time.Second, nil
}

// Snapshot uses one logical CPU core = 100%. DiskFreeBytes is the smallest
// available free space across the state and selected session filesystems.
// Unavailable measurements and a CPU counter's first sample remain absent.
func (m *Monitor) Snapshot(ctx context.Context, request *pb.StatsRequest) (*pb.StatsSnapshot, error) {
	if _, err := Interval(request); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.gate:
	}
	defer func() { m.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targets, err := m.source.SampleTargets(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	result := &pb.StatsSnapshot{Timestamp: now.Unix()}
	if times, err := cpu.TimesWithContext(ctx, false); err == nil && len(times) == 1 {
		t := times[0]
		busy := t.User + t.Nice + t.System + t.Irq + t.Softirq + t.Steal
		result.CpuPercent = percentage(m.system, counter{at: now, seconds: busy})
		m.system = counter{at: now, seconds: busy}
	}
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		value := int64(memory.Used)
		result.MemoryUsedBytes = &value
	}
	paths := map[string]bool{m.stateDir: true}
	directories, err := m.source.DiskDirectories(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		paths[directory] = true
	}
	for _, target := range targets {
		if target.Directory != "" {
			paths[target.Directory] = true
		}
	}
	diskComplete := true
	for path := range paths {
		if usage, err := disk.UsageWithContext(ctx, path); err == nil {
			value := int64(usage.Free)
			if result.DiskFreeBytes == nil || value < *result.DiskFreeBytes {
				result.DiskFreeBytes = &value
			}
		} else {
			diskComplete = false
		}
	}
	if !diskComplete {
		result.DiskFreeBytes = nil
	}
	next := map[int32]counter{}
	for _, target := range targets {
		stats := &pb.SessionStats{SessionId: target.SessionID}
		if target.PID > 0 {
			memory, percent, samples, ok := m.sampleTree(ctx, target.PID, now)
			if ok {
				stats.MemoryBytes = memory
				stats.CpuPercent = percent
				for pid, sample := range samples {
					next[pid] = sample
				}
			}
		}
		result.Sessions = append(result.Sessions, stats)
	}
	// Discard results if the managed pane changed while OS queries ran. A stale
	// PID is never used as proof that a process still belongs to this session.
	after, err := m.source.SampleTargets(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	current := map[string]Target{}
	for _, target := range after {
		current[target.SessionID] = target
	}
	for i, target := range targets {
		if actual, ok := current[target.SessionID]; !ok || actual.PID != target.PID || actual.Identity != target.Identity {
			result.Sessions[i].MemoryBytes = nil
			result.Sessions[i].CpuPercent = nil
		}
	}
	m.processes = next
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func percentage(previous, current counter) *float64 {
	if previous.at.IsZero() || previous.created != current.created || current.seconds < previous.seconds {
		return nil
	}
	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 {
		return nil
	}
	value := (current.seconds - previous.seconds) / elapsed * 100
	return &value
}

// sampleTree includes the pane process and its verified descendants. Every
// process is checked before and after sampling for UID/start-time stability;
// incomplete trees produce unavailable aggregate values, never partial totals.
func (m *Monitor) sampleTree(ctx context.Context, root int32, now time.Time) (*int64, *float64, map[int32]counter, bool) {
	queue := []int32{root}
	seen := map[int32]bool{}
	samples := map[int32]counter{}
	var memory int64
	var percent float64
	cpuAvailable := true
	parents := map[int32]int32{}
	for len(queue) > 0 {
		if len(seen) >= 4096 {
			return nil, nil, nil, false
		}
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		proc, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			return nil, nil, nil, false
		}
		created, err := proc.CreateTimeWithContext(ctx)
		if err != nil {
			return nil, nil, nil, false
		}
		uids, err := proc.UidsWithContext(ctx)
		if err != nil || len(uids) == 0 {
			return nil, nil, nil, false
		}
		for _, uid := range uids {
			if uid != uint32(os.Geteuid()) {
				return nil, nil, nil, false
			}
		}
		times, err := proc.TimesWithContext(ctx)
		if err != nil {
			return nil, nil, nil, false
		}
		usage, err := proc.MemoryInfoWithContext(ctx)
		if err != nil {
			return nil, nil, nil, false
		}
		children, err := proc.ChildrenWithContext(ctx)
		if err != nil {
			return nil, nil, nil, false
		}
		for _, child := range children {
			parent, err := child.PpidWithContext(ctx)
			if err != nil || parent != pid {
				return nil, nil, nil, false
			}
			queue = append(queue, child.Pid)
			parents[child.Pid] = pid
		}
		// Use a new object: gopsutil caches CreateTime on each Process instance.
		check, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			return nil, nil, nil, false
		}
		again, err := check.CreateTimeWithContext(ctx)
		if err != nil || again != created {
			return nil, nil, nil, false
		}
		current := counter{at: now, seconds: times.User + times.System, created: created}
		samples[pid] = current
		if value := percentage(m.processes[pid], current); value != nil {
			percent += *value
		} else {
			cpuAvailable = false
		}
		memory += int64(usage.RSS)
	}
	for pid, sample := range samples {
		proc, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			return nil, nil, nil, false
		}
		created, err := proc.CreateTimeWithContext(ctx)
		if err != nil || created != sample.created {
			return nil, nil, nil, false
		}
		if parent, ok := parents[pid]; ok {
			actual, err := proc.PpidWithContext(ctx)
			if err != nil || actual != parent {
				return nil, nil, nil, false
			}
		}
	}
	if !cpuAvailable {
		return &memory, nil, samples, true
	}
	return &memory, &percent, samples, true
}

// Watch sends synchronously, so network backpressure cannot create an unbounded
// sampling queue. The caller's stream context owns the complete lifetime.
func (m *Monitor) Watch(ctx context.Context, request *pb.StatsRequest, send func(*pb.StatsSnapshot) error) error {
	interval, err := Interval(request)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		snapshot, err := m.Snapshot(ctx, request)
		if err != nil {
			return err
		}
		if err = send(snapshot); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
