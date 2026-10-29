package monitor

import (
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type sourceFunc func(context.Context, string) ([]Target, error)

func (f sourceFunc) SampleTargets(ctx context.Context, id string) ([]Target, error) {
	return f(ctx, id)
}
func (f sourceFunc) DiskDirectories(context.Context, string) ([]string, error) { return nil, nil }

func TestRealProcessMetricsAndIdentityChange(t *testing.T) {
	process := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	target := Target{SessionID: "sess_real", Identity: "$1/@1/%1", PID: int32(process.Process.Pid), Directory: t.TempDir()}
	var change atomic.Bool
	var calls atomic.Int32
	m := New(sourceFunc(func(context.Context, string) ([]Target, error) {
		value := target
		if change.Load() && calls.Add(1)%2 == 0 {
			value.Identity = "$2/@2/%2"
		}
		return []Target{value}, nil
	}), target.Directory)
	first, err := m.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.CpuPercent != nil || first.Sessions[0].CpuPercent != nil {
		t.Fatal("first CPU sample invented a value")
	}
	if first.MemoryUsedBytes == nil || *first.MemoryUsedBytes <= 0 || first.DiskFreeBytes == nil || first.Sessions[0].MemoryBytes == nil || *first.Sessions[0].MemoryBytes <= 0 {
		t.Fatalf("missing real resource measurements: %v", first)
	}
	second, err := m.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.CpuPercent == nil || second.Sessions[0].CpuPercent == nil {
		t.Fatalf("second CPU sample unavailable: %v", second)
	}
	change.Store(true)
	third, err := m.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.Sessions[0].CpuPercent != nil || third.Sessions[0].MemoryBytes != nil {
		t.Fatal("changed pane retained process measurements")
	}
}

func TestWatchCancellationIntervalAndSendFailure(t *testing.T) {
	m := New(sourceFunc(func(context.Context, string) ([]Target, error) {
		return []Target{{SessionID: "sess_absent", PID: 2147483647}}, nil
	}), t.TempDir())
	if _, err := m.Snapshot(t.Context(), &pb.StatsRequest{IntervalSeconds: 61}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err := m.Watch(ctx, nil, func(s *pb.StatsSnapshot) error {
		count++
		if s.Sessions[0].MemoryBytes != nil || s.Sessions[0].CpuPercent != nil {
			t.Fatal("invented unavailable metric")
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("watch cancellation: %d %v", count, err)
	}
	failure := errors.New("network send failed")
	if err := m.Watch(t.Context(), nil, func(*pb.StatsSnapshot) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestCPUIdentityAndUnits(t *testing.T) {
	now := time.Now()
	previous := counter{at: now, seconds: 10, created: 1}
	value := percentage(previous, counter{at: now.Add(time.Second), seconds: 12, created: 1})
	if value == nil || *value != 200 {
		t.Fatal("CPU must use one core = 100%", value)
	}
	if percentage(previous, counter{at: now.Add(time.Second), seconds: 12, created: 2}) != nil {
		t.Fatal("recycled PID inherited old counters")
	}
}

func TestCanceledSamplerDoesNotWaitBehindBusyQuery(t *testing.T) {
	m := New(sourceFunc(func(context.Context, string) ([]Target, error) { return nil, nil }), t.TempDir())
	<-m.gate
	defer func() { m.gate <- struct{}{} }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Snapshot(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
