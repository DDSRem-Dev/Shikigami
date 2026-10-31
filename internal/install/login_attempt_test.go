package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestInstallerRetainsAcceptedLoginAcrossSDKIntermediateStates(t *testing.T) {
	for _, state := range []struct{ state, reason string }{
		{"uninitialized", ""},
		{"needs_login", "NODE_AUTH_REQUIRED"},
		{"offline", "NODE_CONTROL_UNAVAILABLE"},
		{"denied", "NODE_APPROVAL_REQUIRED"},
		{"expired", "NODE_KEY_EXPIRED"},
	} {
		for _, url := range []bool{false, true} {
			t.Run(state.state+map[bool]string{false: "/online", true: "/url"}[url], func(t *testing.T) {
				pending := &apb.NodeStatus{State: state.state, ReasonCode: state.reason, LoginId: "existing", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}
				last := &apb.NodeStatus{State: "online"}
				if url {
					last = &apb.NodeStatus{State: state.state, ReasonCode: state.reason, LoginId: pending.LoginId, LoginExpiresAt: pending.LoginExpiresAt, LoginUrl: "https://private-login.test/attempt"}
				}
				api := &loginSequence{statuses: []*apb.NodeStatus{pending, last}}
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				got, err := guideLogin(ctx, api, pending, UserOperation{Interactive: true}, true)
				if err != nil || got != last || api.starts != 0 || api.reads != 2 {
					t.Fatalf("accepted attempt changed or returned before completion: got=%v err=%v starts=%d reads=%d", got, err, api.starts, api.reads)
				}
				if url && nodeAllowsApplication(got, false) {
					t.Fatal("pending URL admitted application traffic")
				}
			})
		}
	}
}

func TestInstallerLoginStopsAtExplicitFailureAndCallerDeadline(t *testing.T) {
	for _, status := range []*apb.NodeStatus{
		{State: "denied", ReasonCode: "INVALID_AUTH_KEY"},
		{State: "denied", ReasonCode: "NODE_APPROVAL_REQUIRED"},
		{State: "expired", ReasonCode: "NODE_KEY_EXPIRED", LoginId: "old", LoginExpiresAt: 1},
		{State: "needs_login", ReasonCode: "LOGIN_CANCELED", LoginId: "old", LoginExpiresAt: time.Now().Add(time.Minute).Unix()},
		{State: "uninitialized", ReasonCode: "LOGIN_START_FAILED", LoginId: "old", LoginExpiresAt: time.Now().Add(time.Minute).Unix()},
	} {
		t.Run(status.ReasonCode, func(t *testing.T) {
			api := &loginSequence{statuses: []*apb.NodeStatus{status}}
			got, err := guideLogin(t.Context(), api, &apb.NodeStatus{State: "authenticating"}, UserOperation{}, true)
			if err != nil || got != status || api.starts != 0 || api.reads != 1 {
				t.Fatal("terminal observation polled or restarted", got, err, api.starts, api.reads)
			}
		})
	}
	status := &apb.NodeStatus{State: "denied", ReasonCode: "NODE_APPROVAL_REQUIRED", LoginId: "pending", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}
	api := &loginSequence{statuses: []*apb.NodeStatus{status}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := guideLogin(ctx, api, status, UserOperation{Interactive: true}, true)
	if !errors.Is(err, context.DeadlineExceeded) || api.starts != 0 {
		t.Fatal("pending approval did not honor caller deadline", err, api.starts)
	}
}

func TestNativeInstallerRetainsReauthenticationURLPrivately(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user installer probe")
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []struct{ state, reason string }{{"denied", "NODE_APPROVAL_REQUIRED"}, {"expired", "NODE_KEY_EXPIRED"}} {
		t.Run(state.state, func(t *testing.T) {
			prefix := t.TempDir()
			if err := os.Mkdir(filepath.Join(prefix, "current"), 0700); err != nil {
				t.Fatal(err)
			}
			report := ProbeReport{State: "node_" + state.state, Reason: state.reason, Node: &apb.NodeStatus{State: state.state, ReasonCode: state.reason, LoginId: "pending", LoginExpiresAt: time.Now().Add(time.Minute).Unix(), LoginUrl: "https://private-login.test/attempt"}}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise Native's private helper pipe, without a running service or
			// real credentials. This helper reports an already accepted attempt.
			script := "#!/bin/sh\n/bin/cat <<'REPORT'\n" + string(data) + "\nREPORT\n"
			if err := os.WriteFile(filepath.Join(prefix, "current", "shiki-install"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			native := &Native{Prefix: prefix, Interactive: true}
			got, err := native.Probe(t.Context(), Plan{Role: "client", User: account.Username, Home: account.HomeDir, Path: "/usr/bin:/bin"})
			if err != nil || got != report.State || native.LoginURL != report.Node.LoginUrl {
				t.Fatal("interactive recovery URL lost", got, err)
			}
			public := &Result{State: got}
			native.Describe(public)
			encoded, err := json.Marshal(public)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, value := range fields {
				if value == any(report.Node.LoginUrl) {
					t.Fatal("private URL escaped into public result")
				}
			}
		})
	}
}
