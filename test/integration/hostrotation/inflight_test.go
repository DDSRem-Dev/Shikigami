package hostrotation_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
)

func TestRealHostRotationRecoversInflightFiles(t *testing.T) {
	root, binaries := rotationArtifacts(t)
	for _, side := range []string{"client", "server"} {
		for _, direction := range []string{"upload", "download"} {
			t.Run(side+"/"+direction, func(t *testing.T) {
				var gate *fileTransferGate
				h := startHostsVia(t, filepath.Join(root, side+"-"+direction), side, binaries, func(_ context.Context, target, cert, key string) string {
					gate = newFileTransferGate(t, target, cert, key, direction)
					return strings.TrimPrefix(gate.server.URL, "https://")
				})
				exerciseInflightRotation(h, gate, direction)
			})
		}
	}
}

func exerciseInflightRotation(h *harness, gate *fileTransferGate, direction string) {
	t := h.t
	stable := "server"
	if h.side == "server" {
		stable = "client"
	}
	var enabledAt time.Time
	for _, side := range []string{stable, h.side} {
		if side == h.side {
			enabledAt = time.Now()
		}
		for _, id := range []string{"sync", "port"} {
			h.manage(side, id, pb.ManagePluginRequest_ENABLE, nil)
		}
		h.ready(side)
	}
	old, oldStable := h.workers(h.side), h.workers(stable)
	mapping, remoteRoot := h.mapping("inflight", true)
	h.mappingState(mapping, "active")
	oldContent := bytes.Repeat([]byte("common-baseline\n"), 4096)
	before := h.baseline(mapping, oldContent)
	initialRoute := h.inflightRoute()
	stats := h.observeInflightStats()
	payload := make([]byte, 8<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	source, destination := mapping.LocalRoot, remoteRoot
	partialRoot, partialPrefix := remoteRoot, ".shiki-sync-transfer-"
	staging := filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "staging", mapping.Id)
	if direction == "download" {
		source, destination = remoteRoot, mapping.LocalRoot
		partialRoot, partialPrefix = staging, "transfer-"
	}
	observer := observePublicFile(t, h.ctx, filepath.Join(destination, "flow.txt"), oldContent, payload)
	gate.armed.Store(true)
	h.write(source, "flow.txt", payload)
	var partial partialFile
	h.await("real partial "+direction, func(context.Context) error {
		var err error
		partial, err = findPartial(partialRoot, partialPrefix, payload)
		return err
	})
	if time.Since(enabledAt) >= credentialLifetime-3*time.Second {
		t.Fatal("partial transfer was not observed safely before credential expiry")
	}
	select {
	case <-gate.oldDone:
		t.Fatal("file stream ended before natural expiry")
	default:
	}
	observer.check(t)
	t.Logf("%s/%s armed: partial_bytes=%d payload_bytes=%d old_public_sha256=%x partial_prefix_and_bytes_match=true", h.side, direction, partial.size, len(payload), sha256.Sum256(oldContent))
	h.waitRotation(old, enabledAt)
	stats.check(t, h, gate)
	h.inflightReady(h.side, stats, gate)
	h.inflightReady(stable, stats, gate)
	replacement, stillStable := h.workers(h.side), h.workers(stable)
	for _, id := range []string{"sync", "port"} {
		if replacement.pids[id] == old.pids[id] || stillStable.pids[id] != oldStable.pids[id] || replacement.revisions[id] != old.revisions[id] || stillStable.revisions[id] != oldStable.revisions[id] {
			t.Fatal("natural expiry changed the wrong worker or desired intent", id)
		}
		resources.Reaped(t, old.pids[id])
	}
	if !slices.Equal(stillStable.sockets, oldStable.sockets) {
		t.Fatal("unrotated host changed its private CoreAPI sockets")
	}
	for _, path := range replacement.sockets {
		if slices.Contains(old.sockets, path) {
			t.Fatal("replacement reused an expired CoreAPI endpoint")
		}
	}
	observer.check(t)
	// This only removes the fixture's artificial backpressure after the old
	// worker has exited. New file RPCs remain gated. The real old stream must
	// now terminate short of the payload and remove its original private file.
	gate.releaseOld()
	h.await("old file stream terminal result", func(context.Context) error {
		select {
		case <-gate.oldDone:
			return nil
		default:
			return errors.New("old file RPC still running")
		}
	})
	if ended := time.Unix(0, gate.ended.Load()); ended.Before(enabledAt.Add(credentialLifetime)) {
		t.Fatal("old file RPC ended before the configured natural expiry", ended.Sub(enabledAt))
	}
	if gate.bytes.Load() >= int64(len(payload)) {
		t.Fatal("old file RPC carried the complete payload instead of being interrupted", gate.bytes.Load())
	}
	h.await("canceled partial removed before replacement transfer", func(context.Context) error {
		if _, err := os.Lstat(partial.path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("original partial still present: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(destination, "flow.txt"))
		if err != nil || !bytes.Equal(data, oldContent) {
			return errors.New("cancellation did not preserve the complete old destination")
		}
		return nil
	})
	observer.check(t)
	if canceledBaseline := h.baseline(mapping, oldContent); !proto.Equal(canceledBaseline, before) {
		t.Fatal("interrupted transfer changed its persisted common baseline")
	}
	observer.allowNew.Store(true)
	gate.releaseNew()
	h.expectFile(mapping, destination, "flow.txt", payload)
	after := h.baseline(mapping, payload)
	if before.Revision == after.Revision {
		t.Fatal("recovery retained the stale remote baseline revision")
	}
	h.await("all private transfer files removed", func(context.Context) error {
		return errors.Join(noTransferFiles(remoteRoot, ".shiki-sync-transfer-"), noTransferFiles(staging, "transfer-"))
	})
	observer.cancel()
	<-observer.done
	observer.check(t)
	// A subsequent opposite-side edit must use the recovered baseline, rather
	// than appear as a conflict against the interrupted pre-rotation contents.
	followup := []byte("opposite-side-edit-after-confirmed-common-baseline")
	h.write(destination, "flow.txt", followup)
	h.expectFile(mapping, source, "flow.txt", followup)
	h.baseline(mapping, followup)
	freshAfter := time.Now().Unix()
	h.await("same stats stream fresh after transfer recovery", func(context.Context) error {
		stats.check(t, h, gate)
		if stats.timestamp.Load() <= freshAfter {
			return errors.New("awaiting fresh stats frame")
		}
		return nil
	})
	afterRoute := h.inflightRoute()
	if initialRoute.Config.DaemonId != afterRoute.Config.DaemonId || initialRoute.CredentialRevision != afterRoute.CredentialRevision || afterRoute.ConnectionState != "ready" || afterRoute.ConnectionPath != "direct-tls" {
		t.Fatal("file recovery changed daemon identity, credential or route state")
	}
	final, finalStable := h.workers(h.side), h.workers(stable)
	for _, id := range []string{"sync", "port"} {
		if final.pids[id] != replacement.pids[id] || finalStable.pids[id] != oldStable.pids[id] || final.revisions[id] != old.revisions[id] || finalStable.revisions[id] != oldStable.revisions[id] {
			t.Fatal("file recovery caused another worker replacement or intent change", id)
		}
	}
	if !slices.Equal(final.sockets, replacement.sockets) || !slices.Equal(finalStable.sockets, oldStable.sockets) {
		t.Fatal("file recovery changed a confirmed worker endpoint")
	}
	if observer.checks.Load() == 0 {
		t.Fatal("public destination observer collected no samples")
	}
	if elapsed := time.Since(enabledAt); elapsed >= 2*credentialLifetime {
		t.Fatal("file scenario exceeded a single natural-expiry window", elapsed)
	}
	t.Logf("%s/%s recovered: old_stream_bytes=%d old_request_context_canceled=%v old_partial_removed_before_new_request=true canceled_baseline_unchanged=true payload_sha256=%x public_atomicity_samples=%d baseline_revision_changed=true opposite_direction_reconciled=true unrotated_peer_unchanged=true original_stats_stream_survived=true", h.side, direction, gate.bytes.Load(), gate.canceledAtEnd.Load(), sha256.Sum256(payload), observer.checks.Load())
}
