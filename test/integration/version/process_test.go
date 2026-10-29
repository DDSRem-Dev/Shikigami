package version_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

const fixtureBuild = "synthetic-version-fixture-a"

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}
type laboratory struct {
	root, agent, cli, daemon string
	cert, key                string
}

func newLaboratory(t *testing.T) *laboratory {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shiki-version-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	command := exec.CommandContext(t.Context(), "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-ldflags=-X github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo.Version="+fixtureBuild, "-o", bin+string(filepath.Separator), "./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon")
	command.Dir = filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build actual commands: %v\n%s", err, out)
	}
	lab := &laboratory{root: root, agent: filepath.Join(bin, "shiki-agent"), cli: filepath.Join(bin, "shiki"), daemon: filepath.Join(bin, "shiki-daemon")}
	lab.cert, lab.key = certificate(t, root)
	t.Cleanup(func() {
		reports, err := filepath.Glob(filepath.Join(root, "race.*"))
		if err != nil || len(reports) != 0 {
			t.Errorf("child race report: %v %v", reports, err)
		}
	})
	return lab
}

func (l *laboratory) directory(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(l.root, name)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func (l *laboratory) start(t *testing.T, executable string, env []string, args ...string) *process {
	t.Helper()
	log, err := os.CreateTemp(l.root, "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("%s: %s", filepath.Base(log.Name()), data)
		}
	})
	cmd := exec.CommandContext(t.Context(), executable, args...)
	cmd.Env = append(os.Environ(), "GORACE=halt_on_error=1 log_path="+filepath.Join(l.root, "race"))
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { p.stop(t) })
	return p
}
func (p *process) stop(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Error("child required forced termination")
	}
}
func waitFor(t *testing.T, check func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		if err = check(); err == nil {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("condition did not become true:", err)
}
func (l *laboratory) clientConfiguration(t *testing.T, name string) (*config.Client, string) {
	t.Helper()
	root := l.directory(t, name)
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(root, "node")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Agent.Socket = filepath.Join(root, "run", "agent.sock")
	cfg.Agent.AutoStart = false
	cfg.PluginsDir = filepath.Join(root, "plugins")
	path := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, path
}
func openFrontend(t *testing.T, path string) *frontend.Client {
	t.Helper()
	var result *frontend.Client
	waitFor(t, func() error {
		var err error
		result, err = frontend.Open(t.Context(), path, frontend.Options{Timeout: time.Second})
		return err
	})
	t.Cleanup(func() { result.Close() })
	return result
}
func certificate(t *testing.T, root string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(17), Subject: pkix.Name{CommonName: "version-fixture"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(root, "ca.pem"), filepath.Join(root, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
func (l *laboratory) fixture(t *testing.T, name string, cfg fixtureConfiguration) (*process, string) {
	t.Helper()
	root := l.directory(t, name)
	cfg.Ready = filepath.Join(root, "ready.json")
	cfg.Marker = filepath.Join(root, "calls")
	cfg.Cert = l.cert
	cfg.Key = l.key
	path := filepath.Join(root, "fixture.json")
	writeJSON(t, path, cfg)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := l.start(t, executable, []string{"SHIKI_VERSION_FIXTURE=" + path}, "-test.run=^TestVersionProcessFixture$")
	waitFor(t, func() error { _, err := os.Stat(cfg.Ready); return err })
	return p, path
}
func readFixture(t *testing.T, path string) fixtureConfiguration {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg fixtureConfiguration
	if err = json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func fixtureAddress(t *testing.T, path string) string {
	t.Helper()
	cfg := readFixture(t, path)
	data, err := os.ReadFile(cfg.Ready)
	if err != nil {
		t.Fatal(err)
	}
	var value struct{ Address string }
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value.Address
}
func fixtureCalls(t *testing.T, path string) string {
	t.Helper()
	cfg := readFixture(t, path)
	data, err := os.ReadFile(cfg.Marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}
func makeCredential(t *testing.T, dir string) (string, string) {
	t.Helper()
	store, err := auth.Open(dir, "version-test")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id, err := store.DaemonID()
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.Create("version-test", []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token, id
}
func addRemote(t *testing.T, client *frontend.Client, l *laboratory, name, address, token, id string) string {
	t.Helper()
	record, err := client.Agent().UpsertServer(t.Context(), &apb.UpsertServerRequest{Config: &apb.ServerConfig{Name: name, DirectTlsAddress: address, TlsServerName: "localhost", TlsCaFile: l.cert, DaemonId: id}, RequestId: "add-" + name})
	if err != nil {
		t.Fatal(err)
	}
	record, err = client.Agent().SetServerCredential(t.Context(), &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte(token), RequestId: "token-" + name})
	if err != nil {
		t.Fatal(err)
	}
	return record.Config.Id
}

// Keep assertion errors free of fixture credentials and raw transport metadata.
func requireCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if connect.CodeOf(err) != want {
		t.Fatalf("code=%v, want=%v (%v)", connect.CodeOf(err), want, err)
	}
}
