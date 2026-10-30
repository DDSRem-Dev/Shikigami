package main

import (
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

// A private config filename alone is insufficient: all mutable destinations
// must also remain in this acceptance root, and no external login may start.
func (h *helper) validateConfiguration() error {
	c, err := config.LoadClient(h.config)
	if err != nil {
		return err
	}
	if c.Tailscale.Enabled || c.Agent.AutoStart {
		return invalid()
	}
	paths := []string{c.Agent.Socket, c.Agent.StateDir, c.PluginsDir}
	if c.Tailscale.StateDir != "" {
		paths = append(paths, c.Tailscale.StateDir)
	}
	for _, server := range c.Servers {
		if server.TLSCAFile != "" {
			paths = append(paths, server.TLSCAFile)
		}
	}
	if h.daemonConfig != "" {
		d, err := config.LoadDaemon(h.daemonConfig)
		if err != nil {
			return err
		}
		if err = d.CheckRunUser(); err != nil {
			return err
		}
		if d.Tailscale.Enabled || !d.Server.DirectTLS.Enabled || !loopback(d.Server.DirectTLS.Address) {
			return invalid()
		}
		paths = append(paths, d.StateDir, d.ProjectsDir, d.PluginsDir, d.Server.DirectTLS.CertFile, d.Server.DirectTLS.KeyFile, d.Claude.Executable)
		if d.Tailscale.StateDir != "" {
			paths = append(paths, d.Tailscale.StateDir)
		}
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return invalid()
		}
		if _, err = h.path(path); err != nil {
			return err
		}
	}
	return nil
}
