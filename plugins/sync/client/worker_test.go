package client

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/pluginhost"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type coreProbe = pluginhost.Core

func coreClient(t *testing.T, ctx context.Context, probe *coreProbe) sdk.CoreAPI {
	return pluginhost.ServeCore(t, ctx, probe)
}
func write(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}
func contents(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync condition timed out")
}
func pass(t *testing.T, w *Worker, id string) {
	t.Helper()
	task, err := w.get(id)
	if err != nil {
		t.Fatal(err)
	}
	task.op.Lock()
	defer task.op.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = task.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
}
func conflicts(t *testing.T, w *Worker, m *ppb.Mapping) map[string]*ppb.Conflict {
	t.Helper()
	result, err := w.ListConflicts(context.Background(), scopeFor(m))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ppb.Conflict{}
	for _, c := range result.Conflicts {
		out[c.Path] = c
	}
	return out
}
func configure(t *testing.T, w *Worker, core *coreProbe, m *ppb.Mapping, enabled bool) {
	t.Helper()
	m.SyncEnabled = enabled
	m.Revision = id("rev_")
	core.SetMapping(m)
	result, err := w.ConfigureMapping(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled && result.State != "paused" {
		t.Fatal("pause returned before task stopped")
	}
}
func startWorker(t *testing.T, ctx context.Context, state string, core sdk.CoreAPI) *Worker {
	t.Helper()
	worker := New()
	cfg, _ := structpb.NewStruct(map[string]any{"scan_interval_ms": 60000.0})
	if err := worker.Init(ctx, sdk.InitContext{Core: core, Side: "client", StateDir: state, InstanceID: "plugin_test", HostInstanceID: "host_test", Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return worker
}
func TestTwoRootsExplicitPlanConflictResolutionPauseAndRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	localRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	for _, root := range []string{localRoot, remoteRoot} {
		write(t, root, "same.txt", "same")
	}
	write(t, localRoot, "shared.txt", "local")
	write(t, remoteRoot, "shared.txt", "remote")
	write(t, localRoot, "only-local.txt", "local-only")
	write(t, remoteRoot, "only-remote.txt", "remote-only")
	write(t, localRoot, ".env", "must-not-sync")
	write(t, localRoot, ".git/secret", "must-not-sync")
	tree, err := filetree.Open(remoteRoot, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private"), PollInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: localRoot}
	probe := &coreProbe{Tree: tree}
	core := coreClient(t, ctx, probe)
	worker := startWorker(t, ctx, state, core)
	stopped := false
	defer func() {
		if !stopped {
			worker.Stop(context.Background())
		}
	}()
	configure(t, worker, probe, mapping, true)
	pass(t, worker, mapping.Id)
	plan := conflicts(t, worker, mapping)
	if len(plan) != 3 || plan["shared.txt"].Base != nil || contents(t, remoteRoot, "shared.txt") != "remote" {
		t.Fatal("initial plan silently changed files")
	}
	if _, err = os.Stat(filepath.Join(remoteRoot, "only-local.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("initial unilateral file copied without choice")
	}
	status, err := worker.GetSyncState(ctx, scopeFor(mapping))
	if err != nil || status.ReasonCode != "INITIAL_SYNC_CHOICE_REQUIRED" {
		t.Fatal("initial choice not reported")
	}
	// Stale decisions are rejected against fresh disk observations.
	old := plan["shared.txt"]
	write(t, localRoot, "shared.txt", "local-new")
	if _, err = worker.ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: old.Id, ExpectedGeneration: old.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "stale"}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale decision accepted: %v", err)
	}
	pass(t, worker, mapping.Id)
	plan = conflicts(t, worker, mapping)
	current := plan["shared.txt"]
	var preview bytes.Buffer
	var info *pb.FileInfo
	if err = worker.ReadConflictFile(ctx, &ppb.ReadConflictFileRequest{MappingId: mapping.Id, ConflictId: current.Id, ExpectedGeneration: current.Generation, Side: ppb.ReadConflictFileRequest_LOCAL}, func(frame *pb.FileData) error {
		if frame.GetInfo() != nil {
			info = frame.GetInfo()
		} else {
			preview.Write(frame.GetChunk())
		}
		return nil
	}); err != nil || info == nil || preview.String() != "local-new" {
		t.Fatalf("conflict preview: %v", err)
	}
	both := &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: current.Id, ExpectedGeneration: current.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "both"}
	if _, err = worker.ResolveConflict(ctx, both); err != nil {
		t.Fatal(err)
	}
	copies, err := filepath.Glob(filepath.Join(localRoot, "shared.txt.conflict-*"))
	if err != nil || len(copies) != 1 {
		t.Fatal("keep-both did not create exactly one copy")
	}
	copyName := filepath.Base(copies[0])
	if contents(t, localRoot, copyName) != "remote" || contents(t, remoteRoot, copyName) != "remote" || contents(t, remoteRoot, "shared.txt") != "local-new" {
		t.Fatal("keep-both lost either version")
	}
	for path, choice := range map[string]ppb.ResolveConflictRequest_Choice{"only-local.txt": ppb.ResolveConflictRequest_KEEP_LOCAL, "only-remote.txt": ppb.ResolveConflictRequest_KEEP_REMOTE} {
		c := plan[path]
		if _, err = worker.ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: choice, RequestId: path}); err != nil {
			t.Fatal(err)
		}
	}
	pass(t, worker, mapping.Id)
	if len(conflicts(t, worker, mapping)) != 0 {
		t.Fatal("conflict copy caused a loop")
	}
	if contents(t, remoteRoot, "only-local.txt") != "local-only" || contents(t, localRoot, "only-remote.txt") != "remote-only" {
		t.Fatal("explicit directions failed")
	}
	if _, err = os.Stat(filepath.Join(remoteRoot, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("default ignore leaked a secret")
	}
	configure(t, worker, probe, mapping, false)
	waitFor(t, func() bool { return probe.Watches.Load() == 0 })
	write(t, remoteRoot, "shared.txt", "remote-paused")
	worker.notify()
	time.Sleep(50 * time.Millisecond)
	if contents(t, localRoot, "shared.txt") != "local-new" {
		t.Fatal("paused worker wrote local files")
	}
	configure(t, worker, probe, mapping, true)
	pass(t, worker, mapping.Id)
	if contents(t, localRoot, "shared.txt") != "remote-paused" {
		t.Fatal("resume did not reconcile remote edit")
	}
	// Delete-versus-edit remains a conflict, and keep-both preserves the edit as
	// a visible copy while honoring the explicitly chosen local deletion.
	configure(t, worker, probe, mapping, false)
	if err = os.Remove(filepath.Join(localRoot, "shared.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, remoteRoot, "shared.txt", "remote-after-delete")
	configure(t, worker, probe, mapping, true)
	pass(t, worker, mapping.Id)
	deleted := conflicts(t, worker, mapping)["shared.txt"]
	if deleted == nil || deleted.Local.Exists || !deleted.Remote.Exists {
		t.Fatal("delete/edit not retained as conflict")
	}
	deletion := &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: deleted.Id, ExpectedGeneration: deleted.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "delete-both"}
	saved, err := worker.ResolveConflict(ctx, deletion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(remoteRoot, "shared.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("explicit deletion was not applied")
	}
	if err = worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	stopped = true
	restored := startWorker(t, ctx, state, core)
	defer restored.Stop(context.Background())
	configure(t, restored, probe, mapping, true)
	pass(t, restored, mapping.Id)
	replay, err := restored.ResolveConflict(ctx, deletion)
	if err != nil || !proto.Equal(replay, saved) {
		t.Fatal("restart replayed resolution or lost its result")
	}
	copies, err = filepath.Glob(filepath.Join(localRoot, "shared.txt.conflict-*"))
	if err != nil || len(copies) != 2 {
		t.Fatal("replay created another copy")
	}
	for _, name := range copies {
		if contents(t, localRoot, filepath.Base(name)) != contents(t, remoteRoot, filepath.Base(name)) {
			t.Fatal("conflict copy mismatch")
		}
	}
	mismatch := proto.Clone(deletion).(*ppb.ResolveConflictRequest)
	mismatch.Choice = ppb.ResolveConflictRequest_KEEP_REMOTE
	if _, err = restored.ResolveConflict(ctx, mismatch); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("resolution request ID accepted changed choice")
	}
}

func TestAuthenticationFailureNeverBecomesDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	local, _ := filepath.EvalSymlinks(t.TempDir())
	remote := t.TempDir()
	write(t, local, "safe.txt", "safe")
	write(t, remote, "safe.txt", "safe")
	tree, err := filetree.Open(remote, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private")})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	m := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local}
	probe := &coreProbe{Tree: tree}
	worker := startWorker(t, ctx, filepath.Join(t.TempDir(), "state"), coreClient(t, ctx, probe))
	defer worker.Stop(context.Background())
	configure(t, worker, probe, m, true)
	pass(t, worker, m.Id)
	configure(t, worker, probe, m, false)
	write(t, local, "safe.txt", "new-local")
	probe.Denied.Store(true)
	configure(t, worker, probe, m, true)
	task, err := worker.get(m.Id)
	if err != nil {
		t.Fatal(err)
	}
	task.op.Lock()
	err = task.reconcile(ctx)
	task.op.Unlock()
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected auth failure: %v", err)
	}
	status, err := worker.GetSyncState(ctx, scopeFor(m))
	if err != nil || status.State != "auth_required" {
		t.Fatal("auth failure state missing")
	}
	if contents(t, remote, "safe.txt") != "safe" {
		t.Fatal("auth failure altered remote")
	}
	probe.Denied.Store(false)
	pass(t, worker, m.Id)
	if contents(t, remote, "safe.txt") != "new-local" {
		t.Fatal("reauthentication lost pending local change")
	}
}
