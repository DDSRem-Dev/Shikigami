package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

type statusSequence struct {
	values []*apb.ServerRecord
	calls  int
}

func (s *statusSequence) ListServers(context.Context, *pb.Empty) (*apb.ServerList, error) {
	index := min(s.calls, len(s.values)-1)
	s.calls++
	return &apb.ServerList{Servers: []*apb.ServerRecord{proto.Clone(s.values[index]).(*apb.ServerRecord)}}, nil
}

func TestConnectionWaitRequiresAuthenticatedPinnedIdentityWithoutBusinessPermission(t *testing.T) {
	expected := &apb.ServerRecord{Config: &apb.ServerConfig{Id: "srv_one", Name: "one"}, CredentialPresent: true, CredentialRevision: "cred_one"}
	ready := proto.Clone(expected).(*apb.ServerRecord)
	ready.Config.DaemonId = "daemon_one"
	ready.ConnectionState = "ready"
	ready.RemoteProtocol = &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_one", GrantedPermissions: []string{"system.stats"}}
	source := &statusSequence{values: []*apb.ServerRecord{expected, ready}}
	if result, err := awaitServerReady(t.Context(), source, expected); err != nil || result.RemoteProtocol.DaemonId != "daemon_one" || source.calls != 2 {
		t.Fatal("authenticated restricted connection was rejected", result, err)
	}
	for _, test := range []struct {
		name string
		edit func(*apb.ServerRecord)
	}{
		{"unverified identity", func(r *apb.ServerRecord) { r.Config.DaemonId = "" }},
		{"missing agreement", func(r *apb.ServerRecord) { r.RemoteProtocol = nil }},
		{"mismatched identity", func(r *apb.ServerRecord) { r.RemoteProtocol.DaemonId = "daemon_other" }},
		{"credential replacement", func(r *apb.ServerRecord) { r.CredentialRevision = "cred_other" }},
		{"settings replacement", func(r *apb.ServerRecord) { r.Config.TailscaleAddress = "other:9527" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := proto.Clone(ready).(*apb.ServerRecord)
			test.edit(changed)
			if _, err := awaitServerReady(t.Context(), &statusSequence{values: []*apb.ServerRecord{changed}}, expected); err == nil {
				t.Fatal("unverified or concurrently changed connection accepted")
			}
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := awaitServerReady(ctx, &statusSequence{values: []*apb.ServerRecord{expected}}, expected); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("caller deadline was extended", err)
	}
	exhausted := proto.Clone(expected).(*apb.ServerRecord)
	exhausted.ReasonCode = "RECONNECT_EXHAUSTED"
	if _, err := awaitServerReady(t.Context(), &statusSequence{values: []*apb.ServerRecord{exhausted}}, expected); err == nil || !strings.Contains(err.Error(), "shiki connect srv_one") {
		t.Fatal("exhaustion did not offer an executable recovery action", err)
	}
}
