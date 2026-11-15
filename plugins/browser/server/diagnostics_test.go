//go:build linux

package server

import (
	"context"
	"encoding/json"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

func TestDiagnosticsSeparateLatestProfileFromImmutableLaunch(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	profile := putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "start")
	worker.mu.Lock()
	launch := *worker.db.Runtime
	worker.mu.Unlock()
	updated := proto.Clone(profile).(*epb.BrowserProfile)
	updated.Region = &epb.RegionConfig{Country: "JP", Timezone: "Asia/Tokyo", Locale: "ja_JP.UTF-8", Languages: []string{"ja_JP", "ja"}}
	latest, err := worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: updated, ExpectedRevision: profile.Revision, RequestId: "change-region"})
	if err != nil {
		t.Fatal(err)
	}
	report := &epb.DiagnosticReport{}
	worker.appendCurrentConfiguration(context.Background(), report, launch)
	if len(report.Observations) != 1 {
		t.Fatal("missing current configuration evidence")
	}
	observation := report.Observations[0]
	var values struct {
		Latest  string `json:"latest_profile_revision"`
		Launch  string `json:"launch_profile_revision"`
		Restart *bool  `json:"restart_required"`
	}
	if err := json.Unmarshal([]byte(observation.Value), &values); err != nil {
		t.Fatal(err)
	}
	if observation.Vantage != "configuration:server-chrome:one" || values.Latest != latest.Revision || values.Launch != profile.Revision || values.Restart == nil || !*values.Restart {
		t.Fatalf("latest/launch conflated: %#v %s", observation, observation.Value)
	}
	if manager.launches != 1 || manager.stops != 0 {
		t.Fatal("diagnostic mismatch restarted or stopped Chrome")
	}
}
func TestDiagnosticsDeletedCurrentProfileIsUnknownNotPass(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	profile := putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "start")
	worker.mu.Lock()
	launch := *worker.db.Runtime
	worker.mu.Unlock()
	if _, err := worker.DeleteBrowserProfile(context.Background(), &epb.DeleteBrowserProfileRequest{ProfileId: "one", ExpectedRevision: profile.Revision, RequestId: "delete", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	report := &epb.DiagnosticReport{}
	worker.appendCurrentConfiguration(context.Background(), report, launch)
	if len(report.Observations) != 1 || report.Observations[0].State != "unknown" {
		t.Fatal("missing/deleted profile inferred current or passed")
	}
	var values map[string]any
	json.Unmarshal([]byte(report.Observations[0].Value), &values)
	if values["restart_required"] != nil {
		t.Fatal("unknown current settings guessed no restart required")
	}
}
