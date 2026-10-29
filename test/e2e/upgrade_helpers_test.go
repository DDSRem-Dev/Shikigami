//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func (a *upgradeActivity) cli(args ...string) []byte {
	a.t.Helper()
	return run(a.t, a.ctx, os.Environ(), a.cfg.CLI, append([]string{"--config", a.cfg.ClientConfig, "--json"}, args...)...)
}

func (a *upgradeActivity) client() *frontend.Client {
	a.t.Helper()
	var result *frontend.Client
	a.wait(30*time.Second, func(ctx context.Context) error {
		var err error
		result, err = frontend.Open(ctx, a.cfg.ClientConfig, frontend.Options{AutoStart: false, Timeout: 5 * time.Second})
		return err
	})
	a.t.Cleanup(func() { result.Close() })
	return result
}

func (a *upgradeActivity) wait(timeout time.Duration, probe func(context.Context) error) {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	defer cancel()
	for {
		call, stop := context.WithTimeout(ctx, 2*time.Second)
		err := probe(call)
		stop()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			a.t.Fatalf("upgrade condition timed out: %v (%v)", err, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (a *upgradeActivity) write(path string, content []byte) {
	a.t.Helper()
	if err := os.WriteFile(path, content, 0600); err != nil {
		a.t.Fatal(err)
	}
}

func (a *upgradeActivity) record(name string, value any) {
	a.t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		a.t.Fatal(err)
	}
	path := filepath.Join(a.cfg.Control, name+".json")
	a.write(path+".tmp", data)
	if err := os.Rename(path+".tmp", path); err != nil {
		a.t.Fatal(err)
	}
}

func (a *upgradeActivity) waitFile(name string) {
	a.t.Helper()
	a.wait(120*time.Second, func(context.Context) error {
		_, err := os.Stat(filepath.Join(a.cfg.Control, name))
		return err
	})
}

func (a *upgradeActivity) prepare() {
	a.cli("servers", "update", a.cfg.Server, "--tls-address", a.proxy.listener.Addr().String())
	if err := os.MkdirAll(a.cfg.LocalRoot, 0700); err != nil {
		a.t.Fatal(err)
	}
	// Seed an identical baseline. No initial-sync direction is silently chosen.
	keep, err := os.ReadFile(filepath.Join(a.cfg.RemoteRoot, "keep.txt"))
	if err != nil {
		a.t.Fatal(err)
	}
	for _, root := range []string{a.cfg.LocalRoot, a.cfg.RemoteRoot} {
		a.write(filepath.Join(root, "keep.txt"), keep)
		a.write(filepath.Join(root, ".shikiignore"), []byte("manual/\n"))
		a.write(filepath.Join(root, "a-upgrade.bin"), []byte("initial identical payload"))
		a.write(filepath.Join(root, "z-conflict.txt"), []byte("initial identical conflict baseline"))
	}
	keepInfo, err := os.Stat(filepath.Join(a.cfg.RemoteRoot, "keep.txt"))
	if err != nil {
		a.t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(a.cfg.LocalRoot, "keep.txt"), keepInfo.Mode().Perm()); err != nil {
		a.t.Fatal(err)
	}
	manual := filepath.Join(a.cfg.RemoteRoot, "manual")
	if err := os.MkdirAll(manual, 0700); err != nil {
		a.t.Fatal(err)
	}
	mappings := new(ppb.MappingList)
	decode(a.t, a.cli("sync", "map", a.cfg.ProjectID, "--local", a.cfg.LocalRoot, "--server", a.cfg.Server), mappings)
	if len(mappings.Mappings) != 1 {
		a.t.Fatal("upgrade probe requires exactly one mapping", mappings)
	}
	a.mapping = mappings.Mappings[0]
	a.cli("sync", "resume", a.mapping.Id)
	c := a.client()
	a.settled(c)
	session, err := c.Remote(a.mapping.ServerId).GetSession(a.ctx, &pb.GetSessionRequest{Id: a.cfg.SessionID})
	if err != nil || session.ClaudePid == 0 || session.ClaudeStatus != "running" {
		a.t.Fatal("running terminal fixture required", session, err)
	}
	a.pane = session.ClaudePid
	port := a.echo.listener.Addr().(*net.TCPAddr).Port
	configFile := filepath.Join(a.cfg.Control, "port-config.json")
	a.write(configFile, []byte(fmt.Sprintf(`{"allow_ports":[%d],"idle_timeout_seconds":120}`, port)))
	a.cli("plugins", "config", "port", "--side", "remote", "--server", a.cfg.Server, "--file", configFile)
	a.cli("plugins", "enable", "port", "--side", "remote", "--server", a.cfg.Server)
	a.cli("plugins", "enable", "port", "--side", "local")
	a.forward = new(ppb.ForwardState)
	decode(a.t, a.cli("ports", "forward", strconv.Itoa(port), "--local-port", "0", "--server", a.cfg.Server), a.forward)
	if a.forward.State != "listening" || a.forward.BoundPort == 0 {
		a.t.Fatal("forward not listening", a.forward)
	}
}

func (a *upgradeActivity) settled(c *frontend.Client) {
	a.t.Helper()
	a.wait(45*time.Second, func(ctx context.Context) error {
		state, err := c.Agent().GetSyncState(ctx, deviceScope(a.mapping))
		if err != nil {
			return err
		}
		if state.State != "active" || state.PendingFiles != 0 || state.ConflictCount != 0 {
			if state.ConflictCount != 0 {
				conflicts, listErr := c.Agent().ListConflicts(ctx, deviceScope(a.mapping))
				return fmt.Errorf("sync not settled: %v; conflicts=%v error=%v", state, conflicts, listErr)
			}
			return fmt.Errorf("sync not settled: %v", state)
		}
		return nil
	})
}

func (a *upgradeActivity) ledgerCount(marker string) int {
	a.t.Helper()
	data, err := os.ReadFile(a.cfg.Ledger)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == marker {
			count++
		}
	}
	return count
}

func (a *upgradeActivity) partialBytes(root string, expected []byte, prefix string) int64 {
	a.t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		a.t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			a.t.Fatal(err)
		}
		if info.Mode().IsRegular() && info.Size() > 0 && info.Size() < int64(len(expected)) {
			file, err := os.Open(filepath.Join(root, entry.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				a.t.Fatal(err)
			}
			prefix := make([]byte, min(4096, info.Size()))
			n, readErr := file.Read(prefix)
			file.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				a.t.Fatal(readErr)
			}
			if n > 0 && bytes.Equal(prefix[:n], expected[:n]) {
				return info.Size()
			}
		}
	}
	return 0
}

func (a *upgradeActivity) terminal() (*process, *os.File) {
	a.t.Helper()
	p, terminal := terminal(a.t, a.ctx, os.Environ(), a.cfg.CLI, "--config", a.cfg.ClientConfig, "--server", a.cfg.Server, "attach", a.cfg.SessionID)
	a.t.Cleanup(func() { terminal.Close() })
	read, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	readTerminal(a.t, read, terminal, "SYSTEMD_FIXTURE_READY")
	return p, terminal
}

func (a *upgradeActivity) exchange(forward *ppb.ForwardState, marker string) net.Conn {
	a.t.Helper()
	connection, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(forward.BoundPort))), 5*time.Second)
	if err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(func() { connection.Close() })
	connection.SetDeadline(time.Now().Add(120 * time.Second))
	if _, err := io.WriteString(connection, marker+"\n"); err != nil {
		a.t.Fatal(err)
	}
	return connection
}

func (a *upgradeActivity) currentForward(c *frontend.Client) *ppb.ForwardState {
	a.t.Helper()
	var result *ppb.ForwardState
	observed := "desired listener not present"
	a.wait(30*time.Second, func(ctx context.Context) error {
		list, err := c.Agent().ListForwards(ctx, &pb.Empty{})
		if err != nil {
			return fmt.Errorf("%w; last listener observation: %s", err, observed)
		}
		for _, forward := range list.Forwards {
			if forward.Spec.Id == a.forward.Spec.Id {
				observed = fmt.Sprintf("state=%s reason=%s bound_port=%d connections=%d", forward.State, forward.ReasonCode, forward.BoundPort, forward.Connections)
				if forward.State == "listening" && forward.BoundPort != 0 {
					result = forward
					return nil
				}
			}
		}
		return fmt.Errorf("saved listener has not recovered: %s", observed)
	})
	return result
}

func (a *upgradeActivity) verifyFiles(c *frontend.Client, payload, localConflict, remoteConflict []byte, manualPath string, manualWant []byte) {
	a.t.Helper()
	want := sha256.Sum256(payload)
	a.wait(45*time.Second, func(context.Context) error {
		for _, root := range []string{a.cfg.LocalRoot, a.cfg.RemoteRoot} {
			actual, err := os.ReadFile(filepath.Join(root, "a-upgrade.bin"))
			if err != nil || sha256.Sum256(actual) != want {
				return errors.New("interrupted sync has not reconciled to exact payload")
			}
		}
		return nil
	})
	var conflict *ppb.Conflict
	a.wait(30*time.Second, func(ctx context.Context) error {
		list, err := c.Agent().ListConflicts(ctx, deviceScope(a.mapping))
		if err != nil {
			return err
		}
		for _, candidate := range list.Conflicts {
			if candidate.Path == "z-conflict.txt" {
				conflict = candidate
				return nil
			}
		}
		return errors.New("two-sided edit conflict was not retained")
	})
	for root, want := range map[string][]byte{a.cfg.LocalRoot: localConflict, a.cfg.RemoteRoot: remoteConflict} {
		actual, err := os.ReadFile(filepath.Join(root, "z-conflict.txt"))
		if err != nil || !bytes.Equal(actual, want) {
			a.t.Fatal("recovery overwrote an unresolved conflicting edit", root, err)
		}
	}
	_, err := c.Agent().ResolveConflict(a.ctx, &ppb.ResolveConflictRequest{MappingId: a.mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: rand.Text()})
	if err != nil {
		a.t.Fatal(err)
	}
	a.settled(c)
	for _, root := range []string{a.cfg.LocalRoot, a.cfg.RemoteRoot} {
		actual, err := os.ReadFile(filepath.Join(root, "z-conflict.txt"))
		if err != nil || !bytes.Equal(actual, localConflict) {
			a.t.Fatal("explicit conflict decision did not converge", root, err)
		}
	}
	manual, err := os.ReadFile(filepath.Join(a.cfg.RemoteRoot, manualPath))
	if err != nil || !bytes.Equal(manual, manualWant) {
		a.t.Fatal("direct upload final content violated commit/ownership semantics", err)
	}
	a.wait(10*time.Second, func(context.Context) error {
		for _, root := range []string{a.cfg.LocalRoot, a.cfg.RemoteRoot, filepath.Join(a.cfg.RemoteRoot, "manual")} {
			if err := upgradeNoTemps(root); err != nil {
				return err
			}
		}
		staging := filepath.Join(c.Config.Agent.StateDir, "plugin-state", "sync", "staging", a.mapping.Id)
		if err := upgradeNoTempsWithPrefix(staging, "transfer-"); err != nil {
			return err
		}
		return nil
	})
}

func upgradeNoTemps(root string) error {
	return upgradeNoTempsWithPrefix(root, ".shiki-sync-transfer-")
}

func upgradeNoTempsWithPrefix(root, prefix string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return errors.New("uncommitted transfer temp remains after recovery")
		}
	}
	return nil
}

func (a *upgradeActivity) directFileClient() rpc.ShikiDaemonClient {
	a.t.Helper()
	certificate, err := os.ReadFile(a.cfg.TLSCert)
	if err != nil {
		a.t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		a.t.Fatal("invalid private TLS test CA")
	}
	endpoint, err := connection.NewTLSClient("https://"+a.cfg.TLSAddress, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(func() { endpoint.Close() })
	secret, err := os.ReadFile(a.cfg.TokenFile)
	if err != nil {
		a.t.Fatal("cannot read private fixture credential")
	}
	credential, err := connection.BearerToken(strings.TrimSpace(string(secret)))
	clear(secret)
	if err != nil {
		a.t.Fatal("invalid fixture credential")
	}
	client := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	agreement, err := client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.FileCapability}, RequiredCapabilities: []string{sdk.FileCapability}})
	if err != nil || agreement.GetMajor() != 1 || agreement.GetMinor() != 0 || agreement.GetDaemonId() == "" {
		a.t.Fatal("authenticated direct file negotiation failed", err)
	}
	found := false
	for _, capability := range agreement.Capabilities {
		found = found || capability == sdk.FileCapability
	}
	if !found {
		a.t.Fatal("direct daemon did not negotiate file support")
	}
	return client
}
