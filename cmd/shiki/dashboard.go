package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/client/ui"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/spf13/cobra"
)

func (a *app) dashboardCommand() *cobra.Command {
	return &cobra.Command{Use: "dashboard", Args: cobra.NoArgs, Short: "Inspect sessions, sync, forwards, plugins and node status", RunE: func(cmd *cobra.Command, _ []string) error {
		if a.timeout <= 0 {
			return errors.New("--timeout must be positive")
		}
		if !a.interactive() || a.json {
			return errors.New("dashboard requires terminal stdin/stdout and cannot use --json")
		}
		server, project := a.server, a.project
		retries := 0
		for {
			c, err := client.Open(cmd.Context(), a.configPath, client.Options{AutoStart: true, Timeout: a.timeout})
			if err != nil {
				return err
			}
			list, err := c.Agent().ListServers(cmd.Context(), &pb.Empty{})
			if err != nil {
				c.Close()
				return err
			}
			serverID, projectID := "", ""
			if len(list.Servers) > 0 {
				selected, err := c.ResolveServer(cmd.Context(), server)
				if err != nil && server != "" {
					c.Close()
					return err
				}
				if err == nil {
					serverID = selected.GetConfig().GetId()
				} else {
					serverID = list.Servers[0].GetConfig().GetId()
				}
				if project != "" {
					selected, err := c.ResolveProject(cmd.Context(), serverID, project)
					if err != nil {
						c.Close()
						return err
					}
					projectID = selected.Id
				}
			}
			prefs, err := c.Agent().GetPreferences(cmd.Context(), &pb.Empty{})
			if err != nil {
				c.Close()
				return err
			}
			action, err := ui.Run(cmd.Context(), c, ui.Options{Input: a.input, Output: a.output.(*os.File), ServerID: serverID, ProjectID: projectID, NoColor: a.noColor || prefs.NoColor, Verbose: a.verbose})
			if err != nil {
				c.Close()
				return err
			}
			if action.Reconnect {
				c.Close()
				retries++
				if retries > 5 {
					return errors.New("dashboard agent connection failed after five retries")
				}
				fmt.Fprintf(a.errorOutput, "Agent connection changed; reconnecting (%d/5).\n", retries)
				timer := time.NewTimer(time.Second * time.Duration(1<<(retries-1)))
				select {
				case <-cmd.Context().Done():
					timer.Stop()
					return cmd.Context().Err()
				case <-timer.C:
				}
				continue
			}
			retries = 0
			if action.Session == nil {
				c.Close()
				return nil
			}
			preferences, err := c.Agent().GetPreferences(cmd.Context(), &pb.Empty{})
			if err != nil {
				c.Close()
				return err
			}
			key, err := client.ParseDetachKey(preferences.DetachKey)
			if err != nil {
				c.Close()
				return err
			}
			err = a.runTerminal(cmd.Context(), c, action.ServerID, action.Session, client.TerminalOptions{Input: a.input, Output: a.output.(*os.File), ReadOnly: action.ReadOnly, TakeControl: action.TakeControl, DetachKey: key}, true)
			c.Close()
			if err != nil {
				return err
			}
			server = action.ServerID
			project = ""
		}
	}}
}
