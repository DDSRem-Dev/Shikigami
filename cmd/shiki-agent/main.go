package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func command() *cobra.Command {
	path := agent.DefaultConfigPath()
	var recoverLoop bool
	cmd := &cobra.Command{Use: "shiki-agent", Short: "Run the per-user Shikigami background agent", SilenceUsage: true, SilenceErrors: true, Version: buildinfo.String("shiki-agent"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return agent.RunWithRecovery(cmd.Context(), path, buildinfo.Version, recoverLoop)
	}}
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.Flags().StringVar(&path, "config", path, "Client configuration file (private YAML)")
	cmd.Flags().BoolVar(&recoverLoop, "recover-crash-loop", false, "Explicitly reset a valid crash-loop record before startup (agent must be stopped)")
	return cmd
}
