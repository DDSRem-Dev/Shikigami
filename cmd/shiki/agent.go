package main

import (
	"context"
	"errors"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
)

func (a *app) agentCommands() *cobra.Command {
	root := &cobra.Command{Use: "agent", Short: "Manage the shared local background agent"}
	var recoverLoop bool
	start := &cobra.Command{Use: "start", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if a.timeout <= 0 {
			return errors.New("--timeout must be positive")
		}
		c, e := client.Open(cmd.Context(), a.configPath, client.Options{AutoStart: true, ExplicitStart: true, RecoverCrashLoop: recoverLoop, Timeout: a.timeout})
		if e != nil {
			return e
		}
		defer c.Close()
		status, e := c.Agent().GetStatus(cmd.Context(), &pb.Empty{})
		if e != nil {
			return e
		}
		return a.print(status)
	}}
	start.Flags().BoolVar(&recoverLoop, "recover-crash-loop", false, "After inspecting logs and fixing the cause, explicitly reset the agent crash-loop guard")
	root.AddCommand(start)
	root.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), false, func(c *client.Client) error {
			status, e := c.Agent().GetStatus(cmd.Context(), &pb.Empty{})
			if e != nil {
				return e
			}
			return a.print(status)
		})
	}})
	root.AddCommand(&cobra.Command{Use: "stop", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), false, func(c *client.Client) error {
			status, e := c.Agent().Shutdown(cmd.Context(), &apb.ShutdownRequest{ExpectedInstanceId: c.InstanceID(), RequestId: a.id()})
			if e != nil {
				return e
			}
			wait, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			if e := c.WaitStopped(wait); e != nil {
				return e
			}

			return a.print(status)
		})
	}})
	return root
}
