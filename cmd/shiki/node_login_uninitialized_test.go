package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestNodeLoginWaitsForAcceptedSDKStartup(t *testing.T) {
	for _, outcome := range []string{"interactive_url", "auth_key_online", "wait_timeout", "parent_deadline"} {
		t.Run(outcome, func(t *testing.T) {
			pending := pendingLoginStatus()
			pending.State, pending.ReasonCode = "uninitialized", ""
			last := pendingLoginStatus()
			last.LoginUrl = "https://login.example.invalid/startup"
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pending, last}}
			args := []string{"--wait-timeout", "2s"}
			ctx := t.Context()
			switch outcome {
			case "auth_key_online":
				last.State, last.ReasonCode, last.LoginUrl, last.LoginExpiresAt = "online", "", "", 0
				last.Addresses = []string{"100.64.0.9"}
				probe.authKey = true
				path := filepath.Join(t.TempDir(), "key")
				if err := os.WriteFile(path, []byte("unit-test-auth-key"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--auth-key-file", path)
			case "wait_timeout":
				probe.statuses = []*apb.NodeStatus{pending}
				args = []string{"--wait-timeout", "350ms"}
			case "parent_deadline":
				probe.statuses = []*apb.NodeStatus{pending}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 350*time.Millisecond)
				defer cancel()
			}
			out, _, err := runNodeLogin(ctx, nodeLoginSocket(t, probe), args...)
			if outcome == "wait_timeout" || outcome == "parent_deadline" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("startup wait lost deadline: %v", err)
				}
				probe.mu.Lock()
				defer probe.mu.Unlock()
				if probe.begins != 1 || probe.cancels != 0 || probe.reads < 2 || probe.reads > 4 {
					t.Fatal("startup attempt restarted, canceled or polled without a bound")
				}
				return
			}
			var result struct {
				State    string `json:"state"`
				LoginUrl string `json:"login_url"`
			}
			if err != nil || json.Unmarshal([]byte(out), &result) != nil {
				t.Fatalf("SDK startup returned before login progressed: %v", err)
			}
			if outcome == "interactive_url" && result.LoginUrl != last.LoginUrl || outcome == "auth_key_online" && result.State != "online" {
				t.Fatal("startup lost login result")
			}
			probe.checkCalls(t, 2)
		})
	}
}

func TestNodeLoginDoesNotWaitForUnacceptedOrFailedSDKStartup(t *testing.T) {
	for _, status := range []*apb.NodeStatus{
		{State: "uninitialized"},
		{State: "uninitialized", LoginExpiresAt: time.Now().Add(time.Minute).Unix()},
		{State: "uninitialized", LoginId: "finished"},
		{State: "uninitialized", LoginId: "expired", LoginExpiresAt: 1},
		{State: "uninitialized", ReasonCode: "NODE_START_FAILED", LoginId: "failed", LoginExpiresAt: time.Now().Add(time.Minute).Unix()},
	} {
		t.Run(status.LoginId, func(t *testing.T) {
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{status}}
			_, _, err := runNodeLogin(t.Context(), nodeLoginSocket(t, probe), "--wait-timeout", "2s")
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unaccepted startup was not terminal: %v", err)
			}
			probe.checkCalls(t, 1)
		})
	}
}
