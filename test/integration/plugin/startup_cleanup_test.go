package plugin_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
)

// Establish the external library behavior modeled by the deterministic host
// cleanup regression: Kill before Client does not disable a later Start.
func TestGoPluginKillBeforeClientCanStillStart(t *testing.T) {
	cfg, _ := pluginConfig(t, "")
	if err := os.MkdirAll(cfg.RunDir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cfg.Manifest.Executable, append(cfg.Args, "--side=client")...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "SHIKI_TEST_PLUGIN=1", goplugin.EnvUnixSocketDir + "=" + cfg.RunDir}
	client := goplugin.NewClient(&goplugin.ClientConfig{HandshakeConfig: sdk.Handshake, Plugins: goplugin.PluginSet{sdk.DispenseName: sdk.ClientPlugin()}, Cmd: cmd,
		AutoMTLS: true, AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC}, StartTimeout: 5 * time.Second, SkipHostEnv: true,
		UnixSocketConfig: &goplugin.UnixSocketConfig{TempDir: cfg.RunDir}, Logger: hclog.NewNullLogger(), Stderr: io.Discard, SyncStdout: io.Discard, SyncStderr: io.Discard})
	defer client.Kill()
	start := make(chan struct{})
	result := make(chan error, 1)
	go func() { <-start; _, err := client.Client(); result <- err }()
	client.Kill()
	close(start)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("late Client did not finish its real plugin handshake")
	}
	address := client.ReattachConfig()
	if address == nil || address.Pid <= 1 {
		t.Fatal("late Client did not identify a real child")
	}
	if err := syscall.Kill(address.Pid, 0); err != nil {
		t.Fatalf("late-started child was not alive: %v", err)
	}
	client.Kill()
	if err := syscall.Kill(address.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("final Kill did not reap the late-started child: %v", err)
	}
	t.Logf("real late-started child %d reaped by final Kill", address.Pid)
}

func TestCanceledLaunchReapsPendingClientStart(t *testing.T) {
	cfg, _ := pluginConfig(t, "")
	marker := filepath.Join(t.TempDir(), "child.pid")
	cfg.Env = append(cfg.Env, "SHIKI_PID_MARKER="+marker)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	instance, err := host.Launch(ctx, cfg)
	if instance != nil || !errors.Is(err, context.Canceled) {
		if instance != nil {
			_ = instance.Stop(context.Background())
		}
		t.Fatalf("already canceled launch: instance=%v err=%v", instance != nil, err)
	}
	// The caller has returned, but the owner still joins the pending Client
	// start and final Kill before deleting the runtime and releasing its lease.
	deadline := time.Now().Add(8 * time.Second)
	for {
		entries, err := os.ReadDir(cfg.RunDir)
		if err == nil && len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("canceled launch did not finish owned cleanup: %v %v", entries, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal("expected real pending Client start to publish its child identity", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid owned child marker: %q %v", data, err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("canceled Launch left its real pending child alive: %v", err)
	}
	t.Logf("canceled Launch joined and reaped real pending child %d", pid)
	callbackLeaseHeld(t, cfg.StateDir, false)
}
