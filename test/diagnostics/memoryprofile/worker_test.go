//go:build memoryprofile

package memoryprofile_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	portclient "github.com/DDSRem-Dev/Shikigami/plugins/port/client"
	portserver "github.com/DDSRem-Dev/Shikigami/plugins/port/server"
	syncclient "github.com/DDSRem-Dev/Shikigami/plugins/sync/client"
	syncserver "github.com/DDSRem-Dev/Shikigami/plugins/sync/server"
)

var diagnosticContext = context.Background()

// TestMain intercepts only the private wrapper invocation before Go's testing
// flags/output can interfere with go-plugin's stdout handshake. Production
// workers and SDK are unchanged; these are diagnostic, not shipped entrypoints.
func TestMain(m *testing.M) {
	root, name := os.Getenv("SHIKI_MEMORY_WORKER_ROOT"), os.Getenv("SHIKI_MEMORY_WORKER_NAME")
	if root == "" {
		stop := func() {}
		for _, arg := range os.Args[1:] {
			if strings.Contains(arg, "TestRepeatedWorkerMemoryWindows") {
				diagnosticContext, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
				break
			}
		}
		code := m.Run()
		stop()
		os.Exit(code)
	}
	side := ""
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--side=") {
			side = strings.TrimPrefix(arg, "--side=")
		}
	}
	if !filepath.IsAbs(root) || (name != "sync" && name != "port") || (side != "client" && side != "server") {
		os.Exit(2)
	}
	if _, err := profilePolicy(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	runtime.MemProfileRate = 64 << 10
	role := name + "-" + side
	listener, err := net.Listen("unix", filepath.Join(root, role+".profile.sock"))
	if err != nil {
		os.Exit(3)
	}
	if err = os.Chmod(listener.Addr().String(), 0600); err != nil {
		os.Exit(3)
	}
	identityCtx, identityCancel := context.WithTimeout(context.Background(), 5*time.Second)
	identity, identityErr := currentReady(identityCtx)
	identityCancel()
	if identityErr != nil {
		os.Exit(3)
	}
	if err = saveJSON(filepath.Join(root, role+".ready.json"), identity); err != nil {
		os.Exit(3)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err == nil && validLabel(strings.TrimSpace(line)) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				err = collect(ctx, root, role, strings.TrimSpace(line))
				cancel()
			} else if err == nil {
				err = fmt.Errorf("invalid profile label")
			}
			message := "ok\n"
			if err != nil {
				message = err.Error() + "\n"
			}
			_, _ = conn.Write([]byte(message))
			conn.Close()
		}
	}()
	switch role {
	case "sync-client":
		w := syncclient.New()
		err = sdk.Serve(side, w, sdk.Services{Sync: w})
	case "sync-server":
		w := syncserver.New()
		err = sdk.Serve(side, w, sdk.Services{Files: w})
	case "port-client":
		w := portclient.New()
		err = sdk.Serve(side, w, sdk.Services{Port: w})
	case "port-server":
		w := portserver.New()
		err = sdk.Serve(side, w, sdk.Services{Ports: w})
	}
	listener.Close()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func workerManifests(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(root, "plugins")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := profilePolicy()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for _, name := range []string{"sync", "port"} {
		wrapper := "#!/bin/sh\nexport SHIKI_MEMORY_WORKER_ROOT=" + quote(root) + "\nexport SHIKI_MEMORY_WORKER_NAME=" + quote(name) + "\nexport SHIKI_MEMORY_PROFILE_POLICY=" + quote(policy) + "\nexport GORACE=" + quote("halt_on_error=1 log_path="+filepath.Join(root, "race")) + "\nexec " + quote(exe) + " \"$@\"\n"
		binary := "shiki-plugin-" + name
		if err := os.WriteFile(filepath.Join(bin, binary), []byte(wrapper), 0700); err != nil {
			t.Fatal(err)
		}
		offer, err := sdk.BuiltinProtocol(name, "both")
		if err != nil {
			t.Fatal(err)
		}
		manifest := host.Manifest{Name: name, Version: "dev", Side: "both", Executable: binary, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(wrapper))), Protocol: offer}
		if err := saveJSON(filepath.Join(bin, name+".manifest.json"), manifest); err != nil {
			t.Fatal(err)
		}
	}
}

func sampleWorker(t *testing.T, ctx context.Context, root, role, label string) int {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(root, role+".profile.sock"))
	if err != nil {
		t.Fatal(role, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintln(conn, label); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "ok\n" {
		t.Fatalf("%s sample: %s %v", role, line, err)
	}
	data, err := os.ReadFile(filepath.Join(root, role+"."+label+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var point sample
	if err := json.Unmarshal(data, &point); err != nil {
		t.Fatal(err)
	}
	return point.PID
}
