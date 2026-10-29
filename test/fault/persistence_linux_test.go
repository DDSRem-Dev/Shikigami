//go:build linux

package fault_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	state "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"golang.org/x/sys/unix"
)

type faultFixture struct {
	Scenario     string `json:"scenario"`
	BaseRevision string `json:"base_revision,omitempty"`
	Token        string `json:"token,omitempty"`
	TokenID      string `json:"token_id,omitempty"`
}

func configuredFault(t *testing.T) (string, string) {
	t.Helper()
	scenario, root := os.Getenv("SHIKI_FAULT_SCENARIO"), os.Getenv("SHIKI_FAULT_ROOT")
	if scenario == "" && root == "" {
		t.Skip("opt-in syscall fault helper; run scripts/test-faults.sh")
	}
	if os.Geteuid() == 0 || !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
		t.Fatal("fault helper requires an explicit private root and ordinary user")
	}
	switch scenario {
	case "state-file-sync", "state-directory-sync", "auth-directory-sync", "sync-directory-sync", "sync-history-sync":
	default:
		t.Fatal("unknown fault scenario")
	}
	return scenario, root
}

// Fixture creation is a separate, untraced process. This makes the subsequent
// syscall ordinal auditable without installing test hooks in production code.
func TestPreparePersistenceFault(t *testing.T) {
	scenario, root := configuredFault(t)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := faultFixture{Scenario: scenario}
	switch {
	case strings.HasPrefix(scenario, "state-"):
		store, err := state.Open(filepath.Join(root, "state"), state.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.Update(func(s *state.Snapshot) error { return s.Put("records", "item", "original") }); err != nil {
			t.Fatal(err)
		}
	case strings.HasPrefix(scenario, "auth-"):
		manager, err := auth.Open(filepath.Join(root, "auth"), "fault-user")
		if err != nil {
			t.Fatal(err)
		}
		defer manager.Close()
		token, info, err := manager.Create("fault-user", []string{"session.attach"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		fixture.Token, fixture.TokenID = token, info.ID
	case strings.HasPrefix(scenario, "sync-"):
		project := filepath.Join(root, "project")
		if err := os.Mkdir(project, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, "file"), []byte("original file content"), 0600); err != nil {
			t.Fatal(err)
		}
		tree, err := filetree.Open(project, filetree.Options{ProjectID: "project", StateDir: filepath.Join(root, "sync-state")})
		if err != nil {
			t.Fatal(err)
		}
		prior, err := tree.StatFile(t.Context(), &pb.StatFileRequest{ProjectId: "project", Path: "file"})
		if err != nil {
			t.Fatal(err)
		}
		fixture.BaseRevision = prior.Revision
		if err := tree.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	// Credentials stay only in this container-private fixture, never traces,
	// test output, host evidence directories or environment variables.
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInjectedPersistenceFault(t *testing.T) {
	scenario, root := configuredFault(t)
	var fixture faultFixture
	if err := json.Unmarshal(readBytes(t, filepath.Join(root, "fixture.json")), &fixture); err != nil || fixture.Scenario != scenario {
		t.Fatal("prepared fault fixture does not match scenario")
	}
	// strace's injection counter is per traced thread. All application fsyncs
	// in this scenario must stay on this thread, including after a syscall.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch {
	case strings.HasPrefix(scenario, "state-"):
		checkStateSyncFault(t, root)
	case strings.HasPrefix(scenario, "auth-"):
		checkAuthSyncFault(t, root, fixture)
	case strings.HasPrefix(scenario, "sync-"):
		checkSyncPreservationFault(t, root)
	}
}

// This is a new process, not merely a second Open on the failed writer.
// Recovery is never traced with fault injection, so it cannot consume a later
// injected errno or accidentally reuse an in-memory transaction result.
func TestRecoverInjectedPersistenceFault(t *testing.T) {
	scenario, root := configuredFault(t)
	var fixture faultFixture
	if err := json.Unmarshal(readBytes(t, filepath.Join(root, "fixture.json")), &fixture); err != nil || fixture.Scenario != scenario {
		t.Fatal("prepared recovery fixture does not match scenario")
	}
	switch {
	case strings.HasPrefix(scenario, "state-"):
		checkStateRecovery(t, root, scenario == "state-directory-sync")
	case strings.HasPrefix(scenario, "auth-"):
		checkAuthRecovery(t, root, fixture)
	case strings.HasPrefix(scenario, "sync-"):
		checkSyncRecovery(t, root, fixture)
	}
}

func checkStateSyncFault(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "state")
	original := readBytes(t, filepath.Join(dir, "state.json"))
	store, err := state.Open(dir, state.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.Update(func(s *state.Snapshot) error {
		if _, _, err := s.Request("mutation", "request", "new"); err != nil {
			return err
		}
		if err := s.Put("records", "item", "new"); err != nil {
			return err
		}
		return s.CompleteRequest("mutation", "request", "item", "new", "")
	})
	if !errors.Is(err, syscall.EIO) {
		t.Fatal("expected injected primary fsync error, got", err)
	}
	if _, err := store.View(); !errors.Is(err, state.ErrClosed) {
		t.Fatal("uncertain primary writer served stale memory", err)
	}
	called := false
	if err := store.Update(func(*state.Snapshot) error { called = true; return nil }); !errors.Is(err, state.ErrClosed) || called {
		t.Fatal("failed primary writer admitted another mutation", err)
	}
	if !bytes.Equal(original, readBytes(t, filepath.Join(dir, "state.previous.json"))) {
		t.Fatal("previous committed snapshot was not preserved")
	}
	noStateTemps(t, dir)
}

func checkStateRecovery(t *testing.T, root string, exchanged bool) {
	t.Helper()
	dir := filepath.Join(root, "state")
	original := readBytes(t, filepath.Join(dir, "state.previous.json"))
	reopened, err := state.Open(dir, state.Options{})
	if err != nil {
		t.Fatal("failed writer did not release its lock", err)
	}
	defer reopened.Close()
	snapshot, err := reopened.View()
	if err != nil {
		t.Fatal(err)
	}
	var actual string
	if err := snapshot.Get("records", "item", &actual); err != nil {
		t.Fatal(err)
	}
	want := "original"
	if exchanged {
		want = "new"
	}
	if actual != want {
		t.Fatalf("re-observed commit=%q, want %q", actual, want)
	}
	record, replay, err := snapshot.Request("mutation", "request", "new")
	if err != nil || replay != exchanged || (exchanged && (record.Status != "succeeded" || record.ResourceID != "item")) {
		t.Fatal("request result does not match actual commit outcome")
	}
	if !exchanged && !bytes.Equal(original, readBytes(t, filepath.Join(dir, "state.json"))) {
		t.Fatal("pre-rename fsync failure changed primary bytes")
	}
}

func checkAuthSyncFault(t *testing.T, root string, fixture faultFixture) {
	t.Helper()
	dir := filepath.Join(root, "auth")
	manager, err := auth.Open(dir, "fault-user")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Verify(fixture.Token); err != nil {
		t.Fatal("prepared credential was not valid")
	}
	active, _, release, err := manager.BindContext(t.Context(), fixture.Token)
	if err != nil {
		t.Fatal("could not bind initial credential lifetime")
	}
	defer release()
	if err := manager.Revoke(fixture.TokenID); !errors.Is(err, syscall.EIO) {
		t.Fatal("expected uncertain revocation directory-fsync error", err)
	}
	if _, err := manager.Verify(fixture.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("committed revocation was lost after uncertain response")
	}
	select {
	case <-active.Done():
		if !errors.Is(context.Cause(active), auth.ErrRevoked) {
			t.Fatal("uncertain revocation canceled active call for wrong reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("uncertain committed revocation left an active call authorized")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func checkAuthRecovery(t *testing.T, root string, fixture faultFixture) {
	t.Helper()
	dir := filepath.Join(root, "auth")
	reopened, err := auth.Open(dir, "fault-user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Verify(fixture.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("restart accepted credential after uncertain revocation")
	}
	reopened.Close()
	// The backup contains the old unrevoked token. Prove that fact without
	// copying it into the primary or printing any credential material.
	var previous state.Snapshot
	if err := json.Unmarshal(readBytes(t, filepath.Join(dir, "state.previous.json")), &previous); err != nil {
		t.Fatal(err)
	}
	var old auth.TokenInfo
	if err := previous.Get("tokens", fixture.TokenID, &old); err != nil || old.RevokedAt != 0 {
		t.Fatal("fixture backup does not exercise unsafe revocation rollback")
	}
	damaged := []byte("interrupted-primary")
	if err := os.WriteFile(filepath.Join(dir, "state.json"), damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := auth.Open(dir, "fault-user"); err == nil {
		unexpected.Close()
		t.Fatal("corrupt primary restored unrevoked credential from backup")
	}
	if !bytes.Equal(damaged, readBytes(t, filepath.Join(dir, "state.json"))) {
		t.Fatal("auth recovery changed corruption evidence")
	}
	noStateTemps(t, dir)
}

func checkSyncPreservationFault(t *testing.T, root string) {
	t.Helper()
	project, stateDir := filepath.Join(root, "project"), filepath.Join(root, "sync-state")
	opts := filetree.Options{ProjectID: "project", StateDir: stateDir}
	tree, err := filetree.Open(project, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	ctx := t.Context()
	prior, err := tree.StatFile(ctx, &pb.StatFileRequest{ProjectId: "project", Path: "file"})
	if err != nil {
		t.Fatal(err)
	}
	inode, err := os.Stat(filepath.Join(project, "file"))
	if err != nil {
		t.Fatal(err)
	}
	incoming := []byte("incoming file content")
	if result, err := writeTree(tree, ctx, prior.Revision, incoming); !errors.Is(err, syscall.EIO) || result != nil {
		t.Fatal("post-exchange failure reported success or wrong error", err)
	}
	current, err := tree.StatFile(ctx, &pb.StatFileRequest{ProjectId: "project", Path: "file"})
	if err != nil || current.ContentHash != digest(incoming) || current.Revision == prior.Revision {
		t.Fatal("uncertain write was not re-observable as the new content", err)
	}
	var snapshot state.Snapshot
	if err := json.Unmarshal(readBytes(t, filepath.Join(stateDir, "state.json")), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Buckets["filetree_pending"]) != 1 {
		t.Fatal("post-exchange error lost pending journal")
	}
	var temporary string
	for _, raw := range snapshot.Buckets["filetree_pending"] {
		var pending struct{ ID, Path, Temp string }
		if err := json.Unmarshal(raw, &pending); err != nil || pending.ID != pending.Temp || pending.Path != "file" {
			t.Fatal("invalid retained pending record")
		}
		temporary = pending.Temp
	}
	savedInode, err := os.Stat(filepath.Join(project, temporary))
	if err != nil || !os.SameFile(inode, savedInode) || string(readBytes(t, filepath.Join(project, temporary))) != "original file content" {
		t.Fatal("post-exchange error did not preserve displaced inode", err)
	}
	if _, err := writeTree(tree, ctx, current.Revision, []byte("must not write")); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("pending displaced content did not stop subsequent mutation", err)
	}
	if !bytes.Equal(incoming, readBytes(t, filepath.Join(project, "file"))) {
		t.Fatal("later mutation changed uncertain target")
	}
	if err := tree.Close(); err != nil {
		t.Fatal(err)
	}
}

func checkSyncRecovery(t *testing.T, root string, fixture faultFixture) {
	t.Helper()
	project, stateDir := filepath.Join(root, "project"), filepath.Join(root, "sync-state")
	opts := filetree.Options{ProjectID: "project", StateDir: stateDir}
	ctx := t.Context()
	incoming := []byte("incoming file content")
	recovered, err := filetree.Open(project, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	history, err := recovered.History()
	if err != nil || len(history) != 1 || history[0].Revision != fixture.BaseRevision {
		t.Fatal("recovery failed to register displaced history", err)
	}
	if string(readBytes(t, filepath.Join(stateDir, "history", history[0].ID))) != "original file content" {
		t.Fatal("recovery lost original bytes")
	}
	if !bytes.Equal(incoming, readBytes(t, filepath.Join(project, "file"))) {
		t.Fatal("recovery overwrote current target")
	}
	if _, err := os.Stat(filepath.Join(project, history[0].ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery retained completed transfer temporary", err)
	}
	latest, err := recovered.StatFile(ctx, &pb.StatFileRequest{ProjectId: "project", Path: "file"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeTree(recovered, ctx, latest.Revision, []byte("verified retry after recovery")); err != nil {
		t.Fatal("recovered journal still blocks a new conditional mutation", err)
	}
}

func writeTree(tree *filetree.Tree, ctx context.Context, base string, data []byte) (*pb.FileInfo, error) {
	frames := []*pb.WriteFileRequest{{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "project", Path: "file", BaseRevision: base, ContentHash: digest(data), Size: int64(len(data)), Mode: 0600}}}}
	for len(data) != 0 {
		n := min(len(data), filetree.ChunkSize)
		frames = append(frames, &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: data[:n]}})
		data = data[n:]
	}
	return tree.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if len(frames) == 0 {
			return nil, io.EOF
		}
		frame := frames[0]
		frames = frames[1:]
		return frame, nil
	})
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// This test is enabled only on a dedicated small tmpfs created by the runner.
// It never attempts to fill a caller's ordinary filesystem or host mount.
func TestRealTmpfsExhaustion(t *testing.T) {
	base := os.Getenv("SHIKI_FAULT_TMPFS")
	if base == "" {
		t.Skip("opt-in real ENOSPC; run scripts/test-faults.sh")
	}
	var info unix.Statfs_t
	if os.Geteuid() == 0 || unix.Statfs(base, &info) != nil || info.Type != unix.TMPFS_MAGIC || info.Blocks*uint64(info.Bsize) > 16<<20 {
		t.Fatal("ENOSPC test requires ordinary user and a dedicated tmpfs no larger than 16 MiB")
	}
	for _, scenario := range []string{"state", "sync"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp(base, "exhaustion-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			if scenario == "state" {
				dir := filepath.Join(root, "state")
				store, err := state.Open(dir, state.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				if err := store.Update(func(s *state.Snapshot) error { return s.Put("records", "item", "original") }); err != nil {
					t.Fatal(err)
				}
				original := readBytes(t, filepath.Join(dir, "state.json"))
				filler := fillTmpfs(t, root)
				if err := store.Update(func(s *state.Snapshot) error { return s.Put("records", "item", "new") }); !errors.Is(err, syscall.ENOSPC) {
					t.Fatal("actual full filesystem did not fail state mutation with ENOSPC", err)
				}
				if !bytes.Equal(original, readBytes(t, filepath.Join(dir, "state.json"))) {
					t.Fatal("ENOSPC changed original primary state")
				}
				noStateTemps(t, dir)
				if err := os.Remove(filler); err != nil {
					t.Fatal(err)
				}
				if err := store.Update(func(s *state.Snapshot) error { return s.Put("records", "item", "recovered") }); err != nil {
					t.Fatal("space recovery did not permit explicit retry", err)
				}
			} else {
				project := filepath.Join(root, "project")
				if err := os.Mkdir(project, 0700); err != nil {
					t.Fatal(err)
				}
				original := []byte("existing destination")
				if err := os.WriteFile(filepath.Join(project, "file"), original, 0600); err != nil {
					t.Fatal(err)
				}
				tree, err := filetree.Open(project, filetree.Options{ProjectID: "project", StateDir: filepath.Join(root, "state")})
				if err != nil {
					t.Fatal(err)
				}
				defer tree.Close()
				prior, err := tree.StatFile(t.Context(), &pb.StatFileRequest{ProjectId: "project", Path: "file"})
				if err != nil {
					t.Fatal(err)
				}
				filler := fillTmpfs(t, root)
				if result, err := writeTree(tree, t.Context(), prior.Revision, bytes.Repeat([]byte("x"), 512<<10)); !errors.Is(err, syscall.ENOSPC) || result != nil {
					t.Fatal("actual full filesystem did not fail sync write with ENOSPC", err)
				}
				if !bytes.Equal(original, readBytes(t, filepath.Join(project, "file"))) {
					t.Fatal("ENOSPC replaced destination")
				}
				entries, err := os.ReadDir(project)
				if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
					t.Fatal("ENOSPC leaked upload temporary")
				}
				if err := os.Remove(filler); err != nil {
					t.Fatal(err)
				}
				if _, err := writeTree(tree, t.Context(), prior.Revision, []byte("recovered")); err != nil {
					t.Fatal("space recovery did not permit a fresh conditional transfer", err)
				}
			}
		})
	}
}

func fillTmpfs(t *testing.T, root string) string {
	t.Helper()
	name := filepath.Join(root, "bounded-filler")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	block := bytes.Repeat([]byte("f"), 64<<10)
	for total := 0; total <= 16<<20; {
		n, err := f.Write(block)
		total += n
		if errors.Is(err, syscall.ENOSPC) {
			t.Logf("real tmpfs exhausted after %d filler bytes", total)
			return name
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal(fmt.Errorf("bounded filler exceeded verified tmpfs size without ENOSPC"))
	return ""
}
