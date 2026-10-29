package hostrotation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
)

type workerSnapshot struct {
	pids      map[string]int
	sockets   []string
	revisions map[string]string
}

func (h *harness) workers(side string) workerSnapshot {
	h.t.Helper()
	s := workerSnapshot{pids: map[string]int{}, revisions: map[string]string{}}
	for _, id := range []string{"sync", "port"} {
		binary := filepath.Join(h.root, side, "plugins", id+".race")
		pids := resources.Children(h.t, binary, 1)
		s.pids[id] = pids[0]
		h.pids = append(h.pids, pids[0])
		s.revisions[id] = h.plugin(side, id).Revision
	}
	var err error
	s.sockets, err = filepath.Glob(filepath.Join(h.runDir(side), "p-*", "core.sock"))
	if err != nil || len(s.sockets) != 2 {
		h.t.Fatal("expected two private CoreAPI sockets", side, s.sockets, err)
	}
	return s
}

func (h *harness) waitRotation(old workerSnapshot, enabledAt time.Time) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	unavailable := map[string]bool{}
	var last error
	for ctx.Err() == nil {
		list, err := h.local.ListPlugins(ctx, h.target(h.side))
		last = err
		ready := err == nil && len(list.GetPlugins()) == 2
		if err == nil {
			for _, status := range list.Plugins {
				if !status.Enabled || status.Revision != old.revisions[status.PluginId] {
					h.t.Fatal("expiry changed durable plugin intent")
				}
				if status.State != "ready" {
					unavailable[status.PluginId] = true
					ready = false
					last = fmt.Errorf("%s %s/%s", status.PluginId, status.State, status.ReasonCode)
					if status.Protocol != nil {
						h.t.Fatal("unavailable worker advertised a protocol")
					}
				}
			}
		}
		removed := true
		for _, path := range old.sockets {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				removed = false
			}
		}
		if removed && ready {
			if time.Since(enabledAt) < credentialLifetime {
				h.t.Fatal("worker replaced before its actual configured expiry")
			}
			if !unavailable["sync"] || !unavailable["port"] {
				h.t.Fatal("rotation did not expose each unavailable worker before recovery", unavailable)
			}
			for _, pid := range old.pids {
				resources.Reaped(h.t, pid)
			}
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(20 * time.Millisecond):
		}
	}
	h.t.Fatal("natural credential rotation did not complete", ctx.Err(), last)
}

func (h *harness) durable(w *syncWork, p *portWork) {
	h.t.Helper()
	// Read the atomic committed snapshot; never open a second writer or edit it.
	data, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "resources", "state.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	var state storage.Snapshot
	if err := json.Unmarshal(data, &state); err != nil {
		h.t.Fatal(err)
	}
	for _, want := range []*ppb.Mapping{w.active, w.paused} {
		got := new(ppb.Mapping)
		if err := state.Get("mappings", want.Id, got); err != nil || !proto.Equal(want, got) {
			h.t.Fatal("mapping intent not durably preserved", err)
		}
	}
	active := new(ppb.ForwardSpec)
	if err := state.Get("forwards", p.active.Id, active); err != nil || !proto.Equal(p.active, active) {
		h.t.Fatal("enabled forward intent changed", err)
	}
	if _, exists := state.Buckets["forwards"][p.stopped.Id]; exists {
		h.t.Fatal("completed stop retained a forward spec")
	}
	receipt, exists := state.Requests["StopForward\x00"+p.stopRequest]
	if !exists || receipt.Status != "succeeded" || receipt.ResourceID != p.stopped.Id {
		h.t.Fatal("explicit stop receipt was not durably recorded")
	}
}

func exerciseRotation(h *harness) {
	t := h.t
	target := startEcho(t)
	h.configurePorts(target)
	enabledAt := time.Now()
	// Start the side with the normal zero-value lifetime first, then the side
	// whose actual credential timer this scenario expires. No restart RPCs.
	stable := "server"
	if h.side == "server" {
		stable = "client"
	}
	for _, side := range []string{stable, h.side} {
		if side == h.side {
			enabledAt = time.Now()
		}
		for _, id := range []string{"sync", "port"} {
			h.manage(side, id, pb.ManagePluginRequest_ENABLE, nil)
		}
		h.ready(side)
	}
	oldStable := h.workers(stable)
	old := h.workers(h.side)
	work := h.prepareSync()
	ports := h.preparePort(target)
	h.durable(work, ports)
	if time.Since(enabledAt) > credentialLifetime-3*time.Second {
		t.Fatal("business setup left insufficient time before credential expiry")
	}
	initialServers, err := h.local.ListServers(h.ctx, &pb.Empty{})
	if err != nil || len(initialServers.GetServers()) != 1 {
		t.Fatal(err)
	}
	initialServer := proto.Clone(initialServers.Servers[0]).(*apb.ServerRecord)
	statsCtx, stopStats := context.WithCancel(h.ctx)
	stats, err := h.remote.WatchStats(statsCtx, &pb.StatsRequest{IntervalSeconds: 1})
	if err != nil {
		stopStats()
		t.Fatal(err)
	}
	var statsTimestamp atomic.Int64
	var statsErr error
	var statsEnded time.Time
	var statsContextError, hostContextError error
	statsStarted := time.Now()
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		for {
			frame, err := stats.Receive()
			if err != nil {
				statsErr = err
				statsEnded = time.Now()
				statsContextError = statsCtx.Err()
				hostContextError = h.ctx.Err()
				return
			}
			statsTimestamp.Store(frame.Timestamp)
		}
	}()
	defer func() { stopStats(); stats.Close(); <-statsDone }()
	statsAfter := func(previous int64) {
		h.await("unrelated live stats stream", func(context.Context) error {
			select {
			case <-statsDone:
				servers, serverErr := h.local.ListServers(h.ctx, &pb.Empty{})
				deadline, _ := h.ctx.Deadline()
				h.t.Fatalf("stats stream ended during plugin rotation: error=%v type=%T started=%s ended=%s elapsed=%s stats_context_error=%v host_context_error=%v host_deadline=%s last_frame_unix=%d servers=%v server_query_error=%v", statsErr, statsErr, statsStarted.Format(time.RFC3339Nano), statsEnded.Format(time.RFC3339Nano), statsEnded.Sub(statsStarted), statsContextError, hostContextError, deadline.Format(time.RFC3339Nano), statsTimestamp.Load(), servers, serverErr)
			default:
			}
			if statsTimestamp.Load() <= previous {
				return errors.New("awaiting fresh stats frame")
			}
			return nil
		})
	}
	statsAfter(0)
	h.waitRotation(old, enabledAt)
	replacement := h.workers(h.side)
	stillStable := h.workers(stable)
	for _, id := range []string{"sync", "port"} {
		if replacement.pids[id] == old.pids[id] {
			t.Fatal("old process masqueraded as replacement")
		}
		if stillStable.pids[id] != oldStable.pids[id] {
			t.Fatal("unrelated host worker restarted")
		}
		if replacement.revisions[id] != old.revisions[id] {
			t.Fatal("natural recovery changed plugin intent")
		}
		t.Logf("%s %s: old_pid=%d new_pid=%d old_reaped=true", h.side, id, old.pids[id], replacement.pids[id])
	}
	for _, path := range replacement.sockets {
		if slices.Contains(old.sockets, path) {
			t.Fatal("expired CoreAPI endpoint reused")
		}
	}
	if !slices.Equal(stillStable.sockets, oldStable.sockets) {
		t.Fatal("unrelated host CoreAPI endpoints changed")
	}
	h.verifyPort(ports)
	h.verifySync(work)
	h.durable(work, ports)
	// Require a frame generated after recovery, not an earlier buffered frame.
	statsAfter(time.Now().Unix())
	servers, err := h.local.ListServers(h.ctx, &pb.Empty{})
	if err != nil || len(servers.GetServers()) != 1 {
		t.Fatal(err)
	}
	after := servers.Servers[0]
	if after.Config.DaemonId != initialServer.Config.DaemonId || after.ConnectionState != "ready" || after.ConnectionPath != "direct-tls" || after.CredentialRevision != initialServer.CredentialRevision {
		t.Fatal("plugin rotation changed remote identity, credential or route")
	}
	if time.Since(enabledAt) >= 2*credentialLifetime {
		t.Fatal("scenario exceeded its single natural-expiry window")
	}
	final := h.workers(h.side)
	for _, id := range []string{"sync", "port"} {
		if final.pids[id] != replacement.pids[id] {
			t.Fatal("unexpected second replacement")
		}
	}
	t.Logf("%s: elapsed_since_enable=%s old_sockets_removed=2 new_sockets=2 default_lifetime_peer_unchanged=true real_agent_durable_intent=true daemon_id_and_tls_route_preserved=true", h.side, time.Since(enabledAt))
}
