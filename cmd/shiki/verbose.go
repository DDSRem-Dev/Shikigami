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
	fmt.Fprintln(a.errorOutput, "Latest host-redacted local plugin logs (bounded tail; not a complete replay):")
	for _, plugin := range []string{"sync", "port"} {
		records, err := c.PluginLogTail(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, plugin, 20)
		if err != nil {
			fmt.Fprintf(a.errorOutput, "[%s] logs unavailable: %s\n", plugin, safeText(err.Error()))
		}
		for _, record := range records {
			fmt.Fprintf(a.errorOutput, "[%s] %d [%s] %s\n", plugin, record.Timestamp, safeText(record.Level), safeText(record.Message))
		}
	}
}
