//go:build linux

package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestRuntimeExitRecordPreservesFailureUnlessStopWasRequested(t *testing.T) {
	for _, requested := range []bool{false, true} {
		spec := readinessSpec(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if requested {
			cancel()
		}
		original := errors.New("startup failed")
		err := recordRuntimeExit(ctx, spec, "", original)
		got := exitSnapshot(spec, "missing exit")
		if requested {
			if err != nil || got.State != "stopped" || got.Reason != "browser runtime stopped by request; manual start required" {
				t.Fatalf("requested stop became crash: %#v %v", got, err)
			}
		} else if !errors.Is(err, original) || got.State != "crashed" || got.Reason != original.Error() {
			t.Fatalf("failure was suppressed without cancellation: %#v %v", got, err)
		}
	}
}

func TestExitSnapshotPreservesTerminalStateOnlyForOriginalInstance(t *testing.T) {
	spec := readinessSpec(t)
	for _, test := range []struct {
		name, state, profile, instance, want string
	}{
		{"normal-close", "stopped", spec.ProfileID, spec.InstanceID, "stopped"},
		{"legacy-crash", "crashed", spec.ProfileID, spec.InstanceID, "crashed"},
		{"wrong-profile", "stopped", "other", spec.InstanceID, "crashed"},
		{"wrong-instance", "stopped", spec.ProfileID, "other", "crashed"},
		{"wrong-mode", "stopped", spec.ProfileID, spec.InstanceID, "crashed"},
		{"live-state-invalid", "running", spec.ProfileID, spec.InstanceID, "crashed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := Snapshot{ProfileID: test.profile, InstanceID: test.instance, State: test.state, Reason: "recorded exit", RegionRevision: "untrusted-region"}
			if test.name == "wrong-mode" {
				record.Mode = ModeBrowse
			}
			if err := AtomicJSON(filepath.Join(spec.SocketDir, "exit.json"), record, MaxControlBody); err != nil {
				t.Fatal(err)
			}
			for _, absent := range []bool{true, false} {
				got, err := inspectRuntime(t.Context(), spec, func(context.Context, string) (map[string]string, error) {
					if absent {
						return map[string]string{"LoadState": "not-found"}, nil
					}
					properties := readinessProperties(spec, "failed")
					properties["Result"] = "exit-code"
					return properties, nil
				})
				accepted := test.name == "normal-close" || test.name == "legacy-crash"
				if err != nil || got.State != test.want || got.ProfileID != spec.ProfileID || got.InstanceID != spec.InstanceID || got.RegionRevision != spec.RegionRevision || (got.Reason == "recorded exit") != accepted {
					t.Fatalf("incorrect exit identity/state: %#v %v", got, err)
				}
			}
		})
	}
}
