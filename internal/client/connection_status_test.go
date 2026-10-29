package client

import (
	"strings"
	"testing"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestConnectionRecoveryOffersScopedActionsWithoutChangingTrust(t *testing.T) {
	for _, test := range []struct {
		state, reason, want string
	}{
		{"offline", "RECONNECT_EXHAUSTED", "shiki connect srv_one"},
		{"auth_required", "CREDENTIAL_REJECTED", "--token-file"},
		{"incompatible", "DAEMON_ID_MISMATCH", "shiki config get --server srv_one"},
		{"ready", "", ""},
	} {
		record := &apb.ServerRecord{Config: &apb.ServerConfig{Id: "srv_one"}, ConnectionState: test.state, ReasonCode: test.reason}
		got := ConnectionRecovery(record)
		if test.want == "" && got != "" || test.want != "" && !strings.Contains(got, test.want) {
			t.Fatalf("%s/%s recovery = %q", test.state, test.reason, got)
		}
	}
	unsafe := &apb.ServerRecord{Config: &apb.ServerConfig{Id: "$(malicious)"}, ReasonCode: "RECONNECT_EXHAUSTED"}
	if strings.Contains(ConnectionRecovery(unsafe), "$(") {
		t.Fatal("remote text interpolated into a recovery command")
	}
}

func TestConnectionAttemptsDisplayOnlyOrderedRouteVocabulary(t *testing.T) {
	for _, tc := range []struct {
		routes []string
		want   string
	}{
		{nil, "Latest route selection: not reported"},
		{[]string{"tsnet"}, "Latest route selection: tsnet"},
		{[]string{"direct-tls"}, "Latest route selection: direct-tls"},
		{[]string{"tsnet", "direct-tls"}, "Latest route selection: tsnet → direct-tls"},
		{[]string{"tailscale-direct"}, "Latest route selection: not reported"},
		{[]string{"direct-tls", "tsnet"}, "Latest route selection: not reported"},
		{[]string{"tsnet", "tsnet"}, "Latest route selection: not reported"},
		{[]string{"tsnet", "direct-tls", "secret-token"}, "Latest route selection: not reported"},
		{[]string{"\x1b[2Jprivate-host"}, "Latest route selection: not reported"},
	} {
		record := &apb.ServerRecord{ConnectionAttempts: tc.routes}
		if got := ConnectionAttemptsNotice(record); got != tc.want {
			t.Fatal(tc.routes, got)
		}
		list := &apb.ServerList{Servers: []*apb.ServerRecord{record}}
		sanitized := SafeConnectionAttemptList(list)
		if strings.Contains(ConnectionAttemptsNotice(sanitized.Servers[0]), "private") || strings.Contains(ConnectionAttemptsNotice(sanitized.Servers[0]), "secret") {
			t.Fatal("unsafe diagnostic")
		}
		if len(sanitized.Servers[0].ConnectionAttempts) > 0 {
			sanitized.Servers[0].ConnectionAttempts[0] = "changed"
			if record.ConnectionAttempts[0] == "changed" {
				t.Fatal("display mutates source record")
			}
		}
	}
}
