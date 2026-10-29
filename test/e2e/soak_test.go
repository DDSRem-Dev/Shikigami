//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	processinfo "github.com/shirou/gopsutil/v4/process"
)

var applicationSoak = flag.Duration("shiki-soak-duration", 0, "opt-in sustained terminal and sync workload (1m..8h; 1h recommended)")

func applicationSoakDuration(t *testing.T) time.Duration {
	t.Helper()
	// This experiment verifies unchanged process identities, so it ends before
	// the production worker's intentional 12-hour credential replacement.
	if *applicationSoak != 0 && (*applicationSoak < time.Minute || *applicationSoak > 8*time.Hour) {
		t.Fatal("shiki-soak-duration must be 0 (disabled) or 1m..8h")
	}
	return *applicationSoak
}

// exerciseApplicationSoak keeps the shipped agent, daemon, sync workers and
// one actual tmux terminal alive. It uses the existing core scenario so its
// subsequent conflict, recovery and teardown checks still run after the soak.
func exerciseApplicationSoak(t *testing.T, parent context.Context, configuration, local string, project *pb.Project, mapping *ppb.Mapping, sessionID string, daemonPID int) {
	t.Helper()
	duration := applicationSoakDuration(t)
	if duration == 0 {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	client, err := frontend.Open(ctx, configuration, frontend.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	settings, err := config.LoadClient(configuration)
	if err != nil {
		t.Fatal(err)
	}
	pidText, err := os.ReadFile(filepath.Join(settings.Agent.StateDir, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	agentPID, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil || agentPID <= 1 {
		t.Fatal("invalid soak agent identity", err)
	}
	remote := client.Remote(mapping.ServerId)
	initial, err := remote.GetSession(ctx, &pb.GetSessionRequest{Id: sessionID})
	if err != nil || initial.GetClaudeStatus() != "running" || initial.GetClaudePid() <= 1 {
		t.Fatal("soak requires a running original pane", err)
	}
	terminal := openDeviceTerminal(t, ctx, client, mapping.ServerId, sessionID, false)
	terminal.expectMode(t, "controller")
	terminal.mu.Lock()
	terminal.textLimit = 128 << 10
	terminal.mu.Unlock()
	defer terminal.stream.Close()
	pids := []int{os.Getpid(), agentPID, daemonPID, int(initial.ClaudePid)}
	for _, parentPID := range []int{agentPID, daemonPID} {
		pids = append(pids, soakWorkerPID(t, ctx, parentPID, filepath.Join(settings.PluginsDir, "shiki-plugin-sync.race")))
	}
	latencies := &soakLatencies{minimum: time.Hour}
	var transferred uint64
	terminalLatencies := &soakLatencies{minimum: time.Hour}
	mixed := startMixedWorkload(t, ctx, client, mapping.ServerId)
	if mixed != nil {
		defer func() {
			if err := mixed.close(); err != nil {
				t.Errorf("mixed workload stop: %v", err)
			}
		}()
		for _, parentPID := range []int{agentPID, daemonPID} {
			pids = append(pids, soakWorkerPID(t, ctx, parentPID, filepath.Join(settings.PluginsDir, "shiki-plugin-port.race")))
		}
	}
	runCycle := func(cycle int) {
		t.Helper()
		call, stop := context.WithTimeout(ctx, 12*time.Second)
		// The persistent stream otherwise outlives this cycle. Interrupt Send
		// as well as Receive if a peer stops draining it; do not leave a waiter.
		stopWatchdog := context.AfterFunc(call, cancel)
		defer func() { stopWatchdog(); stop() }()
		finishTCP := mixed.begin(t, call, cycle)
		defer finishTCP()
		marker := fmt.Sprintf("soak-%08d", cycle)
		terminal.mu.Lock()
		terminal.text = "" // Bound test-side accumulation independently of duration.
		terminal.mu.Unlock()
		if mixed == nil {
			terminal.send(t, marker)
		}
		size := 64 << 10
		if cycle%8 == 0 || cycle%8 == 3 {
			size = 1 << 20
		}
		payload := bytes.Repeat([]byte(marker+"\n"), size/len(marker+"\n")+1)[:size]
		want := sha256.Sum256(payload)
		source, destination := local, project.Path
		if cycle%2 != 0 {
			source, destination = project.Path, local
		}
		started := time.Now()
		if err := os.WriteFile(filepath.Join(source, "source.txt"), payload, 0600); err != nil {
			t.Fatal(err)
		}
		if mixed != nil {
			terminalLatencies.add(mixedTerminalSample(t, call, terminal, marker))
		}
		await(t, call, func(probe context.Context) error {
			data, err := os.ReadFile(filepath.Join(destination, "source.txt"))
			if err != nil || sha256.Sum256(data) != want {
				return errors.New("soak file has not converged")
			}
			state, err := client.Agent().GetSyncState(probe, deviceScope(mapping))
			if err != nil {
				return err
			}
			if state.State != "active" || state.PendingFiles != 0 || state.ConflictCount != 0 {
				return fmt.Errorf("soak sync not settled: state=%s pending=%d conflicts=%d", state.State, state.PendingFiles, state.ConflictCount)
			}
			return nil
		})
		latencies.add(time.Since(started))
		transferred += uint64(size)
		if mixed == nil {
			terminal.expectText(t, call, "E2E_REPLY:"+marker)
		}
		current, err := remote.GetSession(call, &pb.GetSessionRequest{Id: sessionID})
		if err != nil || current.GetClaudePid() != initial.ClaudePid || current.GetClaudeStatus() != "running" || !current.IsConnected {
			t.Fatal("sustained work changed the original terminal identity", err)
		}
	}
	for i := 0; i < 8; i++ {
		runCycle(i)
	}
	latencies = &soakLatencies{minimum: time.Hour}
	transferred = 0
	terminalLatencies = &soakLatencies{minimum: time.Hour}
	if mixed != nil {
		mixed.latencies = soakLatencies{minimum: time.Hour}
		mixed.payloadBytes, mixed.rounds = 0, 0
		idle := mixedIdleTerminalSamples(t, ctx, cancel, terminal, "before")
		t.Logf("mixed terminal idle before: %s; same warmed processes, no deliberate file/TCP workload, 1ms observer poll", idle)
	}
	baseline := resources.Capture(t, pids...)
	started, checkpoint := time.Now(), time.Now()
	soakProcessMetrics(t, ctx, "baseline", pids)
	t.Logf("application soak started: duration=%s pids=%v; alternating 64KiB/1MiB sync and terminal echo; all shipped children race-enabled; simultaneous_tcp_clients=%d", duration, pids, mixedSoakClients(t))
	cycles := 0
	for time.Since(started) < duration {
		runCycle(cycles + 8)
		cycles++
		if time.Since(checkpoint) >= time.Minute {
			resources.Restored(t, fmt.Sprintf("application soak %s/%s", time.Since(started).Round(time.Second), duration), baseline, 4, 6)
			soakProcessMetrics(t, ctx, "checkpoint", pids)
			checkpoint = time.Now()
			t.Logf("application soak progress: elapsed=%s cycles=%d payload_bytes=%d latency=%s", time.Since(started).Round(time.Second), cycles, transferred, latencies)
		}
		timer := time.NewTimer(min(2*time.Second, max(0, duration-time.Since(started))))
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal(ctx.Err())
		case <-timer.C:
		}
	}
	if mixed != nil {
		idle := mixedIdleTerminalSamples(t, ctx, cancel, terminal, "after")
		t.Logf("mixed terminal idle after: %s; same warmed processes, no deliberate file/TCP workload, 1ms observer poll", idle)
	}
	resources.Restored(t, "application soak final", baseline, 4, 6)
	soakProcessMetrics(t, ctx, "final", pids)
	if _, err := os.Stat(filepath.Join(project.Path, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("sustained sync copied default-excluded credentials", err)
	}
	if mixed != nil {
		t.Logf("mixed workload completed: tcp_clients=%d rounds=%d exchanges=%d sent_payload_bytes=%d echoed_payload_bytes=%d half_close_tail_bytes=%d tcp_latency=%s terminal_latency=%s", mixed.clients, mixed.rounds, mixed.latencies.count, mixed.payloadBytes, mixed.payloadBytes, uint64(mixed.latencies.count)*uint64(len(mixedResponseTail)), &mixed.latencies, terminalLatencies)
	}
	t.Logf("application soak completed: elapsed=%s cycles=%d payload_bytes=%d latency=%s", time.Since(started).Round(time.Millisecond), cycles, transferred, latencies)
}

func soakWorkerPID(t *testing.T, ctx context.Context, parentPID int, executable string) int {
	t.Helper()
	parent, err := processinfo.NewProcess(int32(parentPID))
	if err != nil {
		t.Fatal(err)
	}
	children, err := parent.ChildrenWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	var matching []int
	for _, child := range children {
		path, err := child.ExeWithContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err == nil && path == want {
			matching = append(matching, int(child.Pid))
		}
	}
	if len(matching) != 1 {
		t.Fatalf("expected one owned worker beneath %d, got %v", parentPID, matching)
	}
	return matching[0]
}

func soakProcessMetrics(t *testing.T, ctx context.Context, label string, pids []int) {
	t.Helper()
	for _, pid := range pids {
		p, err := processinfo.NewProcess(int32(pid))
		if err != nil {
			t.Fatal(err)
		}
		memory, err := p.MemoryInfoWithContext(ctx)
		if err != nil {
			t.Fatal("RSS observation unavailable", err)
		}
		cpu, err := p.TimesWithContext(ctx)
		if err != nil {
			t.Fatal("CPU observation unavailable", err)
		}
		t.Logf("soak process %s: pid=%d rss_bytes=%d cpu_seconds=%.3f", label, pid, memory.RSS, cpu.User+cpu.System)
	}
}

type soakLatencies struct {
	count, sum       int64
	minimum, maximum time.Duration
	buckets          [7]int64
}

func (s *soakLatencies) add(value time.Duration) {
	s.count++
	s.sum += int64(value)
	s.minimum, s.maximum = min(s.minimum, value), max(s.maximum, value)
	limits := [...]time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second}
	for i, limit := range limits {
		if value <= limit {
			s.buckets[i]++
			return
		}
	}
	s.buckets[len(s.buckets)-1]++
}

func (s *soakLatencies) String() string {
	return fmt.Sprintf("count=%d min=%s mean=%s max=%s buckets[<=100ms,250ms,500ms,1s,2s,5s,>5s]=%v", s.count, s.minimum.Round(time.Millisecond), time.Duration(s.sum/max(1, s.count)).Round(time.Millisecond), s.maximum.Round(time.Millisecond), s.buckets)
}
