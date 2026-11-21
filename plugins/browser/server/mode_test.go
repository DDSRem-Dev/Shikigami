//go:build linux

package server

import (
	"errors"
	"testing"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

func TestBrowserLaunchModesRequireConfirmedReplacement(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	first := startProfile(t, worker, "one", "normal")
	if first.Mode != browserruntime.ModeBrowse {
		t.Fatal("new launch not normal browsing", first)
	}
	if _, err := worker.DiagnoseBrowser(t.Context(), &epb.DiagnosticRequest{ProfileId: "one"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("normal browsing allowed native diagnostics", err)
	}
	reused := startProfile(t, worker, "one", "reuse")
	if reused.InstanceId != first.InstanceId || manager.launches != 1 {
		t.Fatal("same mode restarted", reused)
	}
	for _, request := range []*epb.BrowserActionRequest{
		{ProfileId: "one", RequestId: "no-replace", Diagnostics: true, Confirm: true},
		{ProfileId: "one", RequestId: "no-confirm", Diagnostics: true, ReplaceRunning: true},
	} {
		if _, err := worker.StartBrowser(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("mode switch lacked confirmation", err)
		}
		if manager.stops != 0 || manager.launches != 1 {
			t.Fatal("rejected switch mutated runtime")
		}
	}
	request := &epb.BrowserActionRequest{ProfileId: "one", RequestId: "diagnostic", Diagnostics: true, ReplaceRunning: true, Confirm: true}
	diagnostic, err := worker.StartBrowser(t.Context(), request)
	if err != nil || diagnostic.Mode != browserruntime.ModeDiagnostic || diagnostic.InstanceId == first.InstanceId || manager.launches != 2 || manager.stops != 1 {
		t.Fatal("explicit diagnostic transition", diagnostic, err)
	}
	if _, err := worker.StartBrowser(t.Context(), request); err != nil || manager.launches != 2 {
		t.Fatal("replay relaunched", err)
	}
	if _, err := worker.StopBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "invalid-stop", Diagnostics: true, Confirm: true}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("stop accepted diagnostics", err)
	}
	status, err := worker.GetBrowserStatus(t.Context(), &epb.BrowserScope{ProfileId: "one"})
	if err != nil || status.Mode != browserruntime.ModeDiagnostic {
		t.Fatal(status, err)
	}
	if _, err := worker.DiagnoseBrowser(t.Context(), &epb.DiagnosticRequest{ProfileId: "one"}); err != nil {
		t.Fatal("diagnostic instance rejected native diagnostics", err)
	}
	normal, err := worker.StartBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "back-to-browse", ReplaceRunning: true, Confirm: true})
	if err != nil || normal.Mode != browserruntime.ModeBrowse || normal.InstanceId == diagnostic.InstanceId {
		t.Fatal(normal, err)
	}
	stopped, err := worker.StopBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "stop", Confirm: true})
	if err != nil || stopped.Mode != browserruntime.ModeBrowse {
		t.Fatal(stopped, err)
	}
}

func TestLegacyBrowserModeIsDiagnosticAndNeverSilentlyReusedAsBrowse(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "original")
	worker.mu.Lock()
	worker.db.Runtime.Mode = ""
	worker.mu.Unlock()
	manager.mu.Lock()
	manager.snapshot.Mode = ""
	manager.mu.Unlock()
	status, err := worker.GetBrowserStatus(t.Context(), &epb.BrowserScope{ProfileId: "one"})
	if err != nil || status.Mode != browserruntime.ModeDiagnostic {
		t.Fatal("legacy mode guessed as normal", status, err)
	}
	if _, err := worker.StartBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "normal"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("legacy diagnostic silently reused", err)
	}
	if manager.launches != 1 || manager.stops != 0 {
		t.Fatal("unconfirmed legacy mode transition")
	}
}

func TestFailedLaunchPreservesObservedNormalChromeExit(t *testing.T) {
	manager := &testManager{launchError: errors.New("Chrome exited before readiness"), launchState: "stopped"}
	state := t.TempDir()
	worker := newWorker(t, state, "boot", manager)
	putProfile(t, worker, "one", false)
	request := &epb.BrowserActionRequest{ProfileId: "one", RequestId: "exit-zero", Confirm: true}
	if _, err := worker.StartBrowser(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal(err)
	}
	status, err := worker.GetBrowserStatus(t.Context(), &epb.BrowserScope{ProfileId: "one"})
	if err != nil || status.State != "stopped" || status.Mode != browserruntime.ModeBrowse {
		t.Fatal("normal exit became crash", status, err)
	}
	if err := worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker = newWorker(t, state, "boot", manager)
	if _, err := worker.StartBrowser(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition || manager.launches != 1 {
		t.Fatal("failed receipt relaunched Chrome", err, manager.launches)
	}
	status, err = worker.GetBrowserStatus(t.Context(), &epb.BrowserScope{ProfileId: "one"})
	if err != nil || status.State != "stopped" || status.Mode != browserruntime.ModeBrowse {
		t.Fatal("persisted exit became crash", status, err)
	}
}
