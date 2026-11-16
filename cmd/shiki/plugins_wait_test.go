package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

type transientPluginStatusProbe struct {
	managementProbe
	failure   connect.Code
	mutations atomic.Int32
	status    atomic.Int32
}

func (p *transientPluginStatusProbe) ManagePlugin(ctx context.Context, request *apb.ManagePluginRequest) (*pb.PluginStatus, error) {
	p.mutations.Add(1)
	return p.managementProbe.ManagePlugin(ctx, request)
}

func (p *transientPluginStatusProbe) ListPlugins(ctx context.Context, target *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	result, err := p.managementProbe.ListPlugins(ctx, target)
	if err == nil && result.Plugins[0].State == "ready" && p.status.Add(1) <= 2 {
		return nil, connect.NewError(p.failure, "REMOTE_RPC_FAILED")
	}
	return result, err
}

func TestPluginConfigRecoversStatusWithoutReplayingMutation(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeUnavailable, connect.CodeDeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			p := &transientPluginStatusProbe{failure: code}
			path := managementFixtureHandlers(t, p, nil)
			values := filepath.Join(t.TempDir(), "port.json")
			if err := os.WriteFile(values, []byte(`{"allow_ports":[3000,40000]}`), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runManagement(t, path, "plugins", "config", "port-forward", "--side", "local", "--request-id", "request", "--file", values)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(output), &result); err != nil || result["state"] != "ready" || result["revision"] != "new" {
				t.Fatalf("completion was not confirmed at the accepted revision: %s %v", output, err)
			}
			if p.mutations.Load() != 1 || p.status.Load() != 3 {
				t.Fatalf("mutation calls=%d status reads=%d", p.mutations.Load(), p.status.Load())
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.managed.Request.Action != pb.ManagePluginRequest_SET_CONFIG || p.managed.Request.ExpectedRevision != "old" || len(p.managed.Request.Config.GetFields()["allow_ports"].GetListValue().GetValues()) != 2 {
				t.Fatalf("configuration mutation changed: %v", p.managed)
			}
		})
	}
}

func TestPluginWaitStopsOnPermanentStatusFailure(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeFailedPrecondition, connect.CodeAborted, connect.CodeNotFound, connect.CodeInvalidArgument, connect.CodeResourceExhausted, connect.CodeInternal, connect.CodeCanceled} {
		t.Run(code.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			accepted := &pb.PluginStatus{Revision: "accepted", State: "starting", Enabled: true}
			failure := connect.NewError(code, "terminal status error")
			reads := 0
			status, err := waitForPlugin(ctx, accepted, func(context.Context) (*pb.PluginStatus, error) {
				reads++
				return nil, failure
			})
			if status != accepted || !errors.Is(err, failure) || reads != 1 {
				t.Fatalf("terminal error was retried or replaced: status=%v err=%v reads=%d", status, err, reads)
			}
		})
	}
}

func TestPluginWaitStillRejectsChangedRevisionAfterTransientFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	reads := 0
	status, err := waitForPlugin(ctx, &pb.PluginStatus{Revision: "accepted", State: "starting", Enabled: true}, func(context.Context) (*pb.PluginStatus, error) {
		reads++
		if reads == 1 {
			return nil, connect.NewError(connect.CodeUnavailable, "REMOTE_RPC_FAILED")
		}
		return &pb.PluginStatus{Revision: "another-change", State: "ready", Enabled: true}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "plugin changed while waiting") || status.Revision != "another-change" || reads != 2 {
		t.Fatalf("another change was accepted: status=%v err=%v reads=%d", status, err, reads)
	}
}

func TestPluginWaitTransientFailureHonorsCancellationAndDeadline(t *testing.T) {
	for _, mode := range []string{"canceled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			budget := time.Second
			if mode == "canceled" {
				budget = 5 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			deadline, _ := ctx.Deadline()
			reads := 0
			accepted := &pb.PluginStatus{Revision: "accepted", State: "starting", Enabled: true}
			status, err := waitForPlugin(ctx, accepted, func(call context.Context) (*pb.PluginStatus, error) {
				if actual, ok := call.Deadline(); !ok || actual != deadline {
					t.Fatal("status retry changed the caller's deadline")
				}
				reads++
				if reads == 1 {
					return nil, connect.NewError(connect.CodeUnavailable, "REMOTE_RPC_FAILED")
				}
				if mode == "canceled" {
					cancel()
				}
				<-call.Done()
				// Even a transient error racing cancellation must not restart
				// the wait budget or replace the caller's terminal cause.
				return nil, connect.NewError(connect.CodeUnavailable, "REMOTE_RPC_FAILED")
			})
			cause := context.Canceled
			if mode == "deadline" {
				cause = context.DeadlineExceeded
			}
			if status != accepted || !errors.Is(err, cause) || !strings.Contains(err.Error(), "accepted but completion was not confirmed") || reads != 2 {
				t.Fatalf("wait escaped its lifetime: status=%v err=%v reads=%d", status, err, reads)
			}
		})
	}
}
