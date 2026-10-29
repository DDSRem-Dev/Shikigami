package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestConnectionAttemptsConnectFailureReportsObservedSelection(t *testing.T) {
	for _, state := range []string{"auth_required", "incompatible", "exhausted"} {
		for _, tc := range []struct {
			name   string
			routes []string
			want   string
		}{
			{"reported", []string{"tsnet", "direct-tls"}, "Latest route selection: tsnet → direct-tls"},
			{"old-agent", nil, "Latest route selection: not reported"},
			{"untrusted", []string{"private-host-secret"}, "Latest route selection: not reported"},
		} {
			t.Run(state+"/"+tc.name, func(t *testing.T) {
				record := &apb.ServerRecord{Config: &apb.ServerConfig{Id: "srv_one"}, CredentialPresent: true, CredentialRevision: "cred_one", ConnectionState: state, ConnectionAttempts: tc.routes}
				if state == "exhausted" {
					record.ReasonCode = "RECONNECT_EXHAUSTED"
				}
				source := &statusSequence{values: []*apb.ServerRecord{record}}
				_, err := awaitServerReady(t.Context(), source, record)
				if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-host-secret") || source.calls != 1 {
					t.Fatal("terminal verification omitted or invented route attempts", err, source.calls)
				}
			})
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := &statusSequence{}
	if _, err := awaitServerReady(ctx, source, nil); !errors.Is(err, context.Canceled) || source.calls != 0 {
		t.Fatal("cancellation started an extra diagnostic RPC", err, source.calls)
	}
}

func TestConnectionAttemptsCLIListAndDoctor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		routes []string
		want   string
	}{
		{"reported", []string{"tsnet", "direct-tls"}, "Latest route selection: tsnet → direct-tls"},
		{"old-agent", nil, "Latest route selection: not reported"},
		{"untrusted", []string{"tsnet", "private-host-secret"}, "Latest route selection: not reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &managementProbe{server: &apb.ServerRecord{Config: &apb.ServerConfig{Id: "server", Name: "test"}, ConnectionState: "offline", ConnectionAttempts: tc.routes}}
			path := managementFixture(t, p)
			for _, args := range [][]string{{"servers", "list"}, {"doctor"}} {
				var out, stderr bytes.Buffer
				cmd := newRoot(nil, &out, &stderr)
				cmd.SetArgs(append([]string{"--config", path}, args...))
				if err := cmd.ExecuteContext(t.Context()); err != nil {
					t.Fatal(args, err)
				}
				if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), "private-host-secret") {
					t.Fatal("human diagnostics", out.String())
				}
				data, err := runManagement(t, path, args...)
				if err != nil || strings.Contains(data, "private-host-secret") {
					t.Fatal("structured diagnostics", data, err)
				}
				if tc.name == "reported" && (!strings.Contains(data, "connection_attempts") || !strings.Contains(data, "direct-tls")) {
					t.Fatal("JSON omitted actual diagnostic", data)
				}
			}
		})
	}
}
