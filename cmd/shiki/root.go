package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type app struct {
	configPath, server, project, requestID string
	json, noColor                          bool
	verbose                                bool
	timeout                                time.Duration
	input                                  *os.File
	output, errorOutput                    io.Writer
}

func newRoot(input *os.File, output, errorOutput io.Writer) *cobra.Command {
	a := &app{input: input, output: output, errorOutput: errorOutput}
	root := &cobra.Command{Use: "shiki", Short: "Use remote Claude Code through the local Shikigami agent", SilenceUsage: true, SilenceErrors: true, Version: buildinfo.Version, RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unknown command %q", args[0])
		}
		return a.smart(cmd.Context())
	}}
	root.SetVersionTemplate(buildinfo.String("shiki") + "\n")
	root.SetIn(input)
	root.SetOut(output)
	root.SetErr(errorOutput)
	root.PersistentFlags().StringVar(&a.configPath, "config", client.DefaultConfig, "Client configuration file")
	root.PersistentFlags().StringVar(&a.server, "server", "", "Server ID or unambiguous name")
	root.PersistentFlags().StringVar(&a.project, "project", "", "Project ID or unambiguous name")
	root.PersistentFlags().StringVar(&a.requestID, "request-id", "", "Stable request ID for an intentional retry of a mutation")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Print structured JSON to stdout")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "Disable color (plain output is the default)")
	root.PersistentFlags().BoolVar(&a.verbose, "verbose", false, "Show bounded plugin log tails after detach and in the dashboard Plugins panel")
	root.PersistentFlags().DurationVar(&a.timeout, "timeout", 5*time.Second, "Timeout for each short IPC call")
	root.AddCommand(a.agentCommands(), a.serverCommands(), a.connectCommand(), a.projectCommands(), a.sessionCommands(), a.attachCommand(), a.doctorCommand(), a.pluginCommands(), a.nodeCommands(), a.syncCommands(), a.portCommands(), a.statsCommand(), a.logsCommand(), a.dashboardCommand())
	initialize, templates := a.templateCommands()
	root.AddCommand(initialize, templates)
	root.AddCommand(a.configCommands())
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, Short: "Show build information", RunE: func(*cobra.Command, []string) error {
		if a.json {
			return a.printJSON(buildinfo.Current())
		}
		_, e := fmt.Fprintln(a.output, buildinfo.String("shiki"))
		return e
	}})
	return root
}
func (a *app) withClient(ctx context.Context, auto bool, run func(*client.Client) error) error {
	if a.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	c, e := client.Open(ctx, a.configPath, client.Options{AutoStart: auto, Timeout: a.timeout})
	if e != nil {
		return e
	}
	defer c.Close()
	return run(c)
}
func (a *app) id() string {
	if a.requestID != "" {
		return a.requestID
	}
	return rand.Text()
}
func (a *app) interactive() bool {
	output, ok := a.output.(*os.File)
	return ok && a.input != nil && term.IsTerminal(int(a.input.Fd())) && term.IsTerminal(int(output.Fd()))
}
func (a *app) prompt(ctx context.Context, label, defaultValue string) (string, error) {
	if !a.interactive() {
		return "", fmt.Errorf("%s is required in non-interactive mode", label)
	}
	if defaultValue != "" {
		fmt.Fprintf(a.errorOutput, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(a.errorOutput, "%s: ", label)
	}
	value, e := client.ReadConsoleLine(ctx, a.input, 4096)
	if e != nil {
		return "", e
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}
func (a *app) confirm(ctx context.Context, label string, yes bool) error {
	if yes {
		return nil
	}
	if !a.interactive() {
		return errors.New("destructive operations require --yes in non-interactive mode")
	}
	answer, e := a.prompt(ctx, label+" (type yes)", "")
	if e != nil {
		return e
	}
	if answer != "yes" {
		return errors.New("operation canceled")
	}
	return nil
}
func (a *app) printJSON(value any) error {
	encoder := json.NewEncoder(a.output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
func (a *app) print(message proto.Message) error {
	if list, ok := message.(*apb.ServerList); ok {
		message = client.SafeConnectionAttemptList(list)
	}
	if a.json {
		data, e := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true, Indent: "  "}.Marshal(message)
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(a.output, string(data))
		return e
	}
	table := tabwriter.NewWriter(a.output, 0, 4, 2, ' ', 0)
	switch value := message.(type) {
	case *apb.ServerList:
		fmt.Fprintln(table, "ID\tNAME\tSTATE\tADDRESS\tROUTE DIAGNOSTIC")
		for _, s := range value.Servers {
			address := s.Config.DirectTlsAddress
			if address == "" {
				address = s.Config.TailscaleAddress
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", s.Config.Id, safeText(s.Config.Name), s.ConnectionState, address, client.ConnectionAttemptsNotice(s))
		}
	case *pb.ListProjectsResponse:
		fmt.Fprintln(table, "ID\tNAME\tREMOTE PATH")
		for _, p := range value.Projects {
			fmt.Fprintf(table, "%s\t%s\t%s\n", p.Id, safeText(p.Name), safeText(p.Path))
		}
	case *pb.ListSessionsResponse:
		fmt.Fprintln(table, "ID\tPROJECT\tNAME\tCLAUDE\tATTACHMENTS")
		for _, s := range value.Sessions {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%d\n", s.Id, s.ProjectId, safeText(s.Name), s.ClaudeStatus, s.ClientCount)
		}
	case *ppb.MappingList:
		fmt.Fprintln(table, "ID\tSERVER\tPROJECT\tSYNC ENABLED\tREVISION\tLOCAL PATH")
		for _, m := range value.Mappings {
			fmt.Fprintf(table, "%s\t%s\t%s\t%t\t%s\t%s\n", m.Id, m.ServerId, m.ProjectId, m.SyncEnabled, m.Revision, safeText(m.LocalRoot))
		}
	case *pb.ListSnapshotsResponse:
		fmt.Fprintln(table, "ID\tPROJECT\tSOURCE SESSION\tDIRECTORY\tCREATED\tDESCRIPTION")
		for _, snapshot := range value.Snapshots {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", snapshot.Id, snapshot.ProjectId, safeText(snapshot.SessionName), safeText(snapshot.WorkingDir), time.Unix(snapshot.CreatedAt, 0).UTC().Format(time.RFC3339), safeText(snapshot.Description))
		}
	case *pb.ListPluginsResponse:
		fmt.Fprintln(table, "ID\tVERSION\tENABLED\tSTATE\tREVISION\tREASON")
		for _, p := range value.Plugins {
			fmt.Fprintf(table, "%s\t%s\t%t\t%s\t%s\t%s\n", safeText(p.PluginId), safeText(p.BuildVersion), p.Enabled, safeText(p.State), p.Revision, safeText(p.ReasonCode))
		}
	case *ppb.ConflictList:
		fmt.Fprintln(table, "ID\tPATH\tLOCAL\tREMOTE\tGENERATION\tREASON")
		for _, c := range value.Conflicts {
			reason := "BOTH_SIDES_CHANGED"
			if c.Base == nil {
				reason = "INITIAL_SYNC_CHOICE_REQUIRED"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Id, safeText(c.Path), conflictVersionLabel(c.Local), conflictVersionLabel(c.Remote), c.Generation, reason)
		}
	default:
		data, e := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}.Marshal(message)
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(a.output, string(data))
		return e
	}
	return table.Flush()
}
func safeText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return '�'
		}
		return r
	}, value)
}
func (a *app) selected(ctx context.Context, c *client.Client) (string, string, error) {
	server, e := c.ResolveServer(ctx, a.server)
	if e != nil {
		return "", "", e
	}
	projectID := ""
	if a.project != "" {
		project, e := c.ResolveProject(ctx, server.Config.Id, a.project)
		if e != nil {
			return "", "", e
		}
		projectID = project.Id
	}
	return server.Config.Id, projectID, nil
}
