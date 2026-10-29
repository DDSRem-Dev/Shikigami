//go:build shiki_load_capacity

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	process "github.com/shirou/gopsutil/v4/process"
)

func certificate(root string) (string, string) {
	key := take(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der := take(x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key))
	private := take(x509.MarshalECPrivateKey(key))
	certPath, keyPath := filepath.Join(root, "tls.pem"), filepath.Join(root, "tls-key.pem")
	need(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	need(os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}), 0600))
	return certPath, keyPath
}

type processIdentity struct {
	PID        int32
	Created    int64
	Executable string
	UID        int32
	SHA256     string
}
type processMetric struct {
	Identity   processIdentity
	FD         int32
	RSS        uint64
	CPUSeconds float64
}
type resourceSample struct {
	Label     string
	Stage     int
	At        int64
	Processes []processMetric
}

func identity(pid int32) processIdentity {
	p := take(process.NewProcess(pid))
	born := take(p.CreateTime())
	uids := take(p.Uids())
	ensure(len(uids) > 0, "process UID unavailable")
	for _, uid := range uids {
		ensure(uid == uint32(os.Getuid()), "non-ordinary process UID")
	}
	executable := take(p.Exe())
	ensure(take(take(process.NewProcess(pid)).CreateTime()) == born, "process generation changed while observing")
	binaryHashCache.Lock()
	defer binaryHashCache.Unlock()
	sum, ok := binaryHashCache.values[executable]
	if !ok {
		data := take(os.ReadFile(executable))
		sum = fmt.Sprintf("%x", sha256.Sum256(data))
		binaryHashCache.values[executable] = sum
	}
	ensure(take(take(process.NewProcess(pid)).CreateTime()) == born, "process generation changed while hashing executable")
	return processIdentity{PID: pid, Created: born, Executable: executable, UID: int32(uids[0]), SHA256: sum}
}

var binaryHashCache = struct {
	sync.Mutex
	values map[string]string
}{values: map[string]string{}}

func (e *environment) identities() []processIdentity {
	agentPID := int32(take(strconv.Atoi(strings.TrimSpace(string(take(os.ReadFile(filepath.Join(e.root, "agent-state", "instance.lock"))))))))
	current := take(e.client.Remote(e.mapping.ServerId).GetSession(e.ctx, &pb.GetSessionRequest{Id: e.session.Id}))
	ensure(current.ClaudePid > 0 && current.ClaudeStatus == "running", "original terminal not running")
	pids := []int32{int32(os.Getpid()), int32(e.terminal.cmd.Process.Pid), agentPID, int32(e.daemon.Process.Pid), int32(current.ClaudePid)}
	for _, parentPID := range []int32{agentPID, int32(e.daemon.Process.Pid)} {
		parent := take(process.NewProcess(parentPID))
		children := take(parent.Children())
		for _, name := range []string{"shiki-plugin-sync", "shiki-plugin-port"} {
			var found []int32
			for _, child := range children {
				path, err := child.Exe()
				if err == nil && path == filepath.Join(e.bin, name) {
					found = append(found, child.Pid)
				}
			}
			ensure(len(found) == 1, "expected one owned real "+name)
			pids = append(pids, found[0])
		}
	}
	var out []processIdentity
	for _, pid := range pids {
		out = append(out, identity(pid))
	}
	for i, name := range []string{"capacity-driver", "shiki", "shiki-agent", "shiki-daemon"} {
		ensure(out[i].Executable == take(filepath.EvalSymlinks(filepath.Join(e.bin, name))), "host executable does not match built artifact")
	}
	return out
}
func (e *environment) checkIdentities() {
	actual := e.identities()
	ensure(len(actual) == len(e.report.Tracked), "process set changed")
	for i := range actual {
		ensure(actual[i] == e.report.Tracked[i], "process generation changed during sweep")
	}
}
func (e *environment) resources(label string, stage int, sets ...[]processIdentity) resourceSample {
	result := resourceSample{Label: label, Stage: stage, At: time.Now().UnixNano()}
	tracked := e.report.Tracked
	if len(sets) > 0 {
		tracked = sets[0]
	}
	for _, observed := range tracked {
		ensure(identity(observed.PID) == observed, "tracked process changed during resource sample")
		p := take(process.NewProcess(observed.PID))
		memory := take(p.MemoryInfo())
		cpu := take(p.Times())
		result.Processes = append(result.Processes, processMetric{observed, take(p.NumFDs()), memory.RSS, cpu.User + cpu.System})
	}
	return result
}
func runtimeRecord() map[string]any {
	return map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "driver_gomaxprocs": runtime.GOMAXPROCS(0), "uid": os.Getuid(), "logical_cpus": runtime.NumCPU(), "scope": "ordinary real application paths; driver/target/hash/observer costs included; not hardware maximum or memory-leak evidence", "sample_clock": fmt.Sprint(time.Now().Location())}
}
func (e *environment) remember(current []processIdentity) {
	for _, value := range current {
		found := false
		for _, prior := range e.report.AllGenerations {
			if value == prior {
				found = true
			}
		}
		if !found {
			e.report.AllGenerations = append(e.report.AllGenerations, value)
		}
	}
	save(filepath.Join(e.output, "owned-processes.json"), e.report.AllGenerations)
}
func (e *environment) verifyCleanup() {
	deadline := time.Now().Add(10 * time.Second)
	for {
		var alive []int32
		for _, prior := range e.report.AllGenerations {
			if prior.PID == int32(os.Getpid()) {
				continue
			}
			p, err := process.NewProcess(prior.PID)
			if err != nil {
				if errors.Is(err, process.ErrorProcessNotRunning) || errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrNotExist) {
					continue
				}
				e.report.Errors = append(e.report.Errors, "cleanup process existence could not be verified: "+err.Error())
				return
			}
			created, err := p.CreateTime()
			if err != nil {
				if errors.Is(err, process.ErrorProcessNotRunning) || errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrNotExist) {
					continue
				}
				e.report.Errors = append(e.report.Errors, "cleanup process generation could not be verified: "+err.Error())
				return
			}
			if err == nil && created == prior.Created {
				running, err := p.IsRunning()
				if err != nil {
					e.report.Errors = append(e.report.Errors, "cleanup live process check failed: "+err.Error())
					return
				}
				if err == nil && running {
					alive = append(alive, prior.PID)
				}
			}
		}
		if len(alive) == 0 {
			e.cleanupVerified = true
			return
		}
		if time.Now().After(deadline) {
			e.report.Errors = append(e.report.Errors, fmt.Sprintf("tracked processes survived normal cleanup: %v", alive))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func (e *environment) recordRuntime() {
	value := map[string]any{"private_root": e.root, "config": e.configuration, "bin": e.bin, "tmux_socket": fmt.Sprintf("shiki-%d", os.Getuid()), "host_driver_gomaxprocs": 2, "worker_gomaxprocs": "product default; host filters environment"}
	if e.session != nil {
		value["tmux_session"] = "shiki-" + e.session.Id
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data := e.command(ctx, "tmux", "-L", value["tmux_socket"].(string), "display-message", "-p", "-t", value["tmux_session"].(string), "#{session_id}\t#{session_created}\t#{pane_pid}\t#{pane_start_command}")
		parts := strings.SplitN(strings.TrimSpace(string(data)), "\t", 4)
		ensure(len(parts) == 4, "tmux ownership receipt malformed")
		value["tmux_session_id"] = parts[0]
		value["tmux_created"] = take(strconv.ParseInt(parts[1], 10, 64))
		value["tmux_pane_pid"] = take(strconv.Atoi(parts[2]))
		value["tmux_pane_start_command"] = parts[3]
		e.remember([]processIdentity{identity(int32(value["tmux_pane_pid"].(int)))})
	}
	save(filepath.Join(e.output, "runtime.json"), value)
}
func (e *environment) rememberWorkers() {
	for _, pid := range []int32{e.agentPID, int32(e.daemon.Process.Pid)} {
		if pid <= 0 {
			continue
		}
		parent := take(process.NewProcess(pid))
		for _, child := range take(parent.Children()) {
			path, err := child.Exe()
			if err == nil && (path == filepath.Join(e.bin, "shiki-plugin-sync") || path == filepath.Join(e.bin, "shiki-plugin-port")) {
				e.remember([]processIdentity{identity(child.Pid)})
			}
		}
	}
}
func (e *environment) waitPlugin(plugin, side string) {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	until(ctx, func() bool {
		status := new(pb.PluginStatus)
		args := []string{"plugins", "status", plugin, "--side", side}
		if side == "remote" {
			args = append(args, "--server", "capacity")
		}
		decode(e.cli(args...), status)
		ensure(status.State != "failed" && status.State != "incompatible", "plugin failed to become ready")
		return status.State == "ready"
	})
}
func (e *environment) finishCleanup() {
	tmuxAbsent := true
	if e.session != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "has-session", "-t", "shiki-"+e.session.Id)
		cmd.Env = e.env
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		tmuxAbsent = errors.As(err, &exit) && exit.ExitCode() == 1 && (strings.Contains(string(out), "can't find session") || strings.Contains(string(out), "no server running") || strings.Contains(string(out), "No such file or directory"))
		if !tmuxAbsent {
			e.report.Errors = append(e.report.Errors, "owned tmux session absence not confirmed")
		}
	}
	if e.cleanupVerified && tmuxAbsent && e.root != "" && len(e.report.Errors) == 0 {
		need(os.RemoveAll(e.root))
	}
	_, err := os.Lstat(e.root)
	save(filepath.Join(e.output, "cleanup.json"), map[string]any{"processes_absent": e.cleanupVerified, "runtime_absent": errors.Is(err, os.ErrNotExist), "tmux_absent": tmuxAbsent})
}
