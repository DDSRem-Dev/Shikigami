//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	processinfo "github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/proto"
)

var defaultRotationMode = flag.String("shiki-default-rotation", "", "independent shipped-default experiment: preflight or observe; no TTL override")

func TestDefaultCredentialRotationPreflight(t *testing.T) {
	if *defaultRotationMode != "preflight" {
		t.Skip("explicit preflight only; NOT_12H_ACCEPTANCE")
	}
	runDefaultRotation(t, true)
}
func TestDefaultCredentialRotation(t *testing.T) {
	if *defaultRotationMode != "observe" {
		t.Skip("explicit 12h30m production-default observation required")
	}
	runDefaultRotation(t, false)
}
func runDefaultRotation(t *testing.T, preflight bool) {
	h := newDefaultRotationHarness(t)
	mode := "observe"
	if preflight {
		mode = "preflight-NOT_12H_ACCEPTANCE"
	}
	h.event("mode", map[string]any{"mode": mode, "natural_rotation_acceptance": !preflight, "minimum_observation": defaultRotationObservation.String(), "post_recovery_minimum": "30m"})
	target := newRotationTarget(h)
	h.configureRotationPort(target)
	var epochs []*rotationEpoch
	for _, side := range []string{"server", "client"} {
		for _, id := range []string{"sync", "port"} {
			epochs = append(epochs, h.enableEpoch(side, id))
		}
	}
	work := h.prepareSync()
	flow, remoteRoot := h.mapping("sustained", true)
	h.mappingState(flow, "active")
	ports := h.prepareRotationPort(target)
	terminal, session := h.terminal()
	statsCtx, stopStats := context.WithCancel(h.ctx)
	stats, err := h.remote.WatchStats(statsCtx, &pb.StatsRequest{IntervalSeconds: 1})
	if err != nil {
		stopStats()
		t.Fatal(err)
	}
	var statsTimestamp atomic.Int64
	var statsErr error
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		for {
			frame, err := stats.Receive()
			if err != nil {
				statsErr = err
				return
			}
			statsTimestamp.Store(frame.Timestamp)
		}
	}()
	t.Cleanup(func() {
		stopStats()
		stats.Close()
		select {
		case <-statsDone:
		case <-time.After(5 * time.Second):
			t.Error("stats reader failed to join")
		}
	})
	freshStats := func(after int64) {
		h.await("fresh original stats stream", func(context.Context) error {
			select {
			case <-statsDone:
				t.Fatal("original stats stream ended", statsErr)
			default:
			}
			if statsTimestamp.Load() <= after {
				return errors.New("waiting for fresh frame")
			}
			return nil
		})
	}
	freshStats(0)
	initialServers, err := h.local.ListServers(h.ctx, &pb.Empty{})
	if err != nil || len(initialServers.GetServers()) != 1 {
		t.Fatal(err)
	}
	initialServer := proto.Clone(initialServers.Servers[0]).(*apb.ServerRecord)
	var fixed []rotationProcessIdentity
	for _, entry := range []struct {
		role string
		pid  int
	}{{"test", os.Getpid()}, {"agent", h.parents["client"]}, {"daemon", h.parents["server"]}, {"pane", int(session.ClaudePid)}} {
		fixed = append(fixed, h.identity(entry.role, entry.pid))
	}
	cycle := 0
	runCycle := func() {
		h.rotationExchange(ports)
		call, cancel := context.WithTimeout(h.ctx, 15*time.Second)
		defer cancel()
		marker := fmt.Sprintf("default-rotation-%08d", cycle)
		mixedTerminalSample(t, call, terminal, marker)
		size := 64 << 10
		if cycle%4 == 0 {
			size = 1 << 20
		}
		data := bytes.Repeat([]byte(marker+"\n"), size/len(marker+"\n")+1)[:size]
		source, destination := flow.LocalRoot, remoteRoot
		if cycle%2 != 0 {
			source, destination = remoteRoot, flow.LocalRoot
		}
		h.write(source, "flow.txt", data)
		h.expectFile(flow, destination, "flow.txt", data)
		current, err := h.remote.GetSession(call, &pb.GetSessionRequest{Id: session.Id})
		if err != nil || current.GetClaudePid() != session.ClaudePid || !current.IsConnected || current.ClaudeStatus != "running" {
			t.Fatal("original terminal/process lost", err)
		}
		cycle++
	}
	for range 8 {
		runCycle()
	}
	h.verifySync(work)
	h.durableRotation(work, ports)
	baseline := map[string]int32{}
	initialIdentities := append([]rotationProcessIdentity(nil), fixed...)
	for _, e := range epochs {
		initialIdentities = append(initialIdentities, e.Current)
	}
	for _, identity := range initialIdentities {
		p, err := processinfo.NewProcess(int32(identity.PID))
		if err != nil {
			t.Fatal(err)
		}
		fds, err := p.NumFDsWithContext(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		baseline[identity.Role] = fds
	}
	goBaseline := runtime.NumGoroutine()
	h.event("fixed_resource_baseline", map[string]any{"fd": baseline, "test_goroutines": goBaseline, "fd_allowance": 4, "goroutine_allowance": 6})
	earliest := epochs[0].Lower.Add(defaultRotationTTL)
	latest := earliest
	for _, e := range epochs {
		if e.Lower.Add(defaultRotationTTL).Before(earliest) {
			earliest = e.Lower.Add(defaultRotationTTL)
		}
		if e.Ready.Add(defaultRotationTTL).After(latest) {
			latest = e.Ready.Add(defaultRotationTTL)
		}
	}
	started := time.Now()
	end := latest.Add(30 * time.Minute)
	if preflight {
		end = started.Add(time.Minute)
	}
	h.event("observation_started", map[string]any{"end_not_before": end, "earliest_expiry": earliest, "latest_expiry": latest, "mode": mode})
	nextWork, nextCheckpoint := time.Now(), time.Now()
	holding, recovered := false, false
	for time.Now().Before(end) {
		if h.ctx.Err() != nil {
			t.Fatal(h.ctx.Err())
		}
		for _, identity := range fixed {
			alive, err := rotationObservedAlive(h.ctx, identity)
			if err != nil {
				t.Fatal("fixed process observation", identity.Role, err)
			}
			if !alive {
				t.Fatal("fixed host/test/pane identity lost", identity)
			}
		}
		for _, e := range epochs {
			h.observeEpoch(e, preflight)
		}
		now := time.Now()
		inWindow := !preflight && !now.Before(earliest.Add(-30*time.Second)) && !recovered
		if inWindow && !holding {
			for _, e := range epochs {
				e.Observer = startRotationProcessObserver(h.ctx, e.Original)
				t.Cleanup(e.Observer.close)
			}
			h.holdRotationConnection(ports)
			holding = true
		}
		if holding && !recovered && now.Before(earliest.Add(-3*time.Second)) {
			h.rotationHeartbeat(ports, []byte("alive-before-natural-expiry\n"))
		}

		if holding && !recovered && !now.Before(earliest.Add(-3*time.Second)) && ports.observer == nil {
			portEarliest, portLatest := latest, earliest
			for _, e := range epochs {
				if e.Plugin == "port" {
					if e.Lower.Add(defaultRotationTTL).Before(portEarliest) {
						portEarliest = e.Lower.Add(defaultRotationTTL)
					}
					if e.Ready.Add(defaultRotationTTL).After(portLatest) {
						portLatest = e.Ready.Add(defaultRotationTTL)
					}
				}
			}
			ports.observer = startRotationTCPObserver(ports.held, portEarliest, portLatest.Add(30*time.Second))
			t.Cleanup(ports.observer.close)
		}
		all := true
		for _, e := range epochs {
			all = all && e.Replacements == 1
		}
		if !preflight && all && !recovered {
			for _, side := range []string{"client", "server"} {
				paths := h.sockets(side)
				if len(paths) != 2 {
					t.Fatal("replacement side does not have two private sockets", side, paths)
				}
				for _, e := range epochs {
					if slices.Contains(paths, e.Socket) {
						t.Fatal("old CoreAPI socket reused")
					}
				}
				h.event("replacement_sockets", map[string]any{"side": side, "paths": paths})
			}
			h.verifyRotatedConnection(ports)
			h.verifySync(work)
			h.durableRotation(work, ports)
			freshStats(time.Now().Unix())
			recovered = true
			end = time.Now().Add(30 * time.Minute)
			h.event("all_roles_recovered", map[string]any{"post_recovery_until": end, "simultaneous_retirement_claimed": false})
		}
		if now.After(nextWork) && (!inWindow || recovered) {
			runCycle()
			nextWork = time.Now().Add(10 * time.Second)
		} else if inWindow {
			call, cancel := context.WithTimeout(h.ctx, 5*time.Second)
			mixedTerminalSample(t, call, terminal, fmt.Sprintf("rotation-window-%d", time.Now().UnixNano()))
			cancel()
		}
		if now.After(nextCheckpoint) && (!inWindow || recovered) {
			h.fixedResources(fixed, epochs, baseline, goBaseline)
			h.verifySync(work)
			h.durableRotation(work, ports)
			pids := []int{}
			for _, identity := range fixed {
				pids = append(pids, identity.PID)
			}
			for _, e := range epochs {
				pids = append(pids, e.Current.PID)
			}
			soakProcessMetrics(t, h.ctx, "default-rotation", pids)
			h.event("checkpoint", map[string]any{"cycles": cycle, "elapsed": time.Since(started).String(), "recovered": recovered})
			nextCheckpoint = time.Now().Add(time.Minute)
		}
		if !preflight && !recovered && now.After(latest.Add(defaultRotationRecovery)) {
			t.Fatal("four workers did not naturally rotate within recovery deadline")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-h.ctx.Done():
			timer.Stop()
			t.Fatal(h.ctx.Err())
		case <-timer.C:
		}
	}
	if !preflight && !recovered {
		t.Fatal("no natural default-lifetime rotation observed")
	}
	for _, e := range epochs {
		h.observeEpoch(e, preflight)
		want := 1
		if preflight {
			want = 0
		}
		if e.Replacements != want {
			t.Fatal("unexpected epoch count", e)
		}
	}
	runCycle()
	h.fixedResources(fixed, epochs, baseline, goBaseline)
	h.verifySync(work)
	h.durableRotation(work, ports)
	freshStats(time.Now().Unix())
	afterServers, err := h.local.ListServers(h.ctx, &pb.Empty{})
	if err != nil || len(afterServers.GetServers()) != 1 {
		t.Fatal(err)
	}
	after := afterServers.Servers[0]
	if after.Config.DaemonId != initialServer.Config.DaemonId || after.CredentialRevision != initialServer.CredentialRevision || after.ConnectionState != "ready" || after.ConnectionPath != "direct-tls" {
		t.Fatal("rotation changed server identity/auth/TLS route")
	}
	h.event("application_result", map[string]any{"mode": mode, "cycles": cycle, "elapsed": time.Since(started).String(), "natural_rotation_passed": !preflight, "cleanup_still_required": true})
}
