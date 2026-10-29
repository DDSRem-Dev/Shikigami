package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func (a *app) pluginTarget(ctx context.Context, c *client.Client, side string) (*apb.PluginTarget, error) {
	target := &apb.PluginTarget{}
	switch side {
	case "local":
		if a.server != "" {
			return nil, errors.New("--server is only valid with --side remote")
		}
		target.Side = apb.PluginTarget_LOCAL
	case "remote":
		if a.server == "" {
			return nil, errors.New("--side remote requires explicit --server")
		}
		server, e := c.ResolveServer(ctx, a.server)
		if e != nil {
			return nil, e
		}
		target.Side = apb.PluginTarget_REMOTE
		target.ServerId = server.Config.Id
	default:
		return nil, errors.New("--side must be explicitly local or remote")
	}
	return target, nil
}
func (a *app) pluginCommands() *cobra.Command {
	var side string
	root := &cobra.Command{Use: "plugins", Short: "Manage local or remote plugins through the agent", Args: cobra.NoArgs}
	root.PersistentFlags().StringVar(&side, "side", "", "Required: local or remote; remote also requires --server")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		if side != "local" && side != "remote" {
			return errors.New("--side must be explicitly local or remote")
		}
		if side == "remote" && a.server == "" {
			return errors.New("--side remote requires explicit --server")
		}
		if side == "local" && a.server != "" {
			return errors.New("--server is only valid with --side remote")
		}
		return nil
	}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			target, e := a.pluginTarget(cmd.Context(), c, side)
			if e != nil {
				return e
			}
			result, e := c.Agent().ListPlugins(cmd.Context(), target)
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	root.RunE = list.RunE
	root.AddCommand(list)
	root.AddCommand(&cobra.Command{Use: "status <plugin-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			target, e := a.pluginTarget(cmd.Context(), c, side)
			if e != nil {
				return e
			}
			status, e := pluginStatus(cmd.Context(), c, target, args[0])
			if e != nil {
				return e
			}
			return a.print(status)
		})
	}})
	for _, operation := range []struct {
		name   string
		action pb.ManagePluginRequest_Action
	}{{"enable", pb.ManagePluginRequest_ENABLE}, {"disable", pb.ManagePluginRequest_DISABLE}, {"restart", pb.ManagePluginRequest_RESTART}, {"config", pb.ManagePluginRequest_SET_CONFIG}} {
		var revision, configFile string
		var wait time.Duration
		command := &cobra.Command{Use: operation.name + " <plugin-id>", Args: cobra.ExactArgs(1), Short: "Persist the requested change and wait for the worker result", RunE: func(cmd *cobra.Command, args []string) error {
			if wait <= 0 {
				return errors.New("--wait-timeout must be positive")
			}
			var values *structpb.Struct
			if operation.action == pb.ManagePluginRequest_SET_CONFIG {
				if configFile == "" {
					return errors.New("config requires --file containing a JSON object")
				}
				data, e := readBoundedFile(configFile, 1<<20)
				if e != nil {
					return e
				}
				values = new(structpb.Struct)
				if e = protojson.Unmarshal(data, values); e != nil {
					return fmt.Errorf("invalid plugin configuration JSON: %w", e)
				}
			}
			return a.withClient(cmd.Context(), true, func(c *client.Client) error {
				target, e := a.pluginTarget(cmd.Context(), c, side)
				if e != nil {
					return e
				}
				expected := revision
				if expected == "" {
					status, e := pluginStatus(cmd.Context(), c, target, args[0])
					if e != nil {
						return e
					}
					expected = status.Revision
				}
				accepted, e := c.Agent().ManagePlugin(cmd.Context(), &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: args[0], Action: operation.action, Config: values, ExpectedRevision: expected, RequestId: a.id()}})
				if e != nil {
					return e
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), wait)
				defer cancel()
				final, e := waitForPlugin(ctx, accepted, func(ctx context.Context) (*pb.PluginStatus, error) { return pluginStatus(ctx, c, target, args[0]) })
				if final != nil {
					if printErr := a.print(final); printErr != nil {
						return printErr
					}
				}
				return e
			})
		}}
		command.Flags().StringVar(&revision, "expected-revision", "", "Require this observed revision; defaults to a fresh status read")
		command.Flags().DurationVar(&wait, "wait-timeout", 30*time.Second, "Maximum time to wait for ready, stopped, or failed")
		if operation.action == pb.ManagePluginRequest_SET_CONFIG {
			command.Flags().StringVar(&configFile, "file", "", "JSON object with the complete plugin configuration")
		}
		root.AddCommand(command)
	}
	root.AddCommand(a.pluginLogsCommand(&side))
	return root
}
func (a *app) pluginLogsCommand(side *string) *cobra.Command {
	var tail uint32
	var follow bool
	logs := &cobra.Command{Use: "logs <plugin-id>", Args: cobra.ExactArgs(1), Short: "Read host-redacted plugin logs", RunE: func(cmd *cobra.Command, args []string) error {
		if tail > 1000 {
			return errors.New("--tail cannot exceed 1000")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			target, e := a.pluginTarget(cmd.Context(), c, *side)
			if e != nil {
				return e
			}
			stream, e := c.Agent().WatchPluginLogs(cmd.Context(), &apb.PluginLogsRequest{Target: target, Request: &pb.PluginLogsRequest{PluginId: args[0], Tail: tail, Follow: follow}})
			if e != nil {
				return e
			}
			defer stream.Close()
			for {
				record, e := stream.Receive()
				if errors.Is(e, io.EOF) {
					return nil
				}
				if e != nil {
					return e
				}
				if a.json {
					data, e := protojson.MarshalOptions{UseProtoNames: true}.Marshal(record)
					if e != nil {
						return e
					}
					if _, e = fmt.Fprintln(a.output, string(data)); e != nil {
						return e
					}
				} else {
					if _, e = fmt.Fprintf(a.output, "%d %s %s\n", record.Timestamp, safeText(record.Level), safeText(record.Message)); e != nil {
						return e
					}
				}
			}
		})
	}}
	logs.Flags().Uint32Var(&tail, "tail", 100, "Recent log records (maximum 1000)")
	logs.Flags().BoolVar(&follow, "follow", false, "Continue reading until canceled")
	return logs
}
func pluginStatus(ctx context.Context, c *client.Client, target *apb.PluginTarget, id string) (*pb.PluginStatus, error) {
	list, e := c.Agent().ListPlugins(ctx, target)
	if e != nil {
		return nil, e
	}
	for _, status := range list.Plugins {
		if status.PluginId == id {
			return status, nil
		}
	}
	return nil, fmt.Errorf("plugin %q was not found", id)
}
func waitForPlugin(ctx context.Context, status *pb.PluginStatus, refresh func(context.Context) (*pb.PluginStatus, error)) (*pb.PluginStatus, error) {
	revision := status.Revision
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if status.Revision != revision {
			return status, errors.New("plugin changed while waiting; inspect its current status")
		}
		switch status.State {
		case "failed", "incompatible":
			return status, fmt.Errorf("plugin %s: %s", status.State, safeText(status.ReasonCode))
		case "ready":
			if status.Enabled {
				return status, nil
			}
		case "stopped":
			if !status.Enabled {
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return status, fmt.Errorf("plugin change was accepted but completion was not confirmed: %w", ctx.Err())
		case <-ticker.C:
		}
		next, e := refresh(ctx)
		if e != nil {
			return status, e
		}
		status = next
	}
}
func readBoundedFile(path string, limit int64) ([]byte, error) {
	absolute, e := client.ConfigPath(path)
	if e != nil {
		return nil, e
	}
	file, e := os.Open(absolute)
	if e != nil {
		return nil, e
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds the allowed size")
	}
	return data, nil
}

func (a *app) logsCommand() *cobra.Command {
	var side string
	cmd := a.pluginLogsCommand(&side)
	cmd.Flags().StringVar(&side, "side", "", "Required: local or remote; remote also requires --server")
	cmd.PreRunE = func(*cobra.Command, []string) error {
		if side != "local" && side != "remote" {
			return errors.New("--side must be explicitly local or remote")
		}
		if side == "remote" && a.server == "" {
			return errors.New("--side remote requires explicit --server")
		}
		if side == "local" && a.server != "" {
			return errors.New("--server is only valid with --side remote")
		}
		return nil
	}
	return cmd
}
