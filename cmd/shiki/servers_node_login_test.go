package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/creack/pty"
)

func TestPrepareTailnetLoginKeepsAcceptedAttempt(t *testing.T) {
	url := "https://login.example.invalid/test-only"
	for _, scenario := range []string{"delayed_url", "existing_url", "online", "new_attempt", "noninteractive", "parent_deadline", "control_offline", "control_offline_noninteractive"} {
		t.Run(scenario, func(t *testing.T) {
			pending := pendingLoginStatus()
			last := pendingLoginStatus()
			last.LoginUrl = url
			probe := &nodeLoginProbe{statuses: []*apb.NodeStatus{pending, pending, last}}
			wantBegins, wantReads := 0, 3
			switch scenario {
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
