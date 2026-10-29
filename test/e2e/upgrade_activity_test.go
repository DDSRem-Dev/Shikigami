//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type upgradeActivityConfig struct {
	TokenFile, TLSCert                              string
	CLI, ClientConfig, Server, ProjectID, SessionID string
	RemoteRoot, LocalRoot, Ledger, Control          string
	TLSAddress                                      string
}

type upgradeActivity struct {
	t        *testing.T
	ctx      context.Context
	cfg      upgradeActivityConfig
	proxy    *upgradeProxy
	echo     *upgradeEcho
	mapping  *ppb.Mapping
	forward  *ppb.ForwardState
	pane     int32
	boundary func(string)
}

type upgradeTransferDirection string
type upgradeReplacedHost string

const (
	upgradeUpload   upgradeTransferDirection = "upload"
	upgradeDownload upgradeTransferDirection = "download"
	upgradeAgent    upgradeReplacedHost      = "agent"
	upgradeDaemon   upgradeReplacedHost      = "daemon"
)

// Direction selects the automatic-sync source and observable staging location.
// Host ownership independently determines whether the direct daemon RPC survives.
func (a *upgradeActivity) exercise(label string, direction upgradeTransferDirection, replaced upgradeReplacedHost) {
	if direction != upgradeUpload && direction != upgradeDownload {
		a.t.Fatal("invalid automatic transfer direction", direction)
	}
	if replaced != upgradeAgent && replaced != upgradeDaemon {
		a.t.Fatal("invalid replaced host", replaced)
	}
	c := a.client()
	a.settled(c)
	// A controller's confirmed input is recorded by the actual tmux fixture,
	// rather than inferred from a terminal redraw that may repeat screen text.
	cliProcess, pty := a.terminal()
	confirmed, uncertain, after := label+":confirmed", label+":uncertain", label+":after"
	if _, err := io.WriteString(pty, confirmed+"\n"); err != nil {
		a.t.Fatal(err)
	}
	a.wait(10*time.Second, func(context.Context) error {
		if a.ledgerCount(confirmed) != 1 {
			return errors.New("terminal input not yet received exactly once")
		}
		return nil
	})
	forward := a.currentForward(c)
	hold := "hold:" + label
	oldTCP := a.exchange(forward, hold)
	a.wait(10*time.Second, func(context.Context) error {
		if a.echo.count(hold) != 1 {
			return errors.New("forwarded TCP request has not reached its real target")
		}
		return nil
	})
	// File RPCs are absent from the frontend proxy whitelist. Automatic sync
	// uses the worker route; this extra authenticated daemon upload has its own
	// lifetime and must survive an unrelated agent replacement.
	manualPath := "manual/" + label + ".bin"
	a.write(filepath.Join(a.cfg.RemoteRoot, manualPath), []byte("retained target"))
	fileClient := a.directFileClient()
	oldInfo, err := fileClient.StatFile(a.ctx, &pb.StatFileRequest{ProjectId: a.cfg.ProjectID, Path: manualPath})
	if err != nil {
		a.t.Fatal(err)
	}
	partial := bytes.Repeat([]byte("p"), 1<<20)
	partialHash := sha256.Sum256(partial)
	writeCtx, cancelWrite := context.WithCancel(a.ctx)
	defer cancelWrite()
	upload, err := fileClient.WriteFile(writeCtx)
	if err != nil {
		a.t.Fatal(err)
	}
	if err := upload.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: a.cfg.ProjectID, Path: manualPath, BaseRevision: oldInfo.Revision, ContentHash: hex.EncodeToString(partialHash[:]), Size: int64(len(partial)), Mode: 0600}}}); err != nil {
		a.t.Fatal(err)
	}
	if err := upload.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: partial[:256<<10]}}); err != nil {
		a.t.Fatal(err)
	}
	manualRoot := filepath.Join(a.cfg.RemoteRoot, "manual")
	a.wait(10*time.Second, func(context.Context) error {
		if a.partialBytes(manualRoot, partial, ".shiki-sync-transfer-") == 0 {
			return errors.New("manual upload has not reached a private partial file")
		}
		return nil
	})
	manualBytes := a.partialBytes(manualRoot, partial, ".shiki-sync-transfer-")
	payload := make([]byte, 8<<20)
	if _, err := rand.Read(payload); err != nil {
		a.t.Fatal(err)
	}
	source := a.cfg.LocalRoot
	observedRoot, observedPrefix, observedStage := a.cfg.RemoteRoot, ".shiki-sync-transfer-", "destination_temp"
	if direction == upgradeDownload {
		source = a.cfg.RemoteRoot
		observedRoot = filepath.Join(c.Config.Agent.StateDir, "plugin-state", "sync", "staging", a.mapping.Id)
		observedPrefix, observedStage = "transfer-", "client_download_staging"
	}
	destination := a.cfg.RemoteRoot
	if direction == upgradeDownload {
		destination = a.cfg.LocalRoot
	}
	previousDestination, err := os.ReadFile(filepath.Join(destination, "a-upgrade.bin"))
	if err != nil {
		a.t.Fatal(err)
	}
	previousHash := sha256.Sum256(previousDestination)
	a.proxy.slow.Store(true)
	defer a.proxy.slow.Store(false)
	defer a.proxy.pause(false)
	a.write(filepath.Join(source, "a-upgrade.bin"), payload)
	var syncBytes int64
	a.wait(30*time.Second, func(context.Context) error {
		syncBytes = a.partialBytes(observedRoot, payload, observedPrefix)
		if syncBytes == 0 {
			return errors.New("sync has not reached a private partial file")
		}
		return nil
	})
	a.proxy.pause(true)
	publicDestination, err := os.ReadFile(filepath.Join(destination, "a-upgrade.bin"))
	if err != nil || sha256.Sum256(publicDestination) != previousHash {
		a.t.Fatal("incomplete transfer replaced its public destination before commit", err)
	}
	// Both edits occur after the in-flight sync transfer was observed and its
	// transport paused. Recovery must compare against the saved common baseline.
	localConflict, remoteConflict := []byte(label+":local-edit"), []byte(label+":remote-edit")
	a.write(filepath.Join(a.cfg.LocalRoot, "z-conflict.txt"), localConflict)
	a.write(filepath.Join(a.cfg.RemoteRoot, "z-conflict.txt"), remoteConflict)
	if _, err := io.WriteString(pty, uncertain+"\n"); err != nil {
		a.t.Fatal(err)
	}
	if a.ledgerCount(uncertain) != 0 {
		a.t.Fatal("uncertain input reached the fixture before the transport gate")
	}
	if a.echo.closedCount(hold) != 0 {
		a.t.Fatal("TCP target closed before the upgrade boundary")
	}
	a.record(label+".ready", map[string]any{"manual_temp_bytes": manualBytes, "manual_declared_bytes": len(partial), "manual_temp_prefix_matches_payload": true, "sync_temp_bytes": syncBytes, "sync_declared_bytes": len(payload), "sync_temp_prefix_matches_payload": true, "public_destination_original_sha256": fmt.Sprintf("%x", previousHash), "public_destination_old_complete_content_preserved": true, "sync_inflight_stage": observedStage, "sync_direction": direction, "replaced_host": replaced, "tcp_request_received": a.echo.count(hold), "tcp_target_still_open": true, "terminal_confirmed_count": a.ledgerCount(confirmed), "terminal_uncertain_count": 0, "pane_pid": a.pane})
	if a.boundary != nil {
		a.boundary(label)
	}
	a.waitFile(label + ".after")
	a.proxy.slow.Store(false)
	a.proxy.pause(false)
	cliProcess.wait(a.t, a.ctx, true)
	pty.Close()
	oldTCP.SetReadDeadline(time.Now().Add(15 * time.Second))
	one := make([]byte, 1)
	if n, err := oldTCP.Read(one); n != 0 || err == nil {
		a.t.Fatalf("old TCP connection survived upgrade: bytes=%d error=%v", n, err)
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		a.t.Fatal("old TCP connection did not close within recovery deadline", err)
	}
	oldTCP.Close()
	a.wait(10*time.Second, func(context.Context) error {
		if a.echo.closedCount(hold) != 1 {
			return errors.New("old backend TCP session remains after host replacement")
		}
		return nil
	})
	manualWant := []byte("retained target")
	if replaced == upgradeDaemon {
		// Observe cancellation cleanup before closing our request side; otherwise
		// a short upload could cause its own cleanup and conceal a surviving RPC.
		a.wait(15*time.Second, func(context.Context) error { return upgradeNoTemps(manualRoot) })
	} else {
		if a.partialBytes(manualRoot, partial, ".shiki-sync-transfer-") != manualBytes {
			a.t.Fatal("agent replacement canceled an independently owned daemon upload")
		}
		actual, err := os.ReadFile(filepath.Join(a.cfg.RemoteRoot, manualPath))
		if err != nil || string(actual) != "retained target" {
			a.t.Fatal("uncommitted direct upload changed its target", err)
		}
		manualWant = partial
	}
	var writeTimedOut atomic.Bool
	writeDeadline := time.AfterFunc(15*time.Second, func() { writeTimedOut.Store(true); cancelWrite() })
	defer writeDeadline.Stop()
	if replaced == upgradeAgent {
		for offset := 256 << 10; offset < len(partial); offset += 256 << 10 {
			if err := upload.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: partial[offset : offset+(256<<10)]}}); err != nil {
				a.t.Fatal("independent daemon upload did not survive agent upgrade", err)
			}
		}
	}
	if result, err := upload.CloseAndReceive(); replaced == upgradeDaemon && err == nil || replaced == upgradeAgent && (err != nil || result.GetSize() != int64(len(partial))) {
		a.t.Fatal("direct upload result does not match replaced process ownership", err)
	}
	writeDeadline.Stop()
	if writeTimedOut.Load() {
		a.t.Fatal("old upload response did not terminate after the service replacement")
	}
	c.Close()
	recovered := a.client()
	a.wait(45*time.Second, func(ctx context.Context) error {
		session, err := recovered.Remote(a.mapping.ServerId).GetSession(ctx, &pb.GetSessionRequest{Id: a.cfg.SessionID})
		if err != nil {
			return err
		}
		if session.ClaudePid != a.pane || session.ClaudeStatus != "running" {
			return fmt.Errorf("upgrade replaced terminal process: %v", session)
		}
		return nil
	})
	a.verifyFiles(recovered, payload, localConflict, remoteConflict, manualPath, manualWant)
	newCLI, newPTY := a.terminal()
	if _, err := io.WriteString(newPTY, after+"\n"); err != nil {
		a.t.Fatal(err)
	}
	a.wait(10*time.Second, func(context.Context) error {
		if a.ledgerCount(after) != 1 {
			return errors.New("new attachment has not reached the original fixture")
		}
		return nil
	})
	if _, err := newPTY.Write([]byte{17}); err != nil {
		a.t.Fatal(err)
	}
	newCLI.wait(a.t, a.ctx, false)
	newPTY.Close()
	newForward := a.currentForward(recovered)
	if newForward.Spec.Id != forward.Spec.Id || newForward.Spec.Revision != forward.Spec.Revision {
		a.t.Fatal("upgrade changed desired listener identity or revision")
	}
	newTCP := a.exchange(newForward, "new:"+label)
	newTCP.SetReadDeadline(time.Now().Add(10 * time.Second))
	response, err := io.ReadAll(newTCP)
	newTCP.Close()
	if err != nil || string(response) != "echo:new:"+label+"\n" {
		a.t.Fatal("fresh TCP connection failed after re-negotiation", string(response), err)
	}
	if a.ledgerCount(confirmed) != 1 || a.ledgerCount(after) != 1 || a.ledgerCount(uncertain) > 1 || a.echo.count(hold) != 1 {
		a.t.Fatal("terminal input or old TCP request was replayed")
	}
	a.record(label+".verified", map[string]any{"pane_pid": a.pane, "old_terminal_exited": true, "old_tcp_closed": true, "direct_upload_canceled_without_commit": replaced == upgradeDaemon, "independent_direct_upload_survived_agent_replacement": replaced == upgradeAgent, "sync_direction": direction, "replaced_host": replaced, "payload_sha256": fmt.Sprintf("%x", sha256.Sum256(payload)), "conflicting_edits_preserved_then_explicitly_resolved": true, "confirmed_input_count": 1, "uncertain_input_count": a.ledgerCount(uncertain), "new_input_count": 1, "old_tcp_request_count": 1, "new_tcp_connection_verified": true, "desired_forward_identity_preserved": true})
}
