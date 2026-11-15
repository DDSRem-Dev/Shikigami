//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRegionBrowserInteractiveConfirmationBindsTargetAndConsequence(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		labels []string
	}{
		{[]string{"region", "apply", "plan_cli", "--expected-revision", "host-v1"}, []string{"plan_cli", "whole host", "DNS", "not fully reversible", "not restarted"}},
		{[]string{"region", "confirm", "operation_cli"}, []string{"operation_cli", "connectivity checks", "timed DNS rollback"}},
		{[]string{"region", "rollback", "operation_cli"}, []string{"operation_cli", "cannot be restored"}},
		{[]string{"browser", "stop", "profile_cli"}, []string{"profile_cli", "profile data remains", "unsaved pages"}},
		{[]string{"browser", "start", "profile_cli", "--replace-running"}, []string{"profile_cli", "other running profile", "unsaved pages"}},
		{[]string{"browser", "profile", "boot", "profile_cli", "--replace-boot"}, []string{"profile_cli", "single boot target", "replace any other"}},
		{[]string{"browser", "profile", "delete", "profile_cli"}, []string{"profile_cli", "Permanently delete", "cookies", "unsaved pages"}},
	} {
		for _, answer := range []string{"yes", "no", ""} {
			t.Run(strings.Join(tc.args[:2], "/")+"/answer="+answer, func(t *testing.T) {
				p := &extensionCLIProbe{}
				path := extensionCLIFixture(t, p)
				master, slave, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				defer slave.Close()
				if _, err := master.Write([]byte(answer + "\n")); err != nil {
					t.Fatal(err)
				}
				var diagnostics bytes.Buffer
				root := newRoot(slave, slave, &diagnostics)
				args := append([]string{"--config", path, "--server", "cli-server", "--request-id", "interactive-request"}, tc.args...)
				root.SetArgs(args)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				err = root.ExecuteContext(ctx)
				p.mu.Lock()
				mutations := len(p.mutations)
				p.mu.Unlock()
				if answer == "yes" {
					if err != nil || mutations != 1 {
						t.Fatalf("confirmed operation: mutations=%d err=%v", mutations, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "canceled") || mutations != 0 {
					t.Fatalf("declined operation: mutations=%d err=%v", mutations, err)
				}
				for _, label := range append(tc.labels, "srv_cli", "type yes") {
					if !strings.Contains(diagnostics.String(), label) {
						t.Errorf("confirmation omitted %q: %s", label, diagnostics.String())
					}
				}
			})
		}
	}
}
