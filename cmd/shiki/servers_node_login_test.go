package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/creack/pty"
)

func TestPrepareTailnetLoginKeepsAcceptedAttempt(t *testing.T) {
	url := "https://login.example.invalid/test-only"
	for _, scenario := range []string{"delayed_url", "existing_url", "online", "new_attempt", "noninteractive", "parent_deadline", "control_offline", "control_offline_noninteractive", "expired_active_attempt", "uninitialized_active_attempt"} {
		t.Run(scenario, func(t *testing.T) {
			pending := pendingLoginStatus()
			last := pendingLoginStatus()
			last.LoginUrl = url
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pending, pending, last}}
			wantBegins, wantReads := 0, 3
			switch scenario {
			case "uninitialized_active_attempt":
				pending.State, pending.ReasonCode = "uninitialized", ""
			case "expired_active_attempt":
				pending.State, pending.ReasonCode = "expired", "NODE_KEY_EXPIRED"
				last.State, last.ReasonCode = "expired", "NODE_KEY_EXPIRED"
			case "control_offline", "control_offline_noninteractive":
				probe.statuses, wantReads = []*apb.NodeStatus{{State: "offline", ReasonCode: "NODE_CONTROL_UNAVAILABLE"}}, 1
			case "existing_url":
				probe.statuses, wantReads = []*apb.NodeStatus{last}, 1
			case "online":
				last.State, last.LoginUrl, last.ReasonCode, last.LoginExpiresAt = "online", "", "", 0
			case "new_attempt":
				probe.statuses[0] = &apb.NodeStatus{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED"}
				wantBegins = 1
			case "noninteractive":
				wantReads = 1
			case "parent_deadline":
				probe.statuses = []*apb.NodeStatus{pending}
			}
			socket := nodeLoginSocket(t, probe)
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			var diagnostics bytes.Buffer
			a := &app{input: slave, output: slave, errorOutput: &diagnostics, timeout: time.Second}
			if scenario == "noninteractive" || scenario == "control_offline_noninteractive" {
				a.input = nil
			}
			ctx := t.Context()
			if scenario == "parent_deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 350*time.Millisecond)
				defer cancel()
			}
			err = a.prepareTailnetLogin(ctx, &client.Client{Config: &config.Client{Agent: config.Agent{Socket: socket}}})
			switch scenario {
			case "online", "control_offline", "control_offline_noninteractive":
				if err != nil || diagnostics.Len() != 0 {
					t.Fatalf("online attempt failed: %v", err)
				}
			case "noninteractive":
				if err == nil || !strings.Contains(err.Error(), "run shiki node login") || diagnostics.Len() != 0 {
					t.Fatalf("noninteractive behavior changed: %v", err)
				}
			case "parent_deadline":
				if err == nil || !strings.Contains(err.Error(), "login is pending") || diagnostics.Len() != 0 {
					t.Fatalf("pending attempt did not honor parent deadline: %v", err)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "finish node login") || !strings.Contains(diagnostics.String(), url) {
					t.Fatalf("interactive URL was not presented: %v", err)
				}
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			readsOK := probe.reads == wantReads
			if scenario == "parent_deadline" {
				readsOK = probe.reads >= 2 && probe.reads <= 5
			}
			if probe.begins != wantBegins || !readsOK || probe.cancels != 0 {
				t.Fatalf("accepted login was restarted or polled incorrectly: begins=%d reads=%d cancels=%d", probe.begins, probe.reads, probe.cancels)
			}
		})
	}
}

// The second GetStatus is an actual Unix/HTTP2 poll after an accepted attempt.
// Hold it across the parent's original 350ms deadline so the RPC error branch,
// rather than the ticker's select, must report the pending login.
func TestPrepareTailnetLoginPollErrorBoundaries(t *testing.T) {
	for _, scenario := range []string{"parent_deadline", "parent_cancel", "remote_deadline", "permission_denied", "unauthenticated", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pendingLoginStatus()}}
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			var cancel context.CancelFunc
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			var wantCode connect.Code
			var wantCause error
			switch scenario {
			case "parent_deadline":
				wantCause = context.DeadlineExceeded
			case "parent_cancel":
				wantCause = context.Canceled
			case "remote_deadline":
				wantCode = connect.CodeDeadlineExceeded
			case "permission_denied":
				wantCode = connect.CodePermissionDenied
			case "unauthenticated":
				wantCode = connect.CodeUnauthenticated
			case "unavailable":
				wantCode = connect.CodeUnavailable
			}
			probe.statusError = func(call context.Context, read int) error {
				if read != 2 {
					return nil
				}
				close(entered)
				if scenario == "parent_cancel" {
					cancel()
				}
				if wantCause != nil {
					// Keep the RPC in flight until the caller returns. The wire
					// timeout is rounded to milliseconds and may expire first.
					<-release
					return call.Err()
				}
				return connect.NewError(wantCode, "status probe rejected")
			}
			socket := nodeLoginSocket(t, probe)
			ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
			defer cancel()
			var diagnostics bytes.Buffer
			a := &app{input: slave, output: slave, errorOutput: &diagnostics, timeout: time.Second}
			err = a.prepareTailnetLogin(ctx, &client.Client{Config: &config.Client{Agent: config.Agent{Socket: socket}}})
			select {
			case <-entered:
			default:
				t.Fatal("accepted login never reached the controlled status poll")
			}
			if wantCause != nil {
				if err == nil || !strings.Contains(err.Error(), "login is pending") || !errors.Is(err, wantCause) {
					t.Fatalf("local wait ended without pending login and original cause: %v", err)
				}
			} else if connect.CodeOf(err) != wantCode || strings.Contains(err.Error(), "login is pending") || ctx.Err() != nil {
				t.Fatalf("live wait changed the actual RPC error: %v, parent=%v", err, ctx.Err())
			}
			if diagnostics.Len() != 0 {
				t.Fatal("status failure published login diagnostics")
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.begins != 0 || probe.reads != 2 || probe.cancels != 0 {
				t.Fatalf("poll failure changed accepted login: begins=%d reads=%d cancels=%d", probe.begins, probe.reads, probe.cancels)
			}
		})
	}
}

func TestTailnetLoginWaitPreservesRPCFailureAtLocalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, code := range []connect.Code{connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeUnavailable, connect.CodeInternal} {
		t.Run(code.String(), func(t *testing.T) {
			failure := connect.NewError(code, "actual RPC failure")
			if got := tailnetLoginWaitError(ctx, failure); got != failure {
				t.Fatalf("local cancellation hid actual RPC error: %v", got)
			}
		})
	}
}
