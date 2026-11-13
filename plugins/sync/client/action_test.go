package client

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

func TestConflictActionDoesNotReportOversizedListAsEmpty(t *testing.T) {
	fixture := newJournalFixture(t)
	// Each path fits the supported filesystem limits, but the aggregate
	// conflict response exceeds the RPC budget. The action must preserve that
	// failure rather than claiming a conflict-free mapping.
	for i := range 6000 {
		path := fmt.Sprintf("%s/%04d", strings.Repeat("x", 180), i)
		fixture.task.data.Conflicts[path] = &ppb.Conflict{
			Id: fmt.Sprintf("conflict_%d", i), MappingId: "map_test",
			Path: path, Generation: "generation_test",
		}
	}
	for _, jsonOut := range []string{"false", "true"} {
		t.Run(jsonOut, func(t *testing.T) {
			result, err := fixture.task.worker.InvokeAction(t.Context(), &sdk.ActionRequest{
				ActionID: "list-conflicts", Flags: map[string]string{"json": jsonOut},
			})
			if connect.CodeOf(err) != connect.CodeResourceExhausted || result != nil {
				t.Fatalf("unavailable conflict list reported as successful: result=%v err=%v", result, err)
			}
		})
	}
}
