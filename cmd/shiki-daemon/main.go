package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := NewCommand(os.Stdout, os.Stderr).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "shiki-daemon:", err)
		os.Exit(1)
	}
}

func NewCommand(out, errOut io.Writer) *cobra.Command {
	var configPath string
	root := &cobra.Command{Use: "shiki-daemon", Short: "Run the single-user Shikigami server", SilenceUsage: true, SilenceErrors: true, Version: buildinfo.String("shiki-daemon")}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().StringVar(&configPath, "config", "/etc/shiki/daemon.yaml", "Absolute daemon configuration path (shared by every command)")
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show build metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String("shiki-daemon"))
		return err
	}})
	root.AddCommand(&cobra.Command{Use: "serve", Short: "Serve authenticated Connect RPCs and local NodeControl", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.LoadDaemon(configPath)
		if err != nil {
			return err
		}
		runtime, err := server.StartDaemon(cmd.Context(), cfg, server.DaemonOptions{Build: buildinfo.Version})
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err = json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Event  string            `json:"event"`
			Daemon server.DaemonInfo `json:"daemon"`
		}{"daemon_started", runtime.Info()}); err != nil {
			return err
		}
		return runtime.Wait()
	}})
	root.AddCommand(initCommand(&configPath), tokenCommand(&configPath))
	return root
}
