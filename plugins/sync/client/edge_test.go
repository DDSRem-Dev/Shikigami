package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"google.golang.org/protobuf/proto"
)

const maxFileBytes = 10 << 20

func TestInterruptedResolutionAndCorruptLedgerFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	local, _ := filepath.EvalSymlinks(t.TempDir())
	remote := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	tree, err := filetree.Open(remote, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private")})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local}
	probe := &coreProbe{Tree: tree}
	core := coreClient(t, ctx, probe)
	worker := startWorker(t, ctx, state, core)
	configure(t, worker, probe, mapping, false)
	request := &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: "conf_interrupted", ExpectedGeneration: "old", Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "interrupted"}
	parameters, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err = worker.store.Update(func(snapshot *storage.Snapshot) error {
		_, _, err := snapshot.Request("resolve/"+mapping.Id, request.RequestId, parameters)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(state, "staging", mapping.Id, "transfer-orphan")
	if err = os.WriteFile(orphan, []byte("private staged content"), 0600); err != nil {
		t.Fatal(err)
	}
	restored := startWorker(t, ctx, state, core)
	configure(t, restored, probe, mapping, false)
	if _, err = restored.ResolveConflict(ctx, request); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("interrupted mutation replayed: %v", err)
	}
	if _, err = os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan staging snapshot retained")
	}
	if err = restored.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "ledger", "state.json")
	if err = os.WriteFile(path, []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := New()
	if err = failed.Init(ctx, sdk.InitContext{Core: core, Side: "client", StateDir: state}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("corrupt ledger silently recovered")
	}
	evidence, _ := os.ReadFile(path)
	if string(evidence) != "truncated" {
		t.Fatal("corrupt state evidence overwritten")
	}
}
func TestOversizeAndSymlinkObservationsDoNotImplyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	local, _ := filepath.EvalSymlinks(t.TempDir())
	remote := t.TempDir()
	write(t, local, "safe.txt", "unchanged")
	write(t, remote, "safe.txt", "unchanged")
	large, err := os.Create(filepath.Join(local, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = large.Truncate(maxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	large.Close()
	tree, err := filetree.Open(remote, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private")})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local}
	probe := &coreProbe{Tree: tree}
	worker := startWorker(t, ctx, filepath.Join(t.TempDir(), "state"), coreClient(t, ctx, probe))
	defer worker.Stop(context.Background())
	configure(t, worker, probe, mapping, true)
	pass(t, worker, mapping.Id)
	status, err := worker.GetSyncState(ctx, scopeFor(mapping))
	if err != nil || status.ReasonCode != "FILE_TOO_LARGE_SKIPPED" {
		t.Fatal("oversized file not reported")
	}
	if _, err = os.Stat(filepath.Join(remote, "large.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("oversized file copied")
	}
	configure(t, worker, probe, mapping, false)
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(local, "safe.txt")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(local, "safe.txt")); err != nil {
		t.Fatal(err)
	}
	configure(t, worker, probe, mapping, true)
	task, err := worker.get(mapping.Id)
	if err != nil {
		t.Fatal(err)
	}
	task.op.Lock()
	err = task.reconcile(ctx)
	task.op.Unlock()
	if err == nil {
		t.Fatal("symlink replacement was interpreted as a deletion")
	}
	if contents(t, remote, "safe.txt") != "unchanged" {
		t.Fatal("unreadable local version deleted or replaced counterpart")
	}
}

type changingSource struct {
	sdk.FileService
	root    string
	changed bool
}

func (s *changingSource) ReadFile(ctx context.Context, in *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	return s.FileService.ReadFile(ctx, in, func(frame *pb.FileData) error {
		if err := send(frame); err != nil {
			return err
		}
		if _, ok := frame.Payload.(*pb.FileData_Chunk); ok && !s.changed {
			s.changed = true
			return os.WriteFile(filepath.Join(s.root, in.Path), []byte("external update"), 0644)
		}
		return nil
	})
}
func TestSourceChangingDuringReadDoesNotCommitDestination(t *testing.T) {
	ctx := context.Background()
	localRoot := t.TempDir()
	remoteRoot := t.TempDir()
	write(t, localRoot, "file.txt", "source")
	write(t, remoteRoot, "file.txt", "destination")
	local, err := filetree.Open(localRoot, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private")})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	remote, err := filetree.Open(remoteRoot, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private")})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	source, _, err := observation(ctx, local, "proj_test", "file.txt", maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := observation(ctx, remote, "proj_test", "file.txt", maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	stagePath := filepath.Join(t.TempDir(), "stage")
	if err := os.Mkdir(stagePath, 0700); err != nil {
		t.Fatal(err)
	}
	stageDir, err := openStagingDirectory(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer stageDir.Close()
	task := &task{worker: &Worker{maxFileBytes: maxFileBytes}, data: record{Mapping: &ppb.Mapping{ProjectId: "proj_test"}}, staging: stagePath, stageDir: stageDir}
	err = task.transfer(ctx, &changingSource{FileService: local, root: localRoot}, remote, "file.txt", "file.txt", source, target)
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("changed source accepted: %v", err)
	}
	if contents(t, remoteRoot, "file.txt") != "destination" {
		t.Fatal("failed source snapshot committed destination")
	}
}
