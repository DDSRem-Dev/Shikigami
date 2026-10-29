package hostrotation_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

type inflightStats struct {
	done      chan struct{}
	timestamp atomic.Int64
	err       error
	callerErr error
	ended     time.Time
}

func (h *harness) observeInflightStats() *inflightStats {
	h.t.Helper()
	ctx, cancel := context.WithCancel(h.ctx)
	stream, err := h.remote.WatchStats(ctx, &pb.StatsRequest{IntervalSeconds: 1})
	if err != nil {
		cancel()
		h.t.Fatal(err)
	}
	s := &inflightStats{done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			frame, err := stream.Receive()
			if err != nil {
				s.err, s.callerErr, s.ended = err, ctx.Err(), time.Now()
				return
			}
			s.timestamp.Store(frame.Timestamp)
		}
	}()
	h.t.Cleanup(func() { cancel(); stream.Close(); <-s.done })
	h.await("original stats stream started", func(context.Context) error {
		if s.timestamp.Load() == 0 {
			return errors.New("no original stats frame")
		}
		return nil
	})
	return s
}

func (s *inflightStats) check(t *testing.T, h *harness, gate *fileTransferGate) {
	t.Helper()
	select {
	case <-s.done:
		h.logInflightRoute("stats-ended")
		t.Fatalf("unrelated original stats stream ended: code=%s error_type=%T caller_context=%v host_context=%v ended=%s last_frame=%d old_file_end=%d old_file_ctx_canceled=%v old_file_bytes=%d", connect.CodeOf(s.err), s.err, s.callerErr, h.ctx.Err(), s.ended.Format(time.RFC3339Nano), s.timestamp.Load(), gate.ended.Load(), gate.canceledAtEnd.Load(), gate.bytes.Load())
	default:
	}
}

func (h *harness) inflightRoute() *apb.ServerRecord {
	h.t.Helper()
	servers, err := h.local.ListServers(h.ctx, &pb.Empty{})
	if err != nil || len(servers.GetServers()) != 1 {
		h.t.Fatal("server identity observation", err)
	}
	return servers.Servers[0]
}

func (h *harness) logInflightRoute(label string) {
	h.t.Helper()
	route := h.inflightRoute()
	h.t.Logf("%s: daemon=%s credential_revision=%s state=%s reason=%s path=%s", label, route.Config.DaemonId, route.CredentialRevision, route.ConnectionState, route.ReasonCode, route.ConnectionPath)
}

func (h *harness) inflightReady(side string, stats *inflightStats, gate *fileTransferGate) {
	h.t.Helper()
	logged := false
	h.await(side+" lifecycle status after file cancellation", func(call context.Context) error {
		stats.check(h.t, h, gate)
		list, err := h.local.ListPlugins(call, h.target(side))
		if err != nil {
			if !logged {
				h.t.Logf("first lifecycle query failure: side=%s code=%s caller_context=%v host_context=%v old_file_end=%d old_file_ctx_canceled=%v old_file_bytes=%d", side, connect.CodeOf(err), call.Err(), h.ctx.Err(), gate.ended.Load(), gate.canceledAtEnd.Load(), gate.bytes.Load())
				h.logInflightRoute("first-query-failure")
				logged = true
			}
			return err
		}
		if len(list.Plugins) != 2 {
			return errors.New("missing worker")
		}
		for _, p := range list.Plugins {
			if p.State != "ready" || !p.Enabled {
				return fmt.Errorf("%s %s/%s", p.PluginId, p.State, p.ReasonCode)
			}
		}
		return nil
	})
}
