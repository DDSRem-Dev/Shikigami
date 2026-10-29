package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSupervisorPersistsIntentBoundsRetriesAndDeduplicatesRestart(t *testing.T) {
	var calls atomic.Int32
	options := SupervisorOptions{StateDir: filepath.Join(t.TempDir(), "plugins"), Validate: func(string, *structpb.Struct) error { return nil }, Config: func(string, *structpb.Struct) (Config, error) {
		calls.Add(1)
		return Config{}, errors.New("test missing artifact")
	}}
	manager, err := NewSupervisor(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.Start()
	list, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	var status *pb.PluginStatus
	for _, item := range list.Plugins {
		if item.PluginId == "sync" {
			status = item
		}
	}
	if status == nil || status.Enabled {
		t.Fatal("plugin defaults must be disabled")
	}
	request := &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: status.Revision, RequestId: "enable"}
	accepted, err := manager.Manage(t.Context(), request)
	if err != nil || !accepted.Enabled || accepted.State != "starting" {
		t.Fatal(accepted, err)
	}
	waitPlugin(t, manager, "sync", "PLUGIN_RESTART_LIMIT")
	if calls.Load() != 4 {
		t.Fatal("restart attempts not bounded", calls.Load())
	}
	replay, err := manager.Manage(t.Context(), request)
	if err != nil || !proto.Equal(accepted, replay) {
		t.Fatal("operation replay changed", replay, err)
	}
	changed := proto.Clone(request).(*pb.ManagePluginRequest)
	changed.Action = pb.ManagePluginRequest_DISABLE
	if _, err := manager.Manage(t.Context(), changed); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("request reuse accepted", err)
	}
	if calls.Load() != 4 {
		t.Fatal("replay restarted worker")
	}
	if _, err := manager.Manage(t.Context(), &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_RESTART, ExpectedRevision: status.Revision, RequestId: "stale"}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatal("stale revision accepted", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewSupervisor(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.Start()
	replay, err = restored.Manage(t.Context(), request)
	if err != nil || !proto.Equal(accepted, replay) {
		t.Fatal("durable replay changed", replay, err)
	}
	disabled, err := restored.Manage(t.Context(), &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_DISABLE, ExpectedRevision: accepted.Revision, RequestId: "disable"})
	if err != nil || disabled.Enabled {
		t.Fatal(disabled, err)
	}
	if err := restored.Call(t.Context(), "sync", "sync.revision.v1", nil); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("disabled worker admitted call", err)
	}
}
func waitPlugin(t *testing.T, manager *Supervisor, id, reason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := manager.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range list.Plugins {
			if status.PluginId == id && status.ReasonCode == reason {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("plugin status timeout", list)
		case <-ticker.C:
		}
	}
}
