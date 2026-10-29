//go:build darwin && e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

func launchdInstallResult(ctx context.Context, environment []string, helper string, args ...string) (install.Result, error) {
	var result install.Result
	cmd := exec.CommandContext(ctx, helper, args...)
	cmd.Env = environment
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	var exited *exec.ExitError
	if err != nil && (!errors.As(err, &exited) || exited.ExitCode() != 3) {
		return result, fmt.Errorf("installer %v: %w (%s)", args, err, stderr.String())
	}
	if parseErr := json.Unmarshal(data, &result); parseErr != nil {
		return result, fmt.Errorf("installer response: %w (%s)", parseErr, data)
	}
	if err != nil && result.State != "application_pending" && !strings.HasPrefix(result.State, "node_") {
		return result, fmt.Errorf("unexpected pending exit with state %s", result.State)
	}
	return result, nil
}

func buildLaunchdBundle(t *testing.T, ctx context.Context, repository, root, version string) (string, string) {
	t.Helper()
	directory := filepath.Join(root, version)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	build := func(args ...string) {
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = repository
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bundle build: %v\n%s", err, out)
		}
	}
	flags := "-X github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo.Version=" + version
	build("build", "-race", "-buildvcs=false", "-mod=readonly", "-ldflags", flags, "-o", directory+"/", "./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon", "./cmd/shiki-install")
	for _, plugin := range []string{"sync", "port"} {
		build("build", "-race", "-buildvcs=false", "-mod=readonly", "-ldflags", flags, "-o", filepath.Join(directory, "shiki-plugin-"+plugin), "./plugins/"+plugin)
		build("run", "./scripts/plugin-manifest.go", "--dir", directory, "--id", plugin, "--version", version)
	}
	for _, name := range []string{"install.sh", "install-client.sh", "uninstall.sh", "LICENSE"} {
		source := filepath.Join(repository, "scripts", name)
		mode := os.FileMode(0700)
		if name == "LICENSE" {
			source, mode = filepath.Join(repository, name), 0600
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	// This pin authenticates locally built test fixtures only; it is not a
	// publisher signature or acceptance of any external release source.
	pin, err := install.CreateManifest(directory, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("built isolated race-enabled test bundle %s", version)
	return directory, pin
}

func launchdWait(t *testing.T, ctx context.Context, timeout time.Duration, probe func() error) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		err := probe()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatal("launchd readiness timeout", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func launchdPID(t *testing.T, ctx context.Context, target, prefix string) int {
	t.Helper()
	data, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).Output()
	if err != nil || !strings.Contains(string(data), "program = "+filepath.Join(prefix, "current", "shiki-agent")+"\n") {
		t.Fatalf("launchd job is not this test's agent: %v\n%s", err, data)
	}
	match := regexp.MustCompile(`(?m)^\s*pid = ([0-9]+)$`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatalf("launchd agent is not running:\n%s", data)
	}
	pid, err := strconv.Atoi(string(match[1]))
	if err != nil || pid <= 1 {
		t.Fatal("invalid launchd PID", err)
	}
	return pid
}

func launchdPluginPIDs(t *testing.T, ctx context.Context, parent int, prefix string) []int {
	t.Helper()
	data, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var result []int
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != strconv.Itoa(parent) || !strings.HasPrefix(filepath.Base(fields[2]), "shiki-plugin-") {
			continue
		}
		executable, err := filepath.EvalSymlinks(fields[2])
		canonicalPrefix, prefixErr := filepath.EvalSymlinks(prefix)
		if err != nil || prefixErr != nil || !strings.HasPrefix(executable, canonicalPrefix+"/") {
			t.Fatal("plugin child executable is outside the owned install", err, prefixErr)
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 1 {
			t.Fatal("invalid plugin PID", err)
		}
		result = append(result, pid)
	}
	return result
}

func launchdWaitGone(t *testing.T, ctx context.Context, pids []int) {
	t.Helper()
	launchdWait(t, ctx, 20*time.Second, func() error {
		for _, pid := range pids {
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("process %d still exists: %v", pid, err)
			}
		}
		return nil
	})
}

func launchdAssertUnlocked(t *testing.T, state string) {
	t.Helper()
	err := filepath.WalkDir(state, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !strings.HasSuffix(entry.Name(), ".lock") {
			return walkErr
		}
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return fmt.Errorf("lock still owned at %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
