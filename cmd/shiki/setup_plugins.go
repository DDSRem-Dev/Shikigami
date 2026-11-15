package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func (w *setupWizard) plugins(ctx context.Context, server, components string) error {
	ids := []string{"sync", "port"}
	if components == "all" {
		ids = append(ids, "region", "browser")
	}
	for _, id := range ids {
		for _, side := range []string{"remote", "local"} {
			if id == "region" && side == "local" {
				continue
			}
			args := []string{}
			if side == "remote" {
				args = append(args, "--server", server)
			}
			args = append(args, "plugins", "--side", side)
			status := new(pb.PluginStatus)
			if err := w.backend.query(ctx, status, append(args, "status", id)...); err != nil {
				return fmt.Errorf("%s %s plugin unavailable; finish the matching installer on that machine: %w", side, id, err)
			}
			if !status.Enabled || status.State != "ready" {
				if err := w.backend.run(ctx, false, append(args, "enable", id, "--expected-revision", status.Revision)...); err != nil {
					return fmt.Errorf("%s %s plugin remains pending; for VPS region/browser prerequisites rerun the VPS quickstart installer: %w", side, id, err)
				}
				if err := w.backend.query(ctx, status, append(args, "status", id)...); err != nil {
					return err
				}
			}
			if !status.Enabled || status.State != "ready" {
				return fmt.Errorf("%s %s plugin is %s (%s), not ready", side, id, safeText(status.State), safeText(status.ReasonCode))
			}
			fmt.Fprintf(w.out, "%s %s: ready\n", side, id)
		}
	}
	if components == "all" {
		status := new(epb.RegionStatus)
		if err := w.backend.query(ctx, status, "--server", server, "region", "inspect"); err != nil {
			return fmt.Errorf("region inspection remains pending; rerun the VPS quickstart installer: %w", err)
		}
		if !status.HelperAvailable {
			return errors.New("region system support is unavailable; rerun the VPS quickstart installer to install its authorized helper")
		}
		fmt.Fprintln(w.out, "Region system support verified; existing whole-host timezone, locale and DNS remain selected.")
	}
	return nil
}

func (w *setupWizard) ports(ctx context.Context, server string, existing bool) error {
	if existing {
		fmt.Fprintln(w.out, "Existing port configuration is preserved unless you explicitly replace it below; individual forward availability has not been checked.")
	}
	value, err := w.prompt(ctx, "Allowed VPS TCP ports, comma-separated (e.g. 3000,8080); blank preserves existing configuration", "")
	if err != nil {
		return err
	}
	if value == "" {
		fmt.Fprintln(w.out, "Port plugin enabled; no new port allowance or forward configured.")
		return nil
	}
	ports, err := setupPorts(value)
	if err != nil {
		return err
	}
	answer, err := w.prompt(ctx, "Replace the COMPLETE remote port configuration with this allowlist and defaults for all other options? yes/no", "no")
	if err != nil {
		return err
	}
	if answer != "yes" {
		fmt.Fprintln(w.out, "Port configuration preserved; no new forward configured.")
		return nil
	}
	status := new(pb.PluginStatus)
	if err := w.backend.query(ctx, status, "--server", server, "plugins", "status", "port", "--side", "remote"); err != nil {
		return err
	}
	return setupJSONFile(map[string]any{"allow_ports": ports}, func(path string) error {
		if err := w.backend.run(ctx, false, "--server", server, "plugins", "config", "port", "--side", "remote", "--expected-revision", status.Revision, "--file", path); err != nil {
			return err
		}
		fmt.Fprintf(w.out, "Allowed ports saved. Start a VPS service, then use: shiki --server %s ports forward %d\n", safeText(server), ports[0])
		return nil
	})
}

func setupPorts(value string) ([]int, error) {
	var ports []int
	seen := map[int]bool{}
	parts := strings.Split(value, ",")
	if len(parts) > 64 {
		return nil, errors.New("choose at most 64 ports")
	}
	for _, text := range parts {
		port, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("ports must be comma-separated integers from 1 to 65535")
		}
		if !seen[port] {
			ports = append(ports, port)
			seen[port] = true
		}
	}
	return ports, nil
}

func setupJSONFile(value any, use func(string) error) error {
	directory, err := os.MkdirTemp("", "shiki-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "settings.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return use(path)
}
