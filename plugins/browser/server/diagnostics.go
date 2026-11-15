package server

import (
	"context"
	"encoding/json"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

func (worker *Worker) appendCurrentConfiguration(ctx context.Context, report *epb.DiagnosticReport, launch browserruntime.Spec) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	at := time.Now().Unix()
	values := map[string]any{"profile_id": launch.ProfileID, "diagnosed_instance_id": launch.InstanceID, "launch_profile_revision": launch.ProfileRevision, "launch_region_revision": launch.RegionRevision}
	state := "observed"
	limitation := "Server profile/configuration versions and last observed/tracked instance state, not native Chrome evidence or a fresh liveness guarantee. Metadata-only profile revision changes do not by themselves require restart; region/startup dimensions are compared separately. No existing browser is modified or restarted."
	profile := worker.db.Profiles[launch.ProfileID]
	if profile == nil {
		state = "unknown"
		limitation = "Profile was deleted during diagnosis; native observations refer only to the captured Chrome instance."
		values["restart_required"] = nil
	} else {
		values["latest_profile_revision"] = profile.Revision
		values["profile_metadata_changed"] = profile.Revision != launch.ProfileRevision
		_, revision, warnings, err := worker.launchRegion(ctx, profile)
		if err != nil {
			state = "unknown"
			values["restart_required"] = nil
			limitation = "Latest desired region revision could not be inspected; no no-restart or consistency result is inferred."
		} else {
			values["latest_region_revision"] = revision
			restart := revision != launch.RegionRevision || worker.config.Width != launch.Width || worker.config.Height != launch.Height
			values["restart_required"] = restart
			if restart {
				report.Warnings = append(report.Warnings, "Current requested region/startup settings differ from the diagnosed immutable launch record; applying them requires an explicit browser restart.")
			}
		}
		report.Warnings = append(report.Warnings, warnings...)
	}
	current := worker.db.Runtime
	stillCurrent := current != nil && current.InstanceID == launch.InstanceID && worker.db.State == "running"
	values["diagnosed_instance_still_current"] = stillCurrent
	values["current_state"] = worker.db.State
	if current != nil {
		values["current_instance_id"] = current.InstanceID
	}
	if !stillCurrent {
		if state != "unknown" {
			state = "warning"
		}
		report.Warnings = append(report.Warnings, "Browser instance/state changed during diagnosis; the native report is historical evidence for the captured instance, not proof about a replacement or stopped browser.")
	}
	encoded, _ := json.Marshal(values)
	report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.configuration.current", State: state, Value: string(encoded), Source: "server browser profile store and same-server region inspection", Vantage: "configuration:server-chrome:" + launch.ProfileID, ObservedAt: at, Limitation: limitation})
}
