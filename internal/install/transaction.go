package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Plan struct {
	Role         string `json:"role"`
	User         string `json:"user"`
	Config       string `json:"config"`
	StateDir     string `json:"state_dir"`
	TailscaleDir string `json:"tailscale_dir"`
	PluginsDir   string `json:"plugins_dir"`
	BinDir       string `json:"bin_dir"`
	ServiceFile  string `json:"service_file"`
	Home         string `json:"home"`
	Path         string `json:"path"`
	Port         int    `json:"port"`
	ProjectsDir  string `json:"projects_dir,omitempty"`
	Claude       string `json:"claude,omitempty"`
	TLSAddress   string `json:"tls_address,omitempty"`
	TLSCert      string `json:"tls_cert,omitempty"`
	TLSKey       string `json:"tls_key,omitempty"`
}
type Selection struct {
	Release   string `json:"release"`
	Version   string `json:"version"`
	Checksums string `json:"checksums_sha256"`
}
type Receipt struct {
	Format        int        `json:"format"`
	Plan          Plan       `json:"plan"`
	Active        Selection  `json:"active"`
	Previous      Selection  `json:"previous"`
	Pending       *Selection `json:"pending,omitempty"`
	PendingAction string     `json:"pending_action,omitempty"`
	Backup        string     `json:"backup,omitempty"`
}
type Result struct {
	State                  string   `json:"state"`
	Version                string   `json:"version,omitempty"`
	Prefix                 string   `json:"prefix"`
	Backup                 string   `json:"backup,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	ReviewFile             string   `json:"review_file,omitempty"`
	NodeState              string   `json:"node_state,omitempty"`
	NodeReason             string   `json:"node_reason,omitempty"`
	NodeAddresses          []string `json:"node_addresses,omitempty"`
	ApplicationState       string   `json:"application_state,omitempty"`
	ExternalPeerAcceptance string   `json:"external_peer_acceptance,omitempty"`
	DaemonID               string   `json:"daemon_id,omitempty"`
	Capabilities           []string `json:"capabilities,omitempty"`
	ClaudeVersion          string   `json:"claude_version,omitempty"`
	ClaudeLoginState       string   `json:"claude_login_state,omitempty"`
	TokenIssued            bool     `json:"token_issued,omitempty"`
	TokenDestination       string   `json:"token_destination,omitempty"`
}

// Lifecycle implementations must stop and join hosts before Backup. They may
// initialize private identity only in Prepare, and must not claim readiness from
// a service manager's process status alone.
type Lifecycle interface {
	Stop(context.Context, Plan) error
	Backup(context.Context, Plan, string) (string, error)
	Prepare(context.Context, Plan, string) error
	Start(context.Context, Plan) error
	Probe(context.Context, Plan) (string, error)
	RemoveService(context.Context, Plan) error
}

func (a *Artifacts) Receipt() (*Receipt, error) {
	var r Receipt
	err := a.readJSON("installation.json", &r)
	if errors.Is(err, os.ErrNotExist) {
		return &Receipt{Format: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Format != 1 || (r.Active.Release != "" && !validReleasePath(r.Active.Release)) || (r.Previous.Release != "" && !validReleasePath(r.Previous.Release)) || (r.Pending != nil && !validReleasePath(r.Pending.Release)) {
		return nil, errors.New("invalid installation receipt")
	}
	if (r.Pending == nil && r.PendingAction != "") || (r.Pending != nil && r.PendingAction != "install" && r.PendingAction != "rollback" && r.PendingAction != "uninstall") {
		return nil, errors.New("invalid pending installation action")
	}
	return &r, nil
}

type startupGate interface {
	Acquire(context.Context, Plan) (func(), error)
}

func acquireStartup(ctx context.Context, plan Plan, lifecycle Lifecycle) (func(), error) {
	if gate, ok := lifecycle.(startupGate); ok {
		return gate.Acquire(ctx, plan)
	}
	return func() {}, nil
}
func restartAfterFailure(ctx context.Context, plan Plan, active Selection, lifecycle Lifecycle, cause error) error {
	if active.Release == "" {
		return cause
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	return errors.Join(cause, lifecycle.Start(recovery, plan))
}

// A rollback may begin with current selecting an interrupted, uncommitted
// release. If backup/journaling fails, do not start that release or implicitly
// switch to an older schema. Keep the receipt and selection for explicit retry.
func (a *Artifacts) restartAfterRollbackFailure(ctx context.Context, r *Receipt, lifecycle Lifecycle, cause error) error {
	if r.Active.Release == "" {
		return cause
	}
	current, err := a.Current()
	if err != nil {
		return errors.Join(cause, err)
	}
	if current != r.Active.Release {
		return errors.Join(cause, errors.New("active selection differs from committed receipt; service remains stopped; explicit rollback required"))
	}
	if _, err := Verify(filepath.Join(a.Prefix, current), r.Active.Version, r.Active.Checksums); err != nil {
		return errors.Join(cause, err)
	}
	return restartAfterFailure(ctx, r.Plan, r.Active, lifecycle, cause)
}
func (a *Artifacts) Install(ctx context.Context, bundle *Bundle, plan Plan, stageOnly bool, lifecycle Lifecycle) (*Result, error) {
	r, err := a.Receipt()
	if err != nil {
		return nil, err
	}
	if r.Pending != nil {
		return nil, errors.New("interrupted activation requires explicit rollback")
	}
	if r.Active.Release != "" && r.Plan != plan {
		return nil, errors.New("existing installation settings differ; configuration was not changed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	candidate := Selection{Release: releaseName(bundle.Manifest), Version: bundle.Manifest.Version, Checksums: bundle.TrustedChecksums}
	if !stageOnly && candidate != r.Active {
		if preflight, ok := lifecycle.(installPreflight); ok {
			needs, err := a.stageNeeds(bundle)
			if err != nil {
				return nil, err
			}
			if err = preflight.Preflight(ctx, plan, a, needs); err != nil {
				return nil, err
			}
		}
	}
	selected, err := a.Stage(bundle)
	if err != nil {
		return nil, err
	}
	result := &Result{State: "staged", Version: bundle.Manifest.Version, Prefix: a.Prefix, Reason: "NOT_STARTED_OR_READY"}
	if stageOnly {
		rendered, err := RenderService(plan, a.Prefix)
		if err != nil {
			return nil, err
		}
		reviewName := "staged-" + plan.Role + ".service"
		if plan.Role == "client" && bundle.Manifest.OS == "darwin" {
			reviewName = "staged-client.plist"
		}
		if err = a.root.WriteFile(reviewName, rendered, 0600); err != nil {
			return nil, err
		}
		result.ReviewFile = filepath.Join(a.Prefix, reviewName)
		return result, nil
	}
	if lifecycle == nil {
		return nil, errors.New("activation requires a service lifecycle")
	}
	current, err := a.Current()
	if err != nil {
		return nil, err
	}
	if current != r.Active.Release {
		return nil, errors.New("active link differs from installation receipt")
	}
	next := Selection{Release: selected, Version: bundle.Manifest.Version, Checksums: bundle.TrustedChecksums}
	if next == r.Active {
		result.State = "unchanged"
		result.Reason = "EXISTING_INSTALLATION_PRESERVED"
		return result, nil
	}
	release, err := acquireStartup(ctx, plan, lifecycle)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = lifecycle.Stop(ctx, plan); err != nil {
		return nil, err
	}
	backup, err := lifecycle.Backup(ctx, plan, a.Prefix)
	if err != nil {
		return nil, restartAfterFailure(ctx, plan, r.Active, lifecycle, err)
	}
	r.Plan = plan
	r.Backup = backup
	r.Pending = &next
	r.PendingAction = "install"
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, restartAfterFailure(ctx, plan, r.Active, lifecycle, err)
	}
	recoverFailure := func(cause error) (*Result, error) {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if err := lifecycle.Stop(recovery, plan); err != nil {
			return nil, errors.Join(cause, err)
		}
		if r.Active.Release == "" {
			if err := lifecycle.RemoveService(recovery, plan); err != nil {
				return nil, errors.Join(cause, err)
			}
		}
		if err := a.switchTo(r.Active.Release); err != nil {
			return nil, errors.Join(cause, err)
		}
		r.Pending = nil
		r.PendingAction = ""
		if err := a.writeJSON("installation.json", r); err != nil {
			return nil, errors.Join(cause, err)
		}
		return nil, restartAfterFailure(recovery, plan, r.Active, lifecycle, cause)
	}
	if err = a.switchTo(next.Release); err != nil {
		return recoverFailure(err)
	}
	if err = lifecycle.Prepare(ctx, plan, filepath.Join(a.Prefix, next.Release)); err != nil {
		return recoverFailure(err)
	}
	if err = lifecycle.Start(ctx, plan); err != nil {
		return recoverFailure(err)
	}
	state, err := lifecycle.Probe(ctx, plan)
	if err != nil {
		return recoverFailure(err)
	}
	r.Previous = r.Active
	r.Active = next
	r.Pending = nil
	r.PendingAction = ""
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, err
	}
	return &Result{State: state, Version: next.Version, Prefix: a.Prefix, Backup: backup, Reason: "REMOTE_APPLICATION_ACCEPTANCE_REQUIRED"}, nil
}
func (a *Artifacts) Rollback(ctx context.Context, lifecycle Lifecycle) (*Result, error) {
	r, err := a.Receipt()
	if err != nil {
		return nil, err
	}
	if r.Active.Release == "" && r.Pending == nil {
		return nil, errors.New("no active installation; reinstall the retained release to restore its service")
	}
	selected := r.Previous
	if r.Pending != nil {
		selected = r.Active
	} else if selected.Release == "" {
		return nil, errors.New("no previous release to roll back to")
	}
	if selected.Release != "" {
		if _, err = Verify(filepath.Join(a.Prefix, selected.Release), selected.Version, selected.Checksums); err != nil {
			return nil, err
		}
	}
	if preflight, ok := lifecycle.(installPreflight); ok {
		if err = preflight.Preflight(ctx, r.Plan, a, nil); err != nil {
			return nil, err
		}
	}
	release, err := acquireStartup(ctx, r.Plan, lifecycle)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = lifecycle.Stop(ctx, r.Plan); err != nil {
		return nil, err
	}
	if _, err = lifecycle.Backup(ctx, r.Plan, a.Prefix); err != nil {
		return nil, a.restartAfterRollbackFailure(ctx, r, lifecycle, err)
	}
	if selected.Release == "" {
		// The first activation never committed. Clear only its managed service and
		// links; preserve the newly initialized identity/configuration for retry.
		if err = lifecycle.RemoveService(ctx, r.Plan); err != nil {
			return nil, err
		}
		if err = a.switchTo(""); err != nil {
			return nil, err
		}
		r.Pending = nil
		r.PendingAction = ""
		if err = a.writeJSON("installation.json", r); err != nil {
			return nil, err
		}
		return &Result{State: "initial_activation_rolled_back", Prefix: a.Prefix, Reason: "PRIVATE_CONFIG_AND_STATE_PRESERVED"}, nil
	}
	old := r.Active
	r.Pending = &selected
	r.PendingAction = "rollback"
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, a.restartAfterRollbackFailure(ctx, r, lifecycle, err)
	}
	if err = a.switchTo(selected.Release); err != nil {
		return nil, err
	}
	if err = lifecycle.Prepare(ctx, r.Plan, filepath.Join(a.Prefix, selected.Release)); err != nil {
		return nil, err
	}
	if err = lifecycle.Start(ctx, r.Plan); err != nil {
		return nil, err
	}
	state, err := lifecycle.Probe(ctx, r.Plan)
	if err != nil {
		return nil, err
	}
	r.Active = selected
	if selected != old {
		r.Previous = old
	}
	r.Pending = nil
	r.PendingAction = ""
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, err
	}
	return &Result{State: state, Version: selected.Version, Prefix: a.Prefix, Reason: "ARTIFACTS_ROLLED_BACK_STATE_PRESERVED"}, nil
}
func (a *Artifacts) Uninstall(ctx context.Context, lifecycle Lifecycle) (*Result, error) {
	r, err := a.Receipt()
	if err != nil {
		return nil, err
	}
	if r.Pending != nil {
		return nil, errors.New("interrupted activation must be recovered before uninstall")
	}
	if r.Active.Release == "" {
		return &Result{State: "not_installed", Prefix: a.Prefix}, nil
	}
	release, err := acquireStartup(ctx, r.Plan, lifecycle)
	if err != nil {
		return nil, err
	}
	defer release()
	selected := r.Active
	r.Pending = &selected
	r.PendingAction = "uninstall"
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, err
	}
	if err = lifecycle.Stop(ctx, r.Plan); err != nil {
		return nil, err
	}
	if err = lifecycle.RemoveService(ctx, r.Plan); err != nil {
		return nil, err
	}
	if err = a.switchTo(""); err != nil {
		return nil, err
	}
	r.Previous = r.Active
	r.Active = Selection{}
	r.Pending = nil
	r.PendingAction = ""
	if err = a.writeJSON("installation.json", r); err != nil {
		return nil, err
	}
	return &Result{State: "uninstalled", Prefix: a.Prefix, Reason: "CONFIG_STATE_PROJECTS_TMUX_AND_RELEASE_BACKUPS_PRESERVED"}, nil
}
