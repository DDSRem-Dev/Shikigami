//go:build memoryprofile

package memoryprofile_test

import (
	"context"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

type daemonOwnerObservation struct {
	owner   weak.Pointer[server.Runtime]
	cleaned <-chan struct{}
}

// No cleanup registered with testing.T retains the Runtime, its Service, or a
// bound method. AddCleanup carries only the independent notification channel.
//
//go:noinline
func closedDaemonOwner(t *testing.T) (func() error, daemonOwnerObservation) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shiki-owner-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cert, key := certificateFiles(t, root)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultDaemon(account.Username, filepath.Join(root, "state"))
	cfg.ProjectsDir = filepath.Join(root, "projects")
	cfg.PluginsDir = filepath.Join(root, "plugins")
	cfg.Tailscale.Enabled = false
	cfg.Claude.Executable = "/bin/sh"
	cfg.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: cert, KeyFile: key}
	r, err := server.StartDaemon(context.Background(), cfg, server.DaemonOptions{Build: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	cleaned := make(chan struct{})
	runtime.AddCleanup(r, func(done chan struct{}) { close(done) }, cleaned)
	observation := daemonOwnerObservation{owner: weak.Make(r), cleaned: cleaned}
	closeHost := r.Close
	if err := closeHost(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "run", "admin.sock")); !os.IsNotExist(err) {
		t.Fatalf("closed daemon left admin socket: %v", err)
	}
	return closeHost, observation
}

// Keep temporary strong references returned by weak.Value off the GC caller's
// stack; this function never returns the referent.
//
//go:noinline
func daemonOwnerAlive(observation daemonOwnerObservation) bool {
	return observation.owner.Value() != nil
}

func awaitDaemonOwnerRelease(t *testing.T, observation daemonOwnerObservation) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if !daemonOwnerAlive(observation) {
			select {
			case <-observation.cleaned:
				return
			default:
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("closed daemon owner remained reachable or cleanup did not run; weak_alive=%t", daemonOwnerAlive(observation))
}

//go:noinline
func retainClosedDaemonThroughGC(t *testing.T) daemonOwnerObservation {
	t.Helper()
	closeHost, observation := closedDaemonOwner(t)
	for range 3 {
		runtime.GC()
		if !daemonOwnerAlive(observation) {
			t.Fatal("positive control lost bound Close owner")
		}
		select {
		case <-observation.cleaned:
			t.Fatal("cleanup ran while bound Close was retained")
		default:
		}
	}
	runtime.KeepAlive(closeHost)
	return observation
}

func TestClosedDaemonBoundCloseOwnsRuntime(t *testing.T) {
	observation := retainClosedDaemonThroughGC(t)
	// All strong owners are now outside the observer's scope.
	awaitDaemonOwnerRelease(t, observation)
}

func TestClosedDaemonDiagnosticReleasesBeforeCollection(t *testing.T) {
	closeHost, observation := closedDaemonOwner(t)
	if err := closeBeforeCollection(&closeHost); err != nil {
		t.Fatal(err)
	}
	awaitDaemonOwnerRelease(t, observation)
	// Match the real diagnostic's still-live deferred cleanup owner. Without
	// explicitly dropping the bound method, this observation must fail.
	runtime.KeepAlive(closeHost)
}
