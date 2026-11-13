package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"
)

// pluginActionCommands returns the `shiki plugins action` subcommand group,
// which provides runtime-routed access to manifest-declared plugin actions.
//
// Manifest command names use the plugins root's runtime dispatch. This group
// also exposes raw action IDs and discovery without building an offline copy
// of the host's command registry.
//
//   - invoke <plugin-id> <action-id>: direct invocation for scripting
//   - list-commands [plugin-id]:    discover available actions
func (a *app) pluginActionCommands() *cobra.Command {
	root := &cobra.Command{
		Use:   "action",
		Short: "Invoke or discover manifest-declared plugin actions",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(a.pluginActionInvokeCmd(), a.pluginActionListCmd())
	return root
}

func (a *app) pluginActionInvokeCmd() *cobra.Command {
	var jsonOut bool
	var actionFlags map[string]string
	cmd := &cobra.Command{
		Use:   "invoke <plugin-id> <action-id>",
		Short: "Invoke a plugin action by ID",
		Long: `Invoke a manifest-declared action on a running local plugin.

Use --flag name=value to pass plugin arguments (repeatable).
Use --json to request structured JSON output.`,
		Example: `  shiki plugins action invoke <plugin-id> <action-id> --json
  shiki plugins action invoke <plugin-id> list-actions`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID, actionID := args[0], args[1]
			return a.withClient(cmd.Context(), true, func(c *client.Client) error {
				return a.invokePluginAction(cmd.Context(), c, pluginID, actionID, "", jsonOut, actionFlags)
			})
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Return JSON output from the action")
	cmd.Flags().StringToStringVar(&actionFlags, "flag", nil, "Plugin argument as name=value (repeatable)")
	return cmd
}

func (a *app) pluginActionListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list-commands [plugin-id]",
		Short: "List manifest-declared actions for a plugin",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withClient(cmd.Context(), true, func(c *client.Client) error {
				return a.listPluginActions(cmd.Context(), c, args, jsonOut)
			})
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print structured JSON")
	return cmd
}

// invokePluginAction sends InvokePluginAction to the agent and prints the result.
func (a *app) invokePluginAction(ctx context.Context, c *client.Client, pluginID, actionID, commandName string, jsonOut bool, flags map[string]string) error {
	reqMap := map[string]any{
		"plugin_id":     pluginID,
		"side":          "local",
		"action_id":     actionID,
		"invocation_id": rand.Text(),
		"flags":         pluginActionFlags(flags, jsonOut),
	}
	if commandName != "" {
		reqMap["command_name"] = commandName
	}
	reqStruct, err := structpb.NewStruct(reqMap)
	if err != nil {
		return fmt.Errorf("build action request: %w", err)
	}

	resStruct, err := c.Actions().InvokePluginAction(ctx, reqStruct)
	if err != nil {
		return err
	}

	result, err := sdk.UnmarshalActionResult(resStruct)
	if err != nil {
		return fmt.Errorf("parse action result: %w", err)
	}
	return a.printActionResult(a.output, result, jsonOut)
}

// listPluginActions prints the available actions for a plugin (or usage text).
func (a *app) listPluginActions(ctx context.Context, c *client.Client, args []string, jsonOut bool) error {
	w := a.output
	if w == nil {
		w = os.Stdout
	}
	if len(args) == 0 {
		fmt.Fprintln(w, "Provide a plugin-id to list its available actions.")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Example:")
		fmt.Fprintln(w, "  shiki plugins action list-commands <plugin-id>")
		return nil
	}
	pluginID := args[0]

	// Discovery is a real action; preserve transport, admission and worker errors.
	reqMap := map[string]any{
		"plugin_id":     pluginID,
		"side":          "local",
		"action_id":     "list-actions",
		"invocation_id": rand.Text(),
		"flags":         map[string]any{},
	}
	reqStruct, err := structpb.NewStruct(reqMap)
	if err != nil {
		return err
	}
	resStruct, err := c.Actions().InvokePluginAction(ctx, reqStruct)
	if err != nil {
		return err
	}
	result, err := sdk.UnmarshalActionResult(resStruct)
	if err != nil {
		return err
	}
	return a.printActionResult(w, result, jsonOut)
}

// printActionResult renders an ActionResult to w.
func (a *app) printActionResult(w io.Writer, result *sdk.ActionResult, jsonOut bool) error {
	if w == nil {
		w = os.Stdout
	}
	if result == nil {
		return nil
	}
	if jsonOut {
		out := map[string]any{"kind": actionResultKind(result.Kind), "done": result.Done}
		switch result.Kind {
		case sdk.ActionResultText:
			out["text"] = result.Text
		case sdk.ActionResultJSON:
			out["json"] = result.JSON
		case sdk.ActionResultNotify:
			if result.Notify != nil {
				out["notify"] = map[string]any{"level": result.Notify.Level, "title": result.Notify.Title, "message": result.Notify.Message}
			}
		case sdk.ActionResultForm:
			if result.Form != nil {
				out["form"] = map[string]any{"title": result.Form.Title, "fields": result.Form.Fields}
			}
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(b))
		return nil
	}

	switch result.Kind {
	case sdk.ActionResultText:
		fmt.Fprint(w, result.Text)
		if !strings.HasSuffix(result.Text, "\n") {
			fmt.Fprintln(w)
		}
	case sdk.ActionResultJSON:
		if result.JSON != nil {
			b, err := json.MarshalIndent(result.JSON, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(w, string(b))
		}
	case sdk.ActionResultNotify:
		if result.Notify != nil {
			fmt.Fprintf(w, "[%s] %s: %s\n", result.Notify.Level, result.Notify.Title, result.Notify.Message)
		}
	case sdk.ActionResultForm:
		if result.Form != nil {
			fmt.Fprintf(w, "Form: %s\n", result.Form.Title)
			if result.Form.Description != "" {
				fmt.Fprintln(w, result.Form.Description)
			}
			for _, f := range result.Form.Fields {
				fmt.Fprintf(w, "  %s (%s)", f.Key, f.Type)
				if f.Label != "" {
					fmt.Fprintf(w, " — %s", f.Label)
				}
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "(interactive form rendering requires TUI/GUI; use --json for structured output)")
		}
	}
	return nil
}

func actionResultKind(k sdk.ActionResultKind) string {
	switch k {
	case sdk.ActionResultText:
		return "text"
	case sdk.ActionResultForm:
		return "form"
	case sdk.ActionResultJSON:
		return "json"
	case sdk.ActionResultNotify:
		return "notification"
	}
	return "unknown"
}

func pluginActionFlags(flags map[string]string, jsonOut bool) map[string]any {
	out := make(map[string]any, len(flags)+1)
	for k, v := range flags {
		out[k] = v
	}
	if jsonOut {
		out["json"] = "true"
	}
	return out
}
