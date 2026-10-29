package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"

	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/spf13/cobra"
)

func initCommand(configPath *string) *cobra.Command {
	var runUser, stateDir, projectsDir, claude, hostname, address, cert, key string
	var args []string
	var tailscale bool
	var port int
	current, _ := user.Current()
	defaultUser := ""
	if current != nil && current.Uid != "0" {
		defaultUser = current.Username
	}
	cmd := &cobra.Command{Use: "init", Short: "Initialize private configuration and identity without replacing existing data", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if runUser == "" {
			return errors.New("--user must name the ordinary account running this command")
		}
		cfg := config.DefaultDaemon(runUser, stateDir)
		loaded, loadErr := config.LoadDaemon(*configPath)
		existing := loadErr == nil
		if existing {
			cfg = loaded
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return loadErr
		}
		original := *cfg
		set := func(flag string) bool { return !existing || cmd.Flags().Changed(flag) }
		if set("user") {
			cfg.RunUser = runUser
		}
		if set("state-dir") {
			cfg.StateDir = stateDir
		}
		if set("projects-dir") && projectsDir != "" {
			cfg.ProjectsDir = projectsDir
		}
		if set("claude-executable") {
			cfg.Claude.Executable = claude
		}
		if set("claude-arg") {
			cfg.Claude.Args = append([]string{}, args...)
		}
		if set("tailscale") {
			cfg.Tailscale.Enabled = tailscale
		}
		if set("hostname") {
			cfg.Tailscale.Hostname = hostname
		}
		if set("port") {
			cfg.Server.Port = port
		}
		if set("direct-tls-address") {
			cfg.Server.DirectTLS.Address = address
		}
		if set("tls-cert") {
			cfg.Server.DirectTLS.CertFile = cert
		}
		if set("tls-key") {
			cfg.Server.DirectTLS.KeyFile = key
		}
		if !existing || cmd.Flags().Changed("direct-tls-address") || cmd.Flags().Changed("tls-cert") || cmd.Flags().Changed("tls-key") {
			cfg.Server.DirectTLS.Enabled = cfg.Server.DirectTLS.Address != "" || cfg.Server.DirectTLS.CertFile != "" || cfg.Server.DirectTLS.KeyFile != ""
		}
		if err := cfg.NormalizeAndValidate(); err != nil {
			return err
		}
		if err := cfg.CheckRunUser(); err != nil {
			return err
		}
		if _, err := tmux.New(""); err != nil {
			return err
		}
		if existing && !reflect.DeepEqual(original, *cfg) {
			return errors.New("existing configuration differs from init flags; it was not overwritten")
		}
		if !existing {
			if err := config.SaveDaemon(*configPath, cfg); err != nil {
				return err
			}
		}
		id, err := server.Initialize(cfg)
		if err != nil {
			return err
		}
		absolute, err := filepath.Abs(*configPath)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Event, Config, DaemonID string
			Existing                bool
		}{"initialized", absolute, id, existing})
	}}
	cmd.Flags().StringVar(&runUser, "user", defaultUser, "Existing ordinary OS account (must match current user)")
	cmd.Flags().StringVar(&stateDir, "state-dir", "/var/lib/shiki", "Private daemon state directory")
	cmd.Flags().StringVar(&projectsDir, "projects-dir", "", "Allowed base for creating new project directories")
	cmd.Flags().StringVar(&claude, "claude-executable", "claude", "Claude executable or command name")
	cmd.Flags().StringArrayVar(&args, "claude-arg", nil, "Claude launch argument; repeat for each argv element")
	cmd.Flags().BoolVar(&tailscale, "tailscale", true, "Enable the embedded Tailscale node")
	cmd.Flags().StringVar(&hostname, "hostname", "shiki-vps", "Embedded Tailscale node hostname")
	cmd.Flags().IntVar(&port, "port", 9527, "Tailnet Connect listener port")
	cmd.Flags().StringVar(&address, "direct-tls-address", "", "Enable a separate direct TLS listener at host:port")
	cmd.Flags().StringVar(&cert, "tls-cert", "", "Direct TLS certificate PEM path")
	cmd.Flags().StringVar(&key, "tls-key", "", "Direct TLS private-key PEM path")
	return cmd
}
