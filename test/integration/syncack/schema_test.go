package syncack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
)

func TestFutureSyncJournalFailsHostReadiness(t *testing.T) {
	root, binaries := ackArtifacts(t)
	h := startHosts(t, filepath.Join(root, "f"), binaries, nil)
	for _, side := range []string{"server", "client"} {
		h.manage(side, "sync", pb.ManagePluginRequest_ENABLE, nil)
		h.ready(side)
	}
	old := h.workerPID("client")
	h.workerPID("server")
	mapping, _ := h.mapping()
	h.awaitBaseline(mapping, commonContents)
	h.manage("client", "sync", pb.ManagePluginRequest_DISABLE, nil)
	resources.Reaped(t, old)
	ledger := filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json")
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["schema_version"] = json.RawMessage(`3`)
	future, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, future, 0600); err != nil {
		t.Fatal(err)
	}
	h.manage("client", "sync", pb.ManagePluginRequest_ENABLE, nil)
	var reason string
	h.await("future journal rejected by real worker Init", func(ctx context.Context) error {
		list, err := h.local.ListPlugins(ctx, h.target("client"))
		if err != nil {
			return err
		}
		for _, status := range list.Plugins {
			if status.PluginId != "sync" {
				continue
			}
			if status.State != "failed" {
				return errors.New("awaiting failed initialization")
			}
			if !status.Enabled || status.Protocol != nil {
				t.Fatal("future ledger worker advertised readiness or changed desired enablement")
			}
			reason = status.ReasonCode
			return nil
		}
		return errors.New("missing sync plugin")
	})
	after, err := os.ReadFile(ledger)
	if err != nil || !bytes.Equal(after, future) {
		t.Fatal("failed worker rewrote future ledger", err)
	}
	t.Logf("future journal rejected: schema=3 expected_schema=2 state=failed reason=%s protocol_absent=true desired_enabled=true state_bytes_unchanged=true", reason)
}
