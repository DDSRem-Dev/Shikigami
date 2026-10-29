//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
)

func resourceTestTimeout(t *testing.T) time.Duration {
	// Opt-in subprocesses are race-enabled: every short CLI invocation also
	// pays the race runtime's exit delay. Preserve the baseline scenario's
	// shutdown/recovery budget after the additional attach/cancel cycles.
	return 3*time.Minute + time.Duration(resources.Cycles(t))*8*time.Second + applicationSoakDuration(t)
}

func resourceBuildArgs(t *testing.T, args ...string) []string {
	flags := []string{"build"}
	if resources.Cycles(t) != 0 || applicationSoakDuration(t) != 0 {
		flags = append(flags, "-race")
	}
	return append(flags, args...)
}

func exerciseTerminalResources(t *testing.T, ctx context.Context, environment []string, binary, configPath, stateDir string, daemonPID int, session *pb.Session, cli func(...string) []byte) {
	t.Helper()
	count := resources.Cycles(t)
	if count == 0 {
		return
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	agentPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || agentPID <= 1 {
		t.Fatal("invalid agent instance PID", err)
	}
	runCycle := func(i int) {
		t.Helper()
		attached, terminalFile := terminal(t, ctx, environment, binary, "--config", configPath, "attach", session.Id, "--read-only", "--reconnect=false")
		defer terminalFile.Close()
		readTerminal(t, ctx, terminalFile, "E2E_READY_世界")
		if i%2 == 0 {
			if _, err := terminalFile.Write([]byte{17}); err != nil {
				t.Fatal(err)
			}
		} else if err := attached.command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		attached.wait(t, ctx, i%2 != 0)
		await(t, ctx, func(context.Context) error {
			current := new(pb.Session)
			decode(t, cli("sessions", "info", session.Id), current)
			if current.IsConnected {
				return errors.New("attachment is still registered after CLI exit")
			}
			if current.ClaudePid != session.ClaudePid || current.ClaudeStatus != "running" {
				t.Fatal("attach/cancel changed the persistent tmux pane", current)
			}
			return nil
		})
	}
	for i := 0; i < 2; i++ {
		runCycle(i)
	}
	baseline := resources.Capture(t, os.Getpid(), agentPID, daemonPID)
	for i := 0; i < count; i++ {
		runCycle(i)
		if (i+1)%4 == 0 || i+1 == count {
			resources.Restored(t, fmt.Sprintf("CLI terminal detach/cancel %d/%d", i+1, count), baseline, 4, 4)
		}
	}
}
