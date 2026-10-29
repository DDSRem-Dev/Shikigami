package config

import (
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
)

func TestSyncConfigurationBoundsAndSides(t *testing.T) {
	for _, parse := range []func(*structpb.Struct) (SyncConfig, error){ParseSyncClient, ParseSyncServer} {
		c, err := parse(nil)
		if err != nil || c.MaxAutoSyncSize != 10<<20 {
			t.Fatal("default limit changed")
		}
		for _, value := range []any{0, -1, 1.5, "1024", float64(MaxAutoSyncSize) + 1} {
			values, _ := structpb.NewStruct(map[string]any{"max_auto_sync_size": value})
			if _, err = parse(values); err == nil {
				t.Fatalf("invalid size accepted: %v", value)
			}
		}
		values, _ := structpb.NewStruct(map[string]any{"max_auto_sync_size": float64(MaxAutoSyncSize)})
		if c, err = parse(values); err != nil || c.MaxAutoSyncSize != MaxAutoSyncSize {
			t.Fatal("valid size rejected")
		}
	}
	values, _ := structpb.NewStruct(map[string]any{"scan_interval_ms": 100})
	if _, err := ParseSyncServer(values); err == nil {
		t.Fatal("server accepted unused scan interval")
	}
}
