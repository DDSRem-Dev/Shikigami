package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect/v2"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type syncResolveProbe struct {
	managementProbe
	failure     error
	preflight   error
	absent      bool
	requests    []*ppb.ResolveConflictRequest
	listCalls   int
	statusCalls int
	operation   func(context.Context) error
	remaining   time.Duration
}

func newSyncResolveProbe() *syncResolveProbe {
	return &syncResolveProbe{managementProbe: managementProbe{
		mapping:  &ppb.Mapping{Id: "map_test", ServerId: "server", ProjectId: "project", LocalRoot: "/private-local-root", SyncEnabled: true},
		conflict: &ppb.Conflict{Id: "conflict", MappingId: "map_test", Generation: "generation", Path: "private-file-name", Local: &ppb.FileVersion{Exists: true}, Remote: &ppb.FileVersion{Exists: true}},
	}}
}

func (p *syncResolveProbe) ListConflicts(context.Context, *ppb.Scope) (*ppb.ConflictList, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listCalls++
	if p.preflight != nil {
		return nil, p.preflight
	}
	if p.absent {
		return &ppb.ConflictList{}, nil
	}
	return &ppb.ConflictList{Conflicts: []*ppb.Conflict{proto.Clone(p.conflict).(*ppb.Conflict)}}, nil
}

func (p *syncResolveProbe) GetSyncState(context.Context, *ppb.Scope) (*ppb.SyncState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusCalls++
	return &ppb.SyncState{MappingId: "map_test", State: "active"}, nil
}

func (p *syncResolveProbe) ResolveConflict(ctx context.Context, request *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	p.mu.Lock()
	p.requests = append(p.requests, proto.Clone(request).(*ppb.ResolveConflictRequest))
	deadline, _ := ctx.Deadline()
	p.remaining = time.Until(deadline)
	p.mu.Unlock()
	if p.operation != nil {
		if err := p.operation(ctx); err != nil {
			return nil, err
		}
	}
	if p.failure != nil {
		return nil, p.failure
	}
	return &ppb.SyncState{MappingId: "map_test", State: "active"}, nil
}

func runSyncResolve(ctx context.Context, config string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root := newRoot(nil, &stdout, &stderr)
	base := []string{"--config", config, "sync", "resolve", "conflict", "--mapping", "map_test", "--generation", "generation", "--choice", "keep-both", "--yes"}
	root.SetArgs(append(base, args...))
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func TestSyncResolveUnknownResultPreservesIDWithoutReplay(t *testing.T) {
	for _, json := range []bool{false, true} {
		for _, code := range []connect.Code{connect.CodeCanceled, connect.CodeDeadlineExceeded, connect.CodeUnavailable} {
			for _, explicitID := range []string{"", "intentional-request"} {
				t.Run(fmt.Sprintf("json=%t/%s/explicit=%t", json, code, explicitID != ""), func(t *testing.T) {
					probe := newSyncResolveProbe()
					probe.failure = connect.NewError(code, "RPC_FAILED")
					config := managementFixtureHandlers(t, probe, nil)
					var args []string
					if json {
						args = append(args, "--json")
					}
					if explicitID != "" {
						args = append(args, "--request-id", explicitID)
					}
					out, stderr, err := runSyncResolve(t.Context(), config, args...)
					if err == nil || connect.CodeOf(err) != code || out != "" || !strings.Contains(stderr, "Resolving conflict") || !strings.Contains(stderr, "Ctrl-C") {
						t.Fatalf("code, stdout or progress contract lost: %q %q %v", out, stderr, err)
					}
					probe.mu.Lock()
					defer probe.mu.Unlock()
					if len(probe.requests) != 1 || probe.listCalls != 1 || probe.statusCalls != 0 {
						t.Fatalf("unexpected replay/recovery read: requests=%d conflicts=%d status=%d", len(probe.requests), probe.listCalls, probe.statusCalls)
					}
					id := probe.requests[0].RequestId
					if id == "" || explicitID != "" && id != explicitID {
						t.Fatal("request ID not preserved", id)
					}
					for _, message := range []string{"result may be unknown", "partially changed", "shiki sync status", "shiki sync conflicts", `--mapping "map_test"`, "both file trees", fmt.Sprintf("%q", id), "do not blindly retry or switch to a new request ID", "cannot replay a conflict that is no longer listed", "absence alone does not prove success"} {
						if !strings.Contains(err.Error(), message) {
							t.Fatalf("missing recovery guidance %q: %v", message, err)
						}
					}
					if strings.Contains(err.Error()+stderr, "private-file-name") || strings.Contains(err.Error()+stderr, "private-local-root") {
						t.Fatal("file details leaked into failure guidance")
					}
				})
			}
		}
	}
}

func TestSyncResolvePreflightDoesNotSendOrPromiseReplay(t *testing.T) {
	for _, scenario := range []string{"missing", "stale", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			probe := newSyncResolveProbe()
			switch scenario {
			case "missing":
				probe.absent = true
			case "stale":
				probe.conflict.Generation = "new-generation"
			case "deadline":
				probe.preflight = connect.NewError(connect.CodeDeadlineExceeded, "RPC_FAILED")
			}
			config := managementFixtureHandlers(t, probe, nil)
			out, stderr, err := runSyncResolve(t.Context(), config, "--json", "--request-id", "original-request")
			if err == nil || out != "" || stderr != "" || !strings.Contains(err.Error(), "request has not been sent (including any requested replay)") || !strings.Contains(err.Error(), "absent conflict does not prove") {
				t.Fatalf("preflight/replay guidance lost: %q %q %v", out, stderr, err)
			}
			if scenario == "deadline" && connect.CodeOf(err) != connect.CodeDeadlineExceeded {
				t.Fatal("preflight code lost", err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if len(probe.requests) != 0 || probe.listCalls != 1 || probe.statusCalls != 0 {
				t.Fatal("preflight bypassed or retried")
			}
		})
	}
}

func TestSyncResolveDefiniteFailureKeepsCodeAndSafeDetails(t *testing.T) {
	for _, failure := range []error{
		connect.NewError(connect.CodeAborted, "RESULT_UNKNOWN_RECONCILE_REQUIRED"),
		connect.NewError(connect.CodeFailedPrecondition, "CONFLICT_COPY_REQUIRES_KEEP_LOCAL_OR_REMOTE"),
		connect.NewError(connect.CodePermissionDenied, "denied"),
		version.MissingRemoteCapability("sync.v1"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			probe := newSyncResolveProbe()
			probe.failure = failure
			out, _, err := runSyncResolve(t.Context(), managementFixtureHandlers(t, probe, nil), "--json")
			if err == nil || out != "" || connect.CodeOf(err) != connect.CodeOf(failure) || !strings.HasPrefix(err.Error(), "ResolveConflict failed:") {
				t.Fatalf("definite rejection contract lost: %q %v", out, err)
			}
			if strings.Contains(err.Error(), "result may be unknown") || strings.Contains(err.Error(), "request ID") {
				t.Fatal("explicit rejection mislabeled as uncertain transport", err)
			}
			if version.SafeDetail(failure) != nil && version.Describe(err, "fixture") != version.Describe(failure, "fixture") {
				t.Fatal("safe diagnostic detail lost")
			}
		})
	}
}

func TestSyncResolveDefaultBudgetAndExplicitFiveSeconds(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			probe := newSyncResolveProbe()
			if !explicit {
				// Exercise the former five-second cutoff through the real IPC.
				probe.operation = func(ctx context.Context) error {
					timer := time.NewTimer(5100 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-timer.C:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			var args = []string{"--json"}
			want := 15 * time.Minute
			if explicit {
				args = append(args, "--timeout", "5s")
				want = 5 * time.Second
			}
			out, stderr, err := runSyncResolve(t.Context(), managementFixtureHandlers(t, probe, nil), args...)
			result := new(ppb.SyncState)
			if err != nil || protojson.Unmarshal([]byte(out), result) != nil || result.State != "active" || !strings.Contains(stderr, "Resolving conflict") {
				t.Fatalf("bounded operation failed or JSON polluted: %q %q %v", out, stderr, err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.remaining > want || probe.remaining < want-time.Second || len(probe.requests) != 1 {
				t.Fatalf("operation budget %s, want %s; calls %d", probe.remaining, want, len(probe.requests))
			}
		})
	}
}

func TestSyncResolveCallerCancellationAndExplicitShortTimeout(t *testing.T) {
	for _, scenario := range []string{"cancel", "deadline", "explicit-timeout"} {
		t.Run(scenario, func(t *testing.T) {
			probe := newSyncResolveProbe()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			args := []string{"--json", "--request-id", "canceled-request"}
			want := connect.CodeDeadlineExceeded
			if scenario == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, time.Second)
				defer stop()
			}
			if scenario == "explicit-timeout" {
				args = append(args, "--timeout", "100ms")
			}
			if scenario == "cancel" {
				want = connect.CodeCanceled
			}
			done := make(chan struct{})
			probe.operation = func(ctx context.Context) error {
				defer close(done)
				if scenario == "cancel" {
					cancel()
				}
				<-ctx.Done()
				return ctx.Err()
			}
			out, _, err := runSyncResolve(ctx, managementFixtureHandlers(t, probe, nil), args...)
			if err == nil || connect.CodeOf(err) != want || out != "" || !strings.Contains(err.Error(), `"canceled-request"`) {
				t.Fatalf("cancellation code or recovery lost: %q %v", out, err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("server operation outlived caller cancellation")
			}
		})
	}
}

func TestSyncResolveErrorPreservesLocalCauseAndEscapesIdentifiers(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := syncResolveError(cause, "mapping\n", "request\x1b[31m")
		if !errors.Is(err, cause) || strings.ContainsAny(err.Error(), "\n\x1b") || !strings.Contains(err.Error(), `"mapping\n"`) || !strings.Contains(err.Error(), `"request\x1b[31m"`) {
			t.Fatal("local cause or safe identifier rendering lost", err)
		}
	}
}

func TestSyncResolveProgressStopsAfterCompletionOrCancellation(t *testing.T) {
	for _, scenario := range []string{"complete", "cancel", "noninteractive"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var stderr bytes.Buffer
				stop := syncResolveProgress(ctx, &stderr, scenario != "noninteractive")
				time.Sleep(11 * time.Second)
				synctest.Wait()
				if got := strings.Contains(stderr.String(), "Still resolving conflict"); got != (scenario != "noninteractive") {
					t.Fatal("incorrect heartbeat policy", stderr.String())
				}
				if scenario == "cancel" {
					cancel()
					synctest.Wait()
				}
				stop()
				before := stderr.String()
				time.Sleep(30 * time.Second)
				synctest.Wait()
				if stderr.String() != before {
					t.Fatal("progress continued after return")
				}
			})
		})
	}
}

func TestClientOptionsPreserveTimeoutIntentAcrossFrontends(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		for _, auto := range []bool{false, true} {
			a := &app{timeout: 5 * time.Second, timeoutSet: func() bool { return explicit }}
			opts := a.clientOptions(auto) // Shared by CLI, dashboard, agent start and reconnect.
			if opts.Timeout != 5*time.Second || opts.TimeoutExplicit != explicit || opts.AutoStart != auto || opts.RecoverCrashLoop || opts.ExplicitStart {
				t.Fatalf("frontend options changed intent: %+v", opts)
			}
		}
	}
}
