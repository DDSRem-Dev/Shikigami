package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// Exercise the production onboarding command adapter over real UDS/HTTP2 and
// generated handlers, including the agent identity and resolved server headers.
func TestSetupCLIUsesExistingAgentRoutingAndSeparateMutationIDs(t *testing.T) {
	probe := &extensionCLIProbe{profiles: []*epb.BrowserProfile{}}
	path := extensionCLIFixture(t, probe)
	var output bytes.Buffer
	backend := setupCLI{app: &app{configPath: path, timeout: time.Second, output: &output, errorOutput: &output}}
	region := new(epb.RegionStatus)
	if err := backend.query(t.Context(), region, "--server", "cli-server", "region", "inspect"); err != nil {
		t.Fatal(err)
	}
	if !region.HelperAvailable || region.PendingOperationId != "pending_cli" {
		t.Fatal("read-only system state was changed or lost", region)
	}
	if err := backend.run(t.Context(), false, "--server", "cli-server", "browser", "profile", "create", "work", "--name", "Work", "--yes"); err != nil {
		t.Fatal(err)
	}
	if err := backend.run(t.Context(), false, "--server", "cli-server", "browser", "start", "work", "--yes"); err != nil {
		t.Fatal(err)
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.mutations) != 2 {
		t.Fatal("unexpected mutation count", probe.mutations)
	}
	create := probe.mutations[0].(*epb.PutBrowserProfileRequest)
	start := probe.mutations[1].(*epb.BrowserActionRequest)
	if create.RequestId == "" || start.RequestId == "" || create.RequestId == start.RequestId || create.Profile.BootStart || start.ReplaceRunning || !start.Confirm {
		t.Fatal("onboarding bypassed independent requests or browser confirmation", create, start)
	}
	if output.Len() != 0 {
		t.Fatal("quiet setup actions leaked diagnostic output", output.String())
	}
}

func TestSetupHelpAndInvalidComponentsNeedNoInstalledAgent(t *testing.T) {
	var output bytes.Buffer
	root := newRoot(nil, &output, &output)
	root.SetArgs([]string{"setup", "--help"})
	if err := root.Execute(); err != nil || !strings.Contains(output.String(), "--components") {
		t.Fatal("setup help unavailable", err, output.String())
	}
	root = newRoot(nil, &output, &output)
	root.SetArgs([]string{"setup", "--components=unsupported"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--components") {
		t.Fatal("invalid selection reached agent or system state", err)
	}
}
