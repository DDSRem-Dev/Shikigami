package main

import (
	"context"
	"fmt"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

// printVerboseLogs runs only after Terminal has restored stdin/stdout. The
// supervisor's bounded history is the backlog; no logger competes with raw ANSI.
func (a *app) printVerboseLogs(ctx context.Context, c *client.Client) {
	if !a.verbose || ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	list, err := c.Agent().ListPlugins(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
	if err != nil || len(list.GetPlugins()) == 0 {
		return
	}
	fmt.Fprintln(a.errorOutput, "Latest host-redacted local plugin logs (bounded tail; not a complete replay):")
	for _, plugin := range list.Plugins {
		records, err := c.PluginLogTail(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, plugin.PluginId, 20)
		if err != nil {
			fmt.Fprintf(a.errorOutput, "[%s] logs unavailable: %s\n", plugin.PluginId, safeText(err.Error()))
			continue
		}
		for _, record := range records {
			fmt.Fprintf(a.errorOutput, "[%s] %d [%s] %s\n", plugin.PluginId, record.Timestamp, safeText(record.Level), safeText(record.Message))
		}
	}
}
