package server

import (
	"errors"
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

// Initialize creates missing private state once and validates existing state.
// It never rotates signing keys or contacts a Tailscale control plane.
func Initialize(cfg *config.Daemon) (daemonID string, err error) {
	if err = cfg.NormalizeAndValidate(); err != nil {
		return "", err
	}
	if err = cfg.CheckRunUser(); err != nil {
		return "", err
	}
	state, err := storage.Open(cfg.StateDir, storage.Options{Validate: storage.ValidateDaemon(cfg.RunUser)})
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, state.Close()) }()
	if err = storage.EnsurePrivateDir(filepath.Join(cfg.StateDir, "run")); err != nil {
		return "", err
	}
	projects, err := project.New(state, cfg.ProjectsDir, cfg.RunUser)
	if err != nil {
		return "", err
	}
	if err = projects.Close(); err != nil {
		return "", err
	}
	credentials, err := auth.Open(filepath.Join(cfg.StateDir, "auth"), cfg.RunUser)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, credentials.Close()) }()
	return credentials.DaemonID()
}
