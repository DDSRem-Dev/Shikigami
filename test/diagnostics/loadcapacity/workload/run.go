//go:build shiki_load_capacity

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type dataInterval struct{ First, Last int64 }
type roundResult struct {
	Round, Concurrency, PeakTarget     int
	PayloadBytes                       int64
	Seconds, PayloadMiBPerSecond       float64
	FileMilliseconds                   []float64
	FileSettledMilliseconds            float64
	Files                              []fileObservation
	DATA                               []dataInterval
	ClientSentDATA, ClientReceivedDATA []dataInterval
	TCPHashes                          []string
	HashAndHalfCloseOK, CountersZero   bool
}
type stageResult struct {
	Stage, Concurrency int
	Rounds             []roundResult
	Terminal           []terminalSample
	LoadedCount        int
}
type report struct {
	Mode                  string
	Passed                bool
	Errors                []string
	IdleBefore, IdleAfter []terminalSample
	Stages                []stageResult
	Admission             []admissionResult
	Resources             []resourceSample
	Tracked               []processIdentity
	AllGenerations        []processIdentity
	Runtime               map[string]any
}

func (e *environment) syncSettled() bool {
	scope := &ppb.Scope{ServerId: e.mapping.ServerId, ProjectId: e.mapping.ProjectId, MappingId: e.mapping.Id}
	value, err := e.client.Agent().GetSyncState(e.ctx, scope)
	if err != nil {
		return false
	}
	ensure(value.ConflictCount == 0, "unexpected sync conflict")
	return value.State == "active" && value.PendingFiles == 0
}
func (e *environment) setLimits(local, remote int) {
	if e.forward != nil {
		e.cli("ports", "stop", e.forward.Spec.Id)
		e.forward = nil
	}
	port := e.target.listener.Addr().(*net.TCPAddr).Port
	for _, side := range []string{"local", "remote"} {
		e.rememberWorkers()
		values := map[string]any{"max_connections": local, "idle_timeout_seconds": 120}
		if side == "remote" {
			values["max_connections"] = remote
			values["allow_ports"] = []int{port}
		}
		path := filepath.Join(e.root, "port-"+side+".json")
		save(path, values)
		args := []string{"plugins", "config", "port", "--side", side, "--file", path}
		if side == "remote" {
			args = append(args, "--server", "capacity")
		}
		e.cli(args...)
		ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
		until(ctx, func() bool {
			value := new(pb.PluginStatus)
			args := []string{"plugins", "status", "port", "--side", side}
			if side == "remote" {
				args = append(args, "--server", "capacity")
			}
			decode(e.cli(args...), value)
			ensure(value.State != "failed" && value.State != "incompatible", "port worker failed config")
			return value.State == "ready"
		})
		cancel()
		e.rememberWorkers()
	}
	e.forward = new(ppb.ForwardState)
	decode(e.cli("--server", "capacity", "ports", "forward", fmt.Sprint(port), "--local-port", "0"), e.forward)
	ensure(e.forward.State == "listening" && e.forward.BoundPort > 0, "actual forward not listening")
}
func (e *environment) settled(ctx context.Context) {
	until(ctx, func() bool {
		list := take(e.client.Agent().ListForwards(ctx, &pb.Empty{}))
		found := false
		for _, forward := range list.Forwards {
			if forward.Spec.Id == e.forward.Spec.Id {
				found = true
				if forward.Connections != 0 {
					return false
				}
			}
		}
		ensure(found, "forward missing during reclamation")
		active, _, _ := e.target.counts()
		return active == 0
	})
	select {
	case err := <-e.target.errors:
		panic(err)
	default:
	}
}

type fileObservation struct {
	Direction                                       string
	Size                                            int
	ExpectedSHA256, SourceSHA256, DestinationSHA256 string
	Started                                         int64
	HashMilliseconds, SettledMilliseconds           float64
}

func (e *environment) files(ctx context.Context, round int, gate <-chan struct{}, done chan<- []fileObservation, errors chan<- error) {
	result := make([]fileObservation, 2)
	defer func() {
		if r := recover(); r != nil {
			errors <- fmt.Errorf("files: %v", r)
			return
		}
		done <- result
	}()
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for direction := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					failures <- fmt.Errorf("direction %d: %v", direction, r)
				}
			}()
			from, to, name := e.local, e.remote, "to-remote.bin"
			if direction == 1 {
				from, to, name = e.remote, e.local, "to-local.bin"
			}
			payload := bytes.Repeat([]byte{byte(round), byte(direction), 0x5a, 0x00}, (2<<20)/4)
			wanted := sha256.Sum256(payload)
			select {
			case <-gate:
			case <-ctx.Done():
				panic(ctx.Err())
			}
			started := time.Now()
			result[direction] = fileObservation{Direction: name, Size: len(payload), ExpectedSHA256: fmt.Sprintf("%x", wanted), Started: started.UnixNano()}
			need(os.WriteFile(filepath.Join(from, name), payload, 0600))
			until(ctx, func() bool {
				data, err := os.ReadFile(filepath.Join(to, name))
				return err == nil && sha256.Sum256(data) == wanted
			})
			result[direction].HashMilliseconds = float64(time.Since(started)) / float64(time.Millisecond)
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		need(err)
	}
	until(ctx, e.syncSettled)
	for direction := range 2 {
		from, to, name := e.local, e.remote, "to-remote.bin"
		if direction == 1 {
			from, to, name = e.remote, e.local, "to-local.bin"
		}
		result[direction].SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(take(os.ReadFile(filepath.Join(from, name)))))
		result[direction].DestinationSHA256 = fmt.Sprintf("%x", sha256.Sum256(take(os.ReadFile(filepath.Join(to, name)))))
		ensure(result[direction].SourceSHA256 == result[direction].ExpectedSHA256 && result[direction].DestinationSHA256 == result[direction].ExpectedSHA256, "settled source/destination hash changed")
		result[direction].SettledMilliseconds = float64(time.Now().UnixNano()-result[direction].Started) / float64(time.Millisecond)
	}
}
func (e *environment) round(round, concurrency int, size int64, stage int) (result roundResult, samples []terminalSample) {
	result.Round = round
	result.Concurrency = concurrency
	result.PayloadBytes = size * int64(concurrency)
	defer func() {
		save(filepath.Join(e.output, fmt.Sprintf("round-%03d.json", round)), map[string]any{"result": result, "terminal": samples})
	}()
	ctx, cancel := context.WithTimeout(e.ctx, 120*time.Second)
	defer cancel()
	entries := e.target.prepare(round, concurrency, size)
	_, accepted, _ := e.target.counts()
	address := fmt.Sprintf("127.0.0.1:%d", e.forward.BoundPort)
	exchanges := make([]*exchange, 0, concurrency)
	gate := make(chan struct{})
	defer func() {
		cancel()
		for _, x := range exchanges {
			_ = x.conn.Close()
		}
		for _, x := range exchanges {
			select {
			case <-x.finished:
			case <-time.After(5 * time.Second):
				panic("client exchange not joined")
			}
		}
	}()
	for _, entry := range entries {
		exchanges = append(exchanges, connectWithGate(ctx, address, entry, gate, false))
	}
	waitReady(ctx, entries, e.target)
	active, _, peak := e.target.counts()
	ensure(active == concurrency, "remote target concurrency differs from requested")
	filesDone := make(chan []fileObservation, 1)
	fileErrors := make(chan error, 1)
	filesFinished := make(chan struct{})
	go func() { defer close(filesFinished); e.files(ctx, round, gate, filesDone, fileErrors) }()
	defer func() { cancel(); <-filesFinished }()
	terminalDone := make(chan struct{})
	terminalStop := make(chan struct{})
	terminalErrors := make(chan error, 1)
	go func() {
		defer close(terminalDone)
		defer func() {
			if r := recover(); r != nil {
				terminalErrors <- fmt.Errorf("terminal: %v", r)
			}
		}()
		<-gate
		e.terminal.collect(ctx, terminalStop, stage, 0, &samples)
	}()
	var terminalOnce sync.Once
	stopTerminal := func() { terminalOnce.Do(func() { close(terminalStop) }); <-terminalDone }
	defer stopTerminal()
	started := time.Now()
	close(gate)
	joinExchanges(ctx, exchanges)
	seconds := time.Since(started).Seconds()
	select {
	case result.Files = <-filesDone:
	case err := <-fileErrors:
		panic(err)
	case <-ctx.Done():
		panic(ctx.Err())
	}
	for _, file := range result.Files {
		result.FileMilliseconds = append(result.FileMilliseconds, file.HashMilliseconds)
		result.FileSettledMilliseconds = max(result.FileSettledMilliseconds, file.SettledMilliseconds)
	}
	e.settled(ctx)
	stopTerminal()
	_, after, _ := e.target.counts()
	ensure(after-accepted == concurrency, "target accepted duplicate/unexpected connection")
	result.Round = round
	result.Concurrency = concurrency
	result.PeakTarget = peak
	result.PayloadBytes = size * int64(concurrency)
	result.Seconds = seconds
	result.PayloadMiBPerSecond = float64(result.PayloadBytes) / (1 << 20) / seconds
	result.HashAndHalfCloseOK = true
	result.CountersZero = true
	for _, entry := range entries {
		ensure(entry.FirstData > 0 && entry.LastData >= entry.FirstData, "target DATA interval missing")
		result.DATA = append(result.DATA, dataInterval{entry.FirstData, entry.LastData})
		result.TCPHashes = append(result.TCPHashes, fmt.Sprintf("%x", entry.Hash))
	}
	for _, x := range exchanges {
		result.ClientSentDATA = append(result.ClientSentDATA, x.SentDATA)
		result.ClientReceivedDATA = append(result.ClientReceivedDATA, x.ReceivedDATA)
	}
	select {
	case err := <-terminalErrors:
		panic(err)
	default:
	}
	return result, samples
}
func (e *environment) run(precheck bool) {
	// One explicit harness warmup initializes all paths before recording idle.
	e.round(0, 1, 1<<20, -1)
	e.report.Tracked = e.identities()
	e.remember(e.report.Tracked)
	e.report.Runtime = runtimeRecord()
	e.report.IdleBefore = e.idle(-2)
	levels := []int{1, 4, 8, 16, 16, 8, 4, 1}
	rounds := 4
	size := int64(64 << 20)
	if precheck {
		levels = []int{1}
		rounds = 1
		size = 1 << 20
	}
	round := 1
	for stage, concurrency := range levels {
		fmt.Printf("stage %d concurrency %d\n", stage, concurrency)
		entry := stageResult{Stage: stage, Concurrency: concurrency}
		var during []resourceSample
		metricsStop := make(chan struct{})
		metricsDone := make(chan error, 1)
		go func() {
			var failure error
			defer func() {
				if r := recover(); r != nil {
					failure = fmt.Errorf("resource sampler: %v", r)
				}
				metricsDone <- failure
			}()
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-metricsStop:
					return
				case <-e.ctx.Done():
					return
				case <-ticker.C:
					during = append(during, e.resources("during-stage", stage))
				}
			}
		}()
		var metricsOnce sync.Once
		var metricsError error
		stopMetrics := func() {
			metricsOnce.Do(func() {
				close(metricsStop)
				metricsError = <-metricsDone
				e.report.Resources = append(e.report.Resources, during...)
			})
			need(metricsError)
		}
		e.deferClose(stopMetrics)
		for range rounds {
			result, samples := e.round(round, concurrency, size, stage)
			round++
			entry.Rounds = append(entry.Rounds, result)
			entry.Terminal = append(entry.Terminal, samples...)
			e.report.Resources = append(e.report.Resources, e.resources("settled", stage))
		}
		stopMetrics()
		for i, sample := range entry.Terminal {
			for _, r := range entry.Rounds {
				for stream, interval := range r.DATA {
					if sample.Sent >= interval.First && sample.Received <= interval.Last {
						entry.Terminal[i].Loaded = true
						entry.Terminal[i].CoveredStreams = append(entry.Terminal[i].CoveredStreams, fmt.Sprintf("round%d/stream%d", r.Round, stream))
					}
				}
			}
			if entry.Terminal[i].Loaded {
				entry.LoadedCount++
			}
		}
		e.checkIdentities()
		e.report.Stages = append(e.report.Stages, entry)
		save(filepath.Join(e.output, "progress.json"), e.report)
	}
	e.report.IdleAfter = e.idle(-3)
	if !precheck {
		for _, level := range []int{1, 4, 8, 16} {
			count := 0
			for _, stage := range e.report.Stages {
				if stage.Concurrency == level {
					count += stage.LoadedCount
				}
			}
			ensure(count >= 20, fmt.Sprintf("insufficient loaded terminal samples at %d: %d", level, count))
		}
		e.admission()
	}
	need(terminalError(e.terminal))
}
