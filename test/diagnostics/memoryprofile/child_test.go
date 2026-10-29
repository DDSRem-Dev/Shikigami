//go:build memoryprofile

package memoryprofile_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

type childCommand struct{ Operation, Label string }
type ready struct {
	PID        int
	Token      string `json:"token,omitempty"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
}

// closeBeforeCollection completes shutdown before the diagnostic samples the
// closed host. Its owner is shared with the deferred emergency cleanup.
func closeBeforeCollection(closeHost *func() error) error {
	if err := (*closeHost)(); err != nil {
		return err
	}
	// A bound Runtime.Close method retains the entire stopped daemon graph.
	// Release it before collection; on failure retain it for deferred cleanup.
	*closeHost = nil
	return nil
}

// Only the explicit tagged diagnostic invokes this helper as a child. Each
// child owns exactly one production host. Profiling control uses its inherited
// stdin and private fixture files, never a product RPC or public pprof server.
func TestMemoryProfileHost(t *testing.T) {
	role := os.Getenv("SHIKI_MEMORY_FIXTURE_ROLE")
	if role == "" {
		return
	}
	root := os.Getenv("SHIKI_MEMORY_FIXTURE_ROOT")
	if !filepath.IsAbs(root) || (role != "agent" && role != "daemon") {
		t.Fatal("invalid private fixture identity")
	}
	if _, err := profilePolicy(); err != nil {
		t.Fatal(err)
	}
	runtime.MemProfileRate = 64 << 10
	// Diagnostic-only ceiling; the parent's command context sets the tighter
	// lifetime for each selected scenario, including the repeated windows.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	done := make(chan error, 1)
	info, err := currentReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var closeHost func() error
	switch role {
	case "agent":
		cfg, err := config.LoadClient(filepath.Join(root, "client.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		go func() { done <- agent.RunConfig(ctx, cfg, "dev") }()
		closeHost = func() error { cancel(); return <-done }
	case "daemon":
		cfg, err := config.LoadDaemon(filepath.Join(root, "daemon.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		info.Token, _, err = r.Service.Auth.Create(cfg.RunUser, []string{"*"}, time.Hour)
		if err != nil {
			r.Close()
			t.Fatal(err)
		}
		closeHost = r.Close
	}
	closed := false
	defer func() {
		if !closed {
			if err := closeHost(); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := saveJSON(filepath.Join(root, role+".ready.json"), info); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		var command childCommand
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			t.Fatal(err)
		}
		switch command.Operation {
		case "sample":
			if !validLabel(command.Label) {
				t.Fatal("invalid sample label")
			}
			if err := collect(ctx, root, role, command.Label); err != nil {
				t.Fatal(err)
			}
		case "close":
			if err := closeBeforeCollection(&closeHost); err != nil {
				t.Fatal(err)
			}
			closed = true
			// The host has exited, but this diagnostic process remains long enough
			// to observe what survives its resource and context ownership cleanup.
			finalCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			err := collect(finalCtx, root, role, "closed")
			stop()
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
			t.Fatal("unknown diagnostic command")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("%s diagnostic input ended before orderly close", role)
}
