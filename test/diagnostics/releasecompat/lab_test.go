//go:build releasecompat

package releasecompat_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"github.com/DDSRem-Dev/Shikigami/test/diagnostics/processidentity"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type lab struct {
	t          *testing.T
	root, repo string
	bins       map[string]string
	events     []any
	sequence   int
	identities []processidentity.Identity
	payloads   map[string]string
	active     map[string]*process
}
type process struct {
	cmd      *exec.Cmd
	done     chan struct{}
	err      error
	log      string
	l        *lab
	identity processidentity.Identity
	recorded bool
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func sum(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func (l *lab) record(kind string, data any) {
	l.events = append(l.events, map[string]any{"kind": kind, "data": data, "at": time.Now().UTC()})
	encoded, err := json.MarshalIndent(l.events, "", "  ")
	must(l.t, err)
	must(l.t, os.WriteFile(filepath.Join(l.root, "events.json"), append(encoded, '\n'), 0600))
}
func (l *lab) directory(name string) string {
	p := filepath.Join(l.root, name)
	must(l.t, os.MkdirAll(p, 0700))
	return p
}
func newLab(t *testing.T) *lab {
	root := os.Getenv("SHIKI_RELEASE_COMPAT_ROOT")
	repo := os.Getenv("SHIKI_RELEASE_COMPAT_REPO")
	if !filepath.IsAbs(root) || !filepath.IsAbs(repo) {
		t.Fatal("explicit private evidence root and repository required")
	}
	l := &lab{t: t, root: root, repo: repo, bins: map[string]string{}, payloads: map[string]string{}, active: map[string]*process{}}
	must(t, os.MkdirAll(root, 0700))
	must(t, os.Chmod(root, 0700))
	t.Cleanup(func() {
		l.verifyCleanup()
		l.record("test_result", map[string]any{"failed": t.Failed(), "product_rebuilt": false, "service_installation": false, "tailscale_enabled": false})
	})
	for _, version := range []string{"v0.0.1-alpha.1", "v0.0.1-alpha.2"} {
		name := "shikigami-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
		archive := filepath.Join(repo, "dist/releases", version, name)
		verified, err := releaseauth.Verify(t.Context(), releaseauth.VerifyOptions{Archive: archive, Signature: archive + ".sig.json", PublicKey: filepath.Join(repo, "configs/release-signing.pub"), Output: filepath.Join(root, "verified-"+version), Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH})
		must(t, err)
		l.record("authenticated_release", verified)
		l.payloads[archive] = verified.ArchiveSHA256
		l.payloads[verified.Archive] = verified.ArchiveSHA256
		out := l.directory("bin-" + version)
		l.extract(verified.Archive, out, verified.ChecksumsSHA256)
		l.bins[version] = out
		for _, binary := range []string{"shiki", "shiki-agent", "shiki-daemon"} {
			l.command(filepath.Join(out, binary), true, "--version")
		}
	}
	return l
}
func (l *lab) extract(archive, out, pin string) {
	f, err := os.Open(archive)
	must(l.t, err)
	defer f.Close()
	g, err := gzip.NewReader(f)
	must(l.t, err)
	defer g.Close()
	r := tar.NewReader(g)
	names := map[string]bool{}
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		must(l.t, err)
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != h.Name || names[h.Name] || h.Size > 128<<20 {
			l.t.Fatal("unsafe or duplicate archive member")
		}
		names[h.Name] = true
		data, err := io.ReadAll(r)
		must(l.t, err)
		must(l.t, os.WriteFile(filepath.Join(out, h.Name), data, os.FileMode(h.Mode)&0755))
	}
	checks, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	must(l.t, err)
	if sum(checks) != pin {
		l.t.Fatal("authenticated checksum pin mismatch")
	}
	verified := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(checks)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			l.t.Fatal("invalid checksum line")
		}
		data, err := os.ReadFile(filepath.Join(out, fields[1]))
		must(l.t, err)
		if sum(data) != fields[0] {
			l.t.Fatal("release member checksum mismatch", fields[1])
		}
		verified[fields[1]] = fields[0]
		l.payloads[filepath.Join(out, fields[1])] = fields[0]
	}
	if len(verified) != len(names)-1 {
		l.t.Fatal("archive/checksum inventory mismatch")
	}
	l.record("verified_members", verified)
}
func (l *lab) command(binary string, success bool, args ...string) []byte {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(l.t.Context(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	l.sequence++
	id := fmt.Sprintf("command-%03d", l.sequence)
	must(l.t, os.WriteFile(filepath.Join(l.root, id+".log"), out, 0600))
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	l.record("command", map[string]any{"id": id, "binary": binary, "argv": args, "success": err == nil, "expected_success": success, "exit_code": exitCode, "context_ended": ctx.Err() != nil})
	if ctx.Err() != nil {
		l.t.Fatalf("command context ended instead of returning a semantic outcome: %s %v", filepath.Base(binary), ctx.Err())
	}
	if (err == nil) != success {
		l.t.Fatalf("%s %v: %v %s", filepath.Base(binary), args, err, out)
	}
	return out
}
func (l *lab) cli(version, cfg string, success bool, args ...string) []byte {
	return l.command(filepath.Join(l.bins[version], "shiki"), success, append([]string{"--config", cfg, "--json"}, args...)...)
}
func (l *lab) start(binary string, args ...string) *process {
	l.t.Helper()
	f, err := os.CreateTemp(l.root, "process-*.log")
	must(l.t, err)
	must(l.t, f.Chmod(0600))
	cmd := exec.Command(binary, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	must(l.t, cmd.Start())
	p := &process{cmd: cmd, done: make(chan struct{}), log: f.Name(), l: l}
	go func() { p.err = cmd.Wait(); f.Close(); close(p.done) }()
	l.t.Cleanup(func() { p.stop() })
	ctx, cancel := context.WithTimeout(l.t.Context(), 2*time.Second)
	p.identity, err = (processidentity.Inspector{}).Inspect(ctx, int32(cmd.Process.Pid))
	cancel()
	must(l.t, err)
	l.identities = append(l.identities, p.identity)
	l.record("process_started", map[string]any{"pid": cmd.Process.Pid, "executable": binary, "argv": args, "log": f.Name()})
	return p
}
func (p *process) stop() {
	if p.recorded {
		return
	}
	select {
	case <-p.done:
		p.recorded = true
		p.l.record("process_reaped", map[string]any{"identity": p.identity, "exit_error": fmt.Sprint(p.err), "already_exited": true})
		if p.err != nil {
			p.l.t.Errorf("owned host exited abnormally: pid=%d err=%v log=%s", p.cmd.Process.Pid, p.err, p.log)
		}
		return
	default:
	}
	must(p.l.t, p.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-p.done:
	case <-time.After(15 * time.Second):
		p.cmd.Process.Kill()
		<-p.done
		p.l.t.Error("owned process required forced termination")
	}
	p.l.record("process_reaped", map[string]any{"pid": p.cmd.Process.Pid, "exit_error": fmt.Sprint(p.err)})
	if p.err != nil {
		p.l.t.Errorf("owned host exited abnormally: pid=%d err=%v log=%s", p.cmd.Process.Pid, p.err, p.log)
	}
	p.recorded = true
}

func (l *lab) observeWorker(side, id string) {
	parent := l.active[side]
	if parent == nil {
		l.t.Fatal("missing private worker parent")
	}
	path := filepath.Join(l.root, map[string]string{"local": "agent", "remote": "daemon"}[side], "plugins", "shiki-plugin-"+id)
	data, err := os.ReadFile(path)
	must(l.t, err)
	ctx, cancel := context.WithTimeout(l.t.Context(), 3*time.Second)
	defer cancel()
	identity, err := (processidentity.Inspector{}).FindChild(ctx, parent.identity, path, sum(data))
	must(l.t, err)
	l.identities = append(l.identities, identity)
	l.record("worker_observed", map[string]any{"side": side, "plugin": id, "identity": identity})
}
func (l *lab) verifyCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	good := true
	for _, identity := range l.identities {
		alive, err := (processidentity.Inspector{}).Alive(ctx, identity)
		if err != nil || alive {
			good = false
			l.t.Errorf("owned identity remains or cannot be verified: pid=%d alive=%v err=%v", identity.PID, alive, err)
		}
	}
	output, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		good = false
		l.t.Error(err)
	}
	realRoot, err := filepath.EvalSymlinks(l.root)
	if err != nil {
		good = false
		l.t.Error(err)
	}
	var remaining []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, l.root) || strings.Contains(line, realRoot) {
			remaining = append(remaining, line)
		}
	}
	if len(remaining) > 0 {
		good = false
		l.t.Error("private-path subprocess remains", remaining)
	}
	for path, want := range l.payloads {
		data, err := os.ReadFile(path)
		if err != nil || sum(data) != want {
			good = false
			l.t.Error("authenticated payload changed", path, err)
		}
	}
	l.record("final_cleanup_and_payload_check", map[string]any{"passed": good, "observed_process_identities": len(l.identities), "private_path_remaining": remaining, "verified_payload_files": len(l.payloads)})
}
func (l *lab) copyPlugins(version, dir string) {
	must(l.t, os.MkdirAll(dir, 0700))
	for _, name := range []string{"sync.manifest.json", "port.manifest.json", "shiki-plugin-sync", "shiki-plugin-port"} {
		data, err := os.ReadFile(filepath.Join(l.bins[version], name))
		must(l.t, err)
		must(l.t, os.WriteFile(filepath.Join(dir, name), data, 0700))
		l.payloads[filepath.Join(dir, name)] = sum(data)
	}
	l.record("plugin_set", map[string]string{"version": version, "directory": dir})
}
func (l *lab) certificate() (string, string) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(l.t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(67), Subject: pkix.Name{CommonName: "release-compat"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &k.PublicKey, k)
	must(l.t, err)
	key, err := x509.MarshalPKCS8PrivateKey(k)
	must(l.t, err)
	certPath, keyPath := filepath.Join(l.root, "ca.pem"), filepath.Join(l.root, "key.pem")
	must(l.t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	must(l.t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
	return certPath, keyPath
}
func (l *lab) hello(cfg *config.Client, expected string) *apb.HandshakeResponse {
	var result *apb.HandshakeResponse
	var last error
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		ep, err := connection.NewUnixClient(cfg.Agent.Socket, uint32(os.Getuid()), "release-compat")
		if err == nil {
			ctx, cancel := context.WithTimeout(l.t.Context(), time.Second)
			api := arpc.NewAgentClient(connection.NewRPCClient(ep))
			result, err = api.Handshake(ctx, &apb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.control.v1", "agent.events.v1", "agent.startup.v1"}, RequiredCapabilities: []string{"agent.control.v1"}}, ClientBuild: "release-compat-observer"})
			if err == nil {
				bound, info := connect.NewClientContext(ctx)
				info.RequestHeader().Set("shiki-agent-instance-id", result.InstanceId)
				info.RequestHeader().Set("shiki-frontend-id", result.FrontendId)
				_, err = api.ReleaseFrontend(bound, &pb.Empty{})
			}
			cancel()
			ep.Close()
		}
		if err == nil {
			if result.Status.GetState() == "starting" {
				time.Sleep(30 * time.Millisecond)
				continue
			}
			if result.Protocol.BuildVersion != expected || result.Protocol.Major != 1 || result.Protocol.Minor != 0 {
				l.t.Fatal("unexpected actual agreement", result.Protocol)
			}
			l.record("observed_agent_handshake", result)
			return result
		}
		last = err
		time.Sleep(30 * time.Millisecond)
	}
	l.t.Fatal("agent handshake", last)
	return nil
}
func decode(t *testing.T, data []byte, msg proto.Message) {
	t.Helper()
	must(t, protojson.Unmarshal(data, msg))
}
