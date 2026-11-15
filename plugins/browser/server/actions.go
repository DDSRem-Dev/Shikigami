package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

func (worker *Worker) launchRegion(ctx context.Context, profile *epb.BrowserProfile) (*epb.RegionConfig, string, []string, error) {
	if profile.Region != nil {
		return proto.Clone(profile.Region).(*epb.RegionConfig), requestDigest(profile.Region), nil, nil
	}
	if extension, ok := worker.core.(sdk.ExtensionCore); ok && extension.Region() != nil {
		status, err := extension.Region().InspectRegion(ctx, &epb.RegionInspectRequest{})
		if err == nil {
			region := status.Desired
			if region == nil {
				region = status.Actual
			}
			if region != nil {
				region = proto.Clone(region).(*epb.RegionConfig)
				if region.Dns != nil {
					region.Dns.ManageSystem = false
					region.Dns.Servers = nil
					region.Dns.CheckDomains = nil
				}
			}
			if err := validateRegion(region); err != nil {
				return nil, "", nil, connect.NewError(connect.CodeFailedPrecondition, "inspected host settings cannot initialize Chrome; apply a UTF-8 region plan or explicitly configure profile.region: "+err.Error())
			}
			return region, status.Revision, status.Warnings, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, "", nil, err
		}
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			return nil, "", nil, err
		}
	}
	return nil, "host-unmanaged", []string{"Region plugin is unavailable: Chrome inherits the service host environment; host region and Secure DNS were not inspected or changed."}, nil
}

func (worker *Worker) StartBrowser(parent context.Context, request *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || !browserruntime.ValidID(request.ProfileId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "profile_id is required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var replay epb.BrowserStatus
	if found, err := worker.replayLocked(request.RequestId, "start", request, &replay); found || err != nil {
		return &replay, err
	}
	profile := worker.db.Profiles[request.ProfileId]
	if profile == nil {
		return nil, connect.NewError(connect.CodeNotFound, "browser profile not found")
	}
	if profile.State == "deleting" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "profile deletion is pending; retry the confirmed deletion")
	}
	if current := worker.db.Runtime; current != nil {
		snapshot, err := worker.manager.Inspect(ctx, *current)
		if err != nil {
			return nil, err
		}
		if snapshot.State == "running" {
			if current.ProfileID == request.ProfileId {
				status := worker.statusLocked(snapshot)
				if err := worker.rememberLocked(request.RequestId, "start", request, status); err != nil {
					return nil, err
				}
				return status, nil
			}
			if !request.ReplaceRunning || !request.Confirm {
				return nil, connect.NewError(connect.CodeFailedPrecondition, "switching profiles requires replace_running and explicit confirmation to stop the old browser")
			}
		}
		if worker.db.LastStartRequest == request.RequestId {
			return nil, connect.NewError(connect.CodeFailedPrecondition, "this start request already attempted the stopped/crashed instance; use a new explicit request_id for manual restart")
		}
		if err := worker.manager.Stop(ctx, *current); err != nil {
			return nil, err
		}
		worker.detachAllLocked()
		worker.db.State, worker.db.Reason = "stopped", "explicit switch/manual restart"
		if err := worker.saveLocked(); err != nil {
			return nil, err
		}
	}
	region, revision, warnings, err := worker.launchRegion(ctx, profile)
	if err != nil {
		return nil, err
	}
	instanceID := browserruntime.NewID()
	spec, err := worker.manager.Prepare(ctx, worker.stateDir, profile.Id, profile.Revision, instanceID, region, revision, worker.config.Width, worker.config.Height)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "browser runtime preparation failed: "+err.Error())
	}
	worker.db.Runtime, worker.db.State, worker.db.Reason = &spec, "starting", "explicit start intent persisted before systemd launch"
	worker.observationError = ""
	worker.db.LastStartRequest, worker.db.LastStartDigest = request.RequestId, requestDigest(request)
	if err := worker.saveLocked(); err != nil {
		return nil, err
	}
	snapshot, launchErr := worker.manager.Launch(ctx, spec)
	if launchErr != nil {
		worker.db.State, worker.db.Reason = "unknown", launchErr.Error()
		if snapshot.State == "crashed" {
			worker.db.State = "crashed"
		}
		failure := connect.NewError(connect.CodeFailedPrecondition, "Chrome start failed; manual restart required: "+launchErr.Error())
		status := &epb.BrowserStatus{ProfileId: spec.ProfileID, InstanceId: spec.InstanceID, State: worker.db.State, Reason: worker.db.Reason, RegionRevision: spec.RegionRevision}
		if err := worker.rememberOutcomeLocked(request.RequestId, "start", request, status, failure); err != nil {
			return nil, err
		}
		return nil, failure
	}
	worker.db.State, worker.db.Reason = snapshot.State, snapshot.Reason
	snapshot.Warnings = append(snapshot.Warnings, warnings...)
	status := worker.statusLocked(snapshot)
	if err := worker.rememberLocked(request.RequestId, "start", request, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (worker *Worker) StopBrowser(parent context.Context, request *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || !browserruntime.ValidID(request.ProfileId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "profile_id is required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var replay epb.BrowserStatus
	if found, err := worker.replayLocked(request.RequestId, "stop", request, &replay); found || err != nil {
		return &replay, err
	}
	if worker.db.Profiles[request.ProfileId] == nil {
		return nil, connect.NewError(connect.CodeNotFound, "browser profile not found")
	}
	status := &epb.BrowserStatus{ProfileId: request.ProfileId, State: "stopped"}
	if current := worker.db.Runtime; current != nil && current.ProfileID == request.ProfileId {
		if !request.Confirm {
			return nil, connect.NewError(connect.CodeFailedPrecondition, "explicit stop confirmation is required")
		}
		if err := worker.manager.Stop(ctx, *current); err != nil {
			return nil, err
		}
		worker.detachAllLocked()
		worker.db.State, worker.db.Reason = "stopped", "explicitly stopped; automatic restart disabled for this boot"
		status.InstanceId, status.Reason = current.InstanceID, worker.db.Reason
	}
	if err := worker.rememberLocked(request.RequestId, "stop", request, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (worker *Worker) DeleteBrowserProfile(parent context.Context, request *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	unlock, err := worker.lockFiles(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if request == nil || !browserruntime.ValidID(request.ProfileId) || !request.Confirm {
		return nil, connect.NewError(connect.CodeInvalidArgument, "confirmed profile deletion is required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var replay epb.BrowserProfile
	if found, err := worker.replayLocked(request.RequestId, "delete", request, &replay); found || err != nil {
		return &replay, err
	}
	profile := worker.db.Profiles[request.ProfileId]
	if profile == nil {
		return nil, connect.NewError(connect.CodeNotFound, "browser profile not found")
	}
	if request.ExpectedRevision != profile.Revision {
		return nil, connect.NewError(connect.CodeAborted, "profile revision changed")
	}
	if current := worker.db.Runtime; current != nil && current.ProfileID == request.ProfileId {
		if err := worker.manager.Stop(ctx, *current); err != nil {
			return nil, err
		}
		worker.detachAllLocked()
		worker.db.State, worker.db.Reason = "stopped", "profile deletion stopped verified runtime"
		if err := worker.saveLocked(); err != nil {
			return nil, err
		}
	}
	deleting := proto.Clone(profile).(*epb.BrowserProfile)
	deleting.BootStart, deleting.State = false, "deleting"
	worker.db.Profiles[profile.Id] = deleting
	if err := worker.saveLocked(); err != nil {
		return nil, err
	}
	// Files are rooted below generated profile IDs. RemoveAll on a Root never
	// traverses a symlink outside this state root.
	profiles := filepath.Join(worker.stateDir, "profiles")
	if _, err := os.Lstat(profiles); err == nil {
		if err := browserruntime.PrivateDir(profiles); err != nil {
			return nil, err
		}
		root, err := os.OpenRoot(profiles)
		if err != nil {
			return nil, err
		}
		err = root.RemoveAll(profile.Id)
		root.Close()
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	result := proto.Clone(profile).(*epb.BrowserProfile)
	result.State, result.Revision, result.BootStart = "deleted", browserruntime.NewID(), false
	delete(worker.db.Profiles, profile.Id)
	if worker.db.Runtime != nil && worker.db.Runtime.ProfileID == profile.Id {
		worker.db.Runtime = nil
		worker.db.State = "stopped"
	}
	if err := worker.rememberLocked(request.RequestId, "delete", request, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (worker *Worker) statusLocked(snapshot browserruntime.Snapshot) *epb.BrowserStatus {
	worker.control.Lock()
	defer worker.control.Unlock()
	return &epb.BrowserStatus{ProfileId: snapshot.ProfileID, InstanceId: snapshot.InstanceID, State: snapshot.State, Reason: snapshot.Reason, ControllerId: worker.controller, ControlRevision: worker.controlRevision, RegionRevision: snapshot.RegionRevision, Attachments: uint32(len(worker.attachments)), ChromeVersion: snapshot.ChromeVersion, Warnings: snapshot.Warnings}
}
func (worker *Worker) GetBrowserStatus(parent context.Context, request *epb.BrowserScope) (*epb.BrowserStatus, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || !browserruntime.ValidID(request.ProfileId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "profile_id is required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	profile := worker.db.Profiles[request.ProfileId]
	if profile == nil {
		return nil, connect.NewError(connect.CodeNotFound, "browser profile not found")
	}
	if profile.State == "deleting" {
		return &epb.BrowserStatus{ProfileId: profile.Id, State: "deleting", Reason: "confirmed deletion is pending; retry DeleteBrowserProfile"}, nil
	}
	current := worker.db.Runtime
	if current == nil || current.ProfileID != profile.Id {
		return &epb.BrowserStatus{ProfileId: profile.Id, State: "stopped"}, nil
	}
	if worker.db.State == "stopped" {
		return &epb.BrowserStatus{ProfileId: profile.Id, InstanceId: current.InstanceID, State: "stopped", Reason: worker.db.Reason}, nil
	}
	snapshot, err := worker.manager.Inspect(ctx, *current)
	if err != nil {
		return nil, err
	}
	if worker.db.State != snapshot.State || worker.db.Reason != snapshot.Reason {
		worker.db.State, worker.db.Reason = snapshot.State, snapshot.Reason
		if snapshot.State != "running" {
			worker.detachAllLocked()
		}
		if err := worker.saveLocked(); err != nil {
			return nil, err
		}
	}
	status := worker.statusLocked(snapshot)
	_, revision, warnings, err := worker.launchRegion(ctx, profile)
	if err != nil {
		return nil, err
	}
	status.RestartRequired = revision != current.RegionRevision || current.Width != worker.config.Width || current.Height != worker.config.Height
	status.Warnings = append(status.Warnings, warnings...)
	return status, nil
}
func (worker *Worker) DiagnoseBrowser(parent context.Context, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || !browserruntime.ValidID(request.ProfileId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "diagnostic profile_id is required")
	}
	worker.mu.Lock()
	current := worker.db.Runtime
	if current == nil || current.ProfileID != request.ProfileId || worker.db.State != "running" {
		worker.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, "target Chrome is not running")
	}
	spec := *current
	worker.mu.Unlock()
	report, err := worker.manager.Diagnose(ctx, spec, request)
	if err != nil {
		return nil, err
	}
	worker.appendCurrentConfiguration(ctx, report, spec)
	return report, nil
}
