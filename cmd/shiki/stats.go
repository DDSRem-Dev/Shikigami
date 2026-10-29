package main

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func (a *app) statsCommand() *cobra.Command {
	var watch bool
	var interval uint32
	cmd := &cobra.Command{Use: "stats [session]", Args: cobra.MaximumNArgs(1), Short: "Sample actual server and session resources", RunE: func(cmd *cobra.Command, args []string) error {
		if interval < 1 || interval > 60 {
			return errors.New("--interval must be 1–60 seconds")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, err := a.selected(cmd.Context(), c)
			if err != nil {
				return err
			}
			request := &pb.StatsRequest{IntervalSeconds: interval}
			if len(args) > 0 {
				session, err := c.ResolveSession(cmd.Context(), server, project, args[0])
				if err != nil {
					return err
				}
				request.SessionId = session.Id
			}
			remote := c.Remote(server)
			if !watch {
				snapshot, err := remote.GetStats(cmd.Context(), request)
				if err != nil {
					return err
				}
				return a.printStats(snapshot, false)
			}
			stream, err := remote.WatchStats(cmd.Context(), request)
			if err != nil {
				return err
			}
			defer stream.Close()
			for {
				snapshot, err := stream.Receive()
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					return err
				}
				if err = a.printStats(snapshot, true); err != nil {
					return err
				}
			}
		})
	}}
	cmd.Flags().BoolVar(&watch, "watch", false, "Continue sampling until canceled")
	cmd.Flags().Uint32Var(&interval, "interval", 5, "Sample interval in seconds (1–60)")
	return cmd
}
func (a *app) printStats(value *pb.StatsSnapshot, stream bool) error {
	if a.json {
		if !stream {
			return a.print(value)
		}
		data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(a.output, string(data))
		return err
	}
	fmt.Fprintf(a.output, "Resources at %d · CPU: one logical core = 100%%\n", value.Timestamp)
	fmt.Fprintf(a.output, "System CPU: %s · memory used: %s · minimum disk free (state/projects): %s\n", cpuText(value.CpuPercent), bytesText(value.MemoryUsedBytes), bytesText(value.DiskFreeBytes))
	table := tabwriter.NewWriter(a.output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "SESSION\tCPU\tPROCESS TREE RSS")
	for _, session := range value.Sessions {
		fmt.Fprintf(table, "%s\t%s\t%s\n", session.SessionId, cpuText(session.CpuPercent), bytesText(session.MemoryBytes))
	}
	return table.Flush()
}
func cpuText(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
func bytesText(value *int64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f MiB", float64(*value)/(1024*1024))
}
