//go:build memoryprofile

package memoryprofile_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"
)

var windowPreflight = flag.Bool("memoryprofile-preflight", false, "one reduced diagnostic window; never full multi-window acceptance")
var windowMode = flag.String("memoryprofile-mode", "load", "load or idle-control; control keeps sampling but performs no measured data-plane work")

type windowPlan struct {
	Mode          string        `json:"mode"`
	ProfilePolicy string        `json:"profile_policy"`
	Windows       int           `json:"windows"`
	Rounds        int           `json:"rounds_per_window"`
	LoadDuration  time.Duration `json:"load_duration_ns"`
	WarmupRounds  int           `json:"warmup_rounds"`
	WarmupIdle    time.Duration `json:"warmup_idle_ns"`
	Preflight     bool          `json:"preflight"`
}

type windowObservation struct {
	Index          int           `json:"index"`
	Started        time.Time     `json:"started"`
	LoadElapsed    time.Duration `json:"load_elapsed_ns"`
	IdleElapsed    time.Duration `json:"idle_elapsed_ns"`
	LoadedSampling time.Duration `json:"loaded_sampling_ns"`
	IdleSampling   time.Duration `json:"idle_sampling_ns"`
	FileUpdates    int           `json:"file_updates"`
	TCPConnections int           `json:"tcp_connections"`
}

func TestRepeatedWorkerMemoryWindows(t *testing.T) {
	if *windowMode != "load" && *windowMode != "idle-control" {
		t.Fatal("memoryprofile-mode must be load or idle-control")
	}
	policy, err := profilePolicy()
	if err != nil {
		t.Fatal(err)
	}
	build, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("missing diagnostic build identity")
	}
	for _, setting := range build.Settings {
		if setting.Key == "-race" && setting.Value == "true" {
			t.Fatal("repeated-window evidence requires an ordinary, non-race build")
		}
	}
	plan := windowPlan{Windows: 5, Rounds: 16, LoadDuration: 30 * time.Second, WarmupRounds: 2, WarmupIdle: 15 * time.Second}
	if *windowPreflight {
		plan = windowPlan{Windows: 1, Rounds: 4, LoadDuration: 8 * time.Second, WarmupRounds: 2, WarmupIdle: 4 * time.Second, Preflight: true}
	}
	plan.Mode = *windowMode
	plan.ProfilePolicy = policy
	measuredRounds := plan.Rounds
	if plan.Mode == "idle-control" {
		measuredRounds = 0
	}
	f := startDataFixtureFor(t, 7*time.Minute)
	started := time.Now()
	files := startWindowFiles(t, f)
	tcp := startWindowTCP(t, f, 2*(plan.WarmupRounds+plan.Windows*measuredRounds))
	round := func() { files.update(t); tcp.exchange(t); tcp.exchange(t) }
	for range plan.WarmupRounds {
		round()
	}
	warmupHash := fmt.Sprintf("%x", files.current)
	f.idle(t, plan.WarmupIdle)
	identities, err := readyIdentities(f.root)
	if err != nil || len(identities) != 6 {
		t.Fatal("expected six independently owned diagnostic processes", identities, err)
	}
	samples := func(label string) time.Duration {
		t.Helper()
		since := time.Now()
		files.checkCurrent(t)
		tcp.settle(t)
		if tcp.accepted.Load() != int64(tcp.completed) {
			t.Fatal("unexpected target connection outside fixed workload", tcp.accepted.Load(), tcp.completed)
		}
		f.agent.sample(t, f.ctx, f.root, label)
		f.daemon.sample(t, f.ctx, f.root, label)
		for _, role := range []string{"sync-client", "sync-server", "port-client", "port-server"} {
			sampleWorker(t, f.ctx, f.root, role, label)
		}
		for _, identity := range identities {
			if err := checkIdentity(f.ctx, identity); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(f.root, identity.Role+"."+label+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var point sample
			if err := json.Unmarshal(data, &point); err != nil {
				t.Fatal(err)
			}
			if point.PID != identity.PID || point.Label != label || point.Role != identity.Role {
				t.Fatal("sample identity mismatch", point.Role, point.PID)
			}
			if point.ProfilePolicy != plan.ProfilePolicy {
				t.Fatal("sample profile policy differs from plan", point.Role, point.ProfilePolicy)
			}
		}
		return time.Since(since)
	}
	baselineSampling := samples("window-00-idle")
	var observations []windowObservation
	saveProgress := func(complete bool) {
		t.Helper()
		if err := saveJSON(filepath.Join(f.root, "windows.json"), map[string]any{
			"schema_version": 2, "mode": plan.Mode,
			"warmup_file_sha256": warmupHash, "current_file_sha256": fmt.Sprintf("%x", files.current),
			"plan": plan, "build": build, "started": started, "elapsed_ns": time.Since(started).Nanoseconds(),
			"processes": identities, "baseline_sampling_ns": baselineSampling.Nanoseconds(), "windows": observations,
			"file_updates_total": files.updates, "file_payload_bytes": 256 << 10,
			"measured_file_updates": files.updates - plan.WarmupRounds, "measured_tcp_connections": tcp.completed - 2*plan.WarmupRounds,
			"tcp_connections_total": tcp.completed, "tcp_target_accepted": tcp.accepted.Load(), "tcp_payload_bytes": tcpBytes,
			"forced_gc_per_sample": 2, "fixture_entrypoints": true, "normal_build": true,
			"worker_closed_profiles": "unavailable: workers exit before host closed samples",
			"complete":               complete, "all_workers_exited": complete, "children_exited": complete,
		}); err != nil {
			t.Fatal(err)
		}
	}
	saveProgress(false)
	for index := 1; index <= plan.Windows; index++ {
		window := windowObservation{Index: index, Started: time.Now()}
		fileBefore, tcpBefore := files.updates, tcp.completed
		for iteration := 0; iteration < plan.Rounds; iteration++ {
			if plan.Mode == "load" {
				round()
			}
			next := window.Started.Add(time.Duration(iteration+1) * plan.LoadDuration / time.Duration(plan.Rounds))
			remaining := time.Until(next)
			if remaining <= 0 {
				t.Fatalf("fixed workload missed window %d round %d slot by %s; no work omitted or catch-up burst", index, iteration+1, -remaining)
			}
			f.idle(t, remaining)
		}
		window.FileUpdates, window.TCPConnections = files.updates-fileBefore, tcp.completed-tcpBefore
		if window.FileUpdates != measuredRounds || window.TCPConnections != 2*measuredRounds {
			t.Fatal("mode-specific fixed workload mismatch", plan.Mode, window)
		}
		window.LoadElapsed = time.Since(window.Started)
		window.LoadedSampling = samples(fmt.Sprintf("window-%02d-loaded", index))
		idleStarted := time.Now()
		f.idle(t, window.LoadElapsed)
		window.IdleElapsed = time.Since(idleStarted)
		window.IdleSampling = samples(fmt.Sprintf("window-%02d-idle", index))
		observations = append(observations, window)
		saveProgress(false)
		t.Logf("mode=%s window %d/%d: %d fixed file updates, %d TCP exchanges; stage=%s equal idle=%s sampling=%s/%s", plan.Mode, index, plan.Windows, window.FileUpdates, window.TCPConnections, window.LoadElapsed, window.IdleElapsed, window.LoadedSampling, window.IdleSampling)
	}
	files.close(t)
	tcp.close(t)
	for _, name := range []string{"sync", "port"} {
		pids := map[string]int{}
		for _, identity := range identities {
			if identity.Role == name+"-client" || identity.Role == name+"-server" {
				pids[identity.Role] = identity.PID
			}
		}
		f.stopWorkers(t, name, pids)
	}
	f.client.Close()
	f.agent.close(t)
	f.daemon.close(t)
	saveProgress(true)
	t.Logf("completed repeated diagnostic: mode=%s preflight=%v, windows=%d, all six processes exited; private artifacts=%s", plan.Mode, plan.Preflight, plan.Windows, f.root)
}
