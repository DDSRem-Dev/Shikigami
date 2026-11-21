//go:build linux || darwin

package runtime

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func exitedTestChild(t *testing.T, component childComponent, script string) *child {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", script)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := observeChild(command, component)
	t.Cleanup(func() { _ = command.Process.Kill(); <-process.done })
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("test child failed to exit")
	}
	return process
}

func TestRuntimeChildExitReportsSafeStatus(t *testing.T) {
	for _, test := range []struct {
		name      string
		component childComponent
		script    string
		stopped   bool
		status    string
	}{
		{"chrome-close", childChrome, "exit 0", true, "chrome exited normally (exit_code=0)"},
		{"dependency-close", childClipboard, "exit 0", false, "clipboard exited (exit_code=0)"},
		{"dependency-error", childIME, "exit 7", false, "ime exited (exit_code=7)"},
		{"chrome-signal", childChrome, "kill -TERM $$", false, "chrome exited (signal=15)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			process := exitedTestChild(t, test.component, test.script)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			reason, err := awaitRuntimeExit(ctx, []*child{process}, nil, nil)
			if test.stopped {
				if err != nil || !strings.Contains(reason, test.status) {
					t.Fatalf("normal Chrome close was not stopped: %q %v", reason, err)
				}
			} else if reason != "" || err == nil || !strings.Contains(err.Error(), test.status) {
				t.Fatalf("dependency failure lost status: %q %v", reason, err)
			}
		})
	}
	for _, result := range []childExit{
		{component: "secret-argv-or-page", code: -1, waitError: true},
		{component: childChrome, code: -1, signal: syscall.SIGKILL},
	} {
		if strings.Contains(result.Error(), "secret-") || result.normalChrome() {
			t.Fatalf("unsafe or incorrect classification: %s", result.Error())
		}
	}
	// Wait on an unstarted command fails without an ExitError. Do not expose
	// command paths or arbitrary Wait errors as a fabricated numeric exit.
	unstarted := observeChild(exec.Command("/private/secret-command"), childChrome)
	<-unstarted.done
	if got := unstarted.exit.Error(); got != "browser component chrome exited (wait_error); manual start required" {
		t.Fatalf("unclassified Wait failure was not safely reported: %s", got)
	}
}

func TestRuntimeChildCompletionIsBroadcast(t *testing.T) {
	process := exitedTestChild(t, childChrome, "exit 0")
	var readers sync.WaitGroup
	for range 16 {
		readers.Go(func() {
			<-process.done
			if !process.exit.normalChrome() {
				t.Error("completion lost immutable result")
			}
		})
	}
	readers.Wait()
}

func TestRuntimeCancellationWinsReadyExitAndFailure(t *testing.T) {
	process := exitedTestChild(t, childIME, "exit 9")
	secretFailure := make(chan struct{})
	close(secretFailure)
	for range 100 {
		serveDone := make(chan error, 1)
		serveDone <- errors.New("server failed")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		reason, err := awaitRuntimeExit(ctx, []*child{process}, secretFailure, serveDone)
		if reason != "" || err != nil {
			t.Fatalf("requested stop misclassified: %q %v", reason, err)
		}
	}
}

func TestNormalChromeCloseDoesNotHideCompletedDependency(t *testing.T) {
	chrome := exitedTestChild(t, childChrome, "exit 0")
	dependency := exitedTestChild(t, childDisplay, "exit 4")
	for range 100 {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		reason, err := awaitRuntimeExit(ctx, []*child{chrome, dependency}, nil, nil)
		cancel()
		if reason != "" || err == nil || !strings.Contains(err.Error(), "display exited (exit_code=4)") {
			t.Fatalf("Chrome close hid a completed dependency: %q %v", reason, err)
		}
	}
}
