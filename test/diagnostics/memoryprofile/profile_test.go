//go:build memoryprofile

package memoryprofile_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

var outputDirectory = flag.String("memoryprofile-output", "", "new private artifact directory; required for the explicit diagnostic")

type child struct {
	role    string
	command *exec.Cmd
	input   io.WriteCloser
	done    chan error
	closed  bool
}

func await(ctx context.Context, check func() error) error {
	var last error
	for ctx.Err() == nil {
		if last = check(); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	return fmt.Errorf("%w; last observation: %v", ctx.Err(), last)
}

func startChild(t *testing.T, ctx context.Context, root, role string) *child {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, exe, "-test.run=^TestMemoryProfileHost$", "-test.v")
	command.Env = append(os.Environ(), "SHIKI_MEMORY_FIXTURE_ROLE="+role, "SHIKI_MEMORY_FIXTURE_ROOT="+root, "GORACE=halt_on_error=1 log_path="+filepath.Join(root, "race"))
	log, err := os.OpenFile(filepath.Join(root, role+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = log
	command.Stderr = log
	input, err := command.StdinPipe()
	if err != nil {
		log.Close()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		input.Close()
		log.Close()
		t.Fatal(err)
	}
	c := &child{role: role, command: command, input: input, done: make(chan error, 1)}
	go func() { err := command.Wait(); log.Close(); c.done <- err }()
	t.Cleanup(func() {
		if !c.closed {
			// On assertion failure or workload cancellation, first ask the host
			// to release its supervisors and workers using normal shutdown.
			_ = json.NewEncoder(input).Encode(childCommand{Operation: "close"})
			input.Close()
			select {
			case err := <-c.done:
				if err != nil {
					t.Error(role, "cleanup exit", err)
				}
			case <-time.After(10 * time.Second):
				t.Error(role, "required forced diagnostic cleanup")
				command.Process.Kill()
				<-c.done
			}
			c.closed = true
		}
	})
	wait, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	if err := await(wait, func() error { _, err := os.Stat(filepath.Join(root, role+".ready.json")); return err }); err != nil {
		t.Fatal(role, "startup", err)
	}
	if err := recordReadyIdentities(root); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *child) send(t *testing.T, command childCommand) {
	t.Helper()
	if err := json.NewEncoder(c.input).Encode(command); err != nil {
		t.Fatal(c.role, "command", err)
	}
}
func (c *child) sample(t *testing.T, ctx context.Context, root, label string) {
	t.Helper()
	c.send(t, childCommand{Operation: "sample", Label: label})
	wait, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
	if err := await(wait, func() error { _, err := os.Stat(filepath.Join(root, c.role+"."+label+".json")); return err }); err != nil {
		t.Fatal(c.role, "sample", err)
	}
}
func (c *child) close(t *testing.T) {
	t.Helper()
	c.send(t, childCommand{Operation: "close"})
	c.input.Close()
	select {
	case err := <-c.done:
		c.closed = true
		if err != nil {
			t.Fatal(c.role, "exit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal(c.role, "did not close")
	}
}

func TestFixedRPCMemoryProfile(t *testing.T) {
	if *outputDirectory == "" {
		t.Fatal("explicit -memoryprofile-output directory is required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("diagnostic daemon requires an ordinary user")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(*outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	certificate, key := certificateFiles(t, root)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dc := config.DefaultDaemon(account.Username, filepath.Join(root, "daemon", "state"))
	dc.ProjectsDir = filepath.Join(root, "projects")
	dc.PluginsDir = filepath.Join(root, "plugins")
	dc.Tailscale.Enabled = false
	dc.Claude.Executable = "/bin/sh"
	dc.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: certificate, KeyFile: key}
	if err := config.SaveDaemon(filepath.Join(root, "daemon.yaml"), dc); err != nil {
		t.Fatal(err)
	}
	ac := config.DefaultClient()
	ac.Tailscale.Enabled = false
	ac.Tailscale.StateDir = filepath.Join(root, "agent", "node")
	ac.Agent.StateDir = filepath.Join(root, "agent", "state")
	ac.Agent.Socket = filepath.Join(root, "agent.sock")
	ac.PluginsDir = filepath.Join(root, "plugins")
	clientConfig := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(clientConfig, ac); err != nil {
		t.Fatal(err)
	}
	daemon := startChild(t, ctx, root, "daemon")
	agent := startChild(t, ctx, root, "agent")
	var info ready
	data, err := os.ReadFile(filepath.Join(root, "daemon.ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	var client *frontend.Client
	wait, stop := context.WithTimeout(ctx, 10*time.Second)
	err = await(wait, func() error {
		var err error
		client, err = frontend.Open(wait, clientConfig, frontend.Options{Timeout: time.Second})
		return err
	})
	stop()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := await(ctx, func() error {
		status, err := client.Agent().GetStatus(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return errors.New("agent resources not ready")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record, err := client.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{RequestId: "setup-server", Config: &apb.ServerConfig{Name: "memory", DirectTlsAddress: address, TlsServerName: "example.com", TlsCaFile: certificate}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{RequestId: "setup-credential", ServerId: record.Config.Id, Token: []byte(info.Token)}); err != nil {
		t.Fatal(err)
	}
	remote := client.Remote(record.Config.Id)
	project, err := remote.CreateProject(ctx, &pb.CreateProjectRequest{Name: "fixed", Path: filepath.Join(dc.ProjectsDir, "fixed"), RequestId: "setup-project"})
	if err != nil {
		t.Fatal(err)
	}
	cycle := func() {
		t.Helper()
		call, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		got, err := remote.GetProject(call, &pb.GetProjectRequest{Id: project.Id})
		if err != nil || got.GetId() != project.Id {
			t.Fatal("fixed remote project", err)
		}
		mappings, err := client.Agent().ListMappings(call, &apb.ListMappingsRequest{})
		if err != nil || len(mappings.GetMappings()) != 0 {
			t.Fatal("fixed local state", err)
		}
	}
	for range 32 {
		cycle()
	}
	sampleBoth := func(label string) { agent.sample(t, ctx, root, label); daemon.sample(t, ctx, root, label) }
	sampleBoth("baseline")
	idle := func(duration time.Duration) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(duration):
		}
	}
	idle(8 * time.Second)
	sampleBoth("idle-before")
	started := time.Now()
	for i := 0; i < 400; i++ {
		cycle()
		remaining := time.Until(started.Add(time.Duration(i+1) * 20 * time.Millisecond))
		if remaining > 0 {
			idle(remaining)
		}
	}
	loadElapsed := time.Since(started)
	sampleBoth("load-after")
	// Same elapsed time as the measured fixed-count work, not a shorter idle.
	idle(loadElapsed)
	sampleBoth("idle-after")
	client.Close()
	agent.close(t)
	daemon.close(t)
	files, err := filepath.Glob(filepath.Join(root, "race.*"))
	if err != nil || len(files) > 0 {
		t.Fatal("child race reports", files, err)
	}
	build, _ := debug.ReadBuildInfo()
	if err := saveJSON(filepath.Join(root, "experiment.json"), map[string]any{"workload": "fixed project read via Agent IPC and authenticated daemon TLS; local ListMappings", "warmup_cycles": 32, "measured_cycles": 400, "remote_business_calls": 400, "local_business_calls": 400, "load_elapsed_ns": loadElapsed.Nanoseconds(), "idle_after_ns": loadElapsed.Nanoseconds(), "build": build, "children_exited": true, "plugin_workers_enabled": false, "forced_gc_per_boundary": 2}); err != nil {
		t.Fatal(err)
	}
	t.Logf("profile artifacts: %s; fixed cycles=400 elapsed=%s; private child hosts exited", root, loadElapsed)
}

func certificateFiles(t *testing.T, root string) (string, string) {
	t.Helper()
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	cert := ts.TLS.Certificates[0]
	ts.Close()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	for path, data := range map[string][]byte{certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return certPath, keyPath
}
