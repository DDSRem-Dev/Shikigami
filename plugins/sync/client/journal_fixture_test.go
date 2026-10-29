package client

import (
	"connectrpc.com/connect/v2"
	"context"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type journalCore struct{ sdk.CoreAPI }

func (journalCore) PublishEvent(context.Context, *ppb.PublishEventRequest) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}

type mutationProbe struct {
	stopAfterChunks int
	onFrame         func(*pb.WriteFileRequest)
	sdk.FileService
	before                func()
	after                 func()
	readCalls, failReadAt int
	lostACK               bool
	writes, deletes       int
}

func (p *mutationProbe) WriteFile(ctx context.Context, next func() (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	p.writes++
	if p.before != nil {
		p.before()
	}
	chunks := 0
	result, err := p.FileService.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if p.stopAfterChunks > 0 && chunks >= p.stopAfterChunks {
			return nil, io.EOF
		}
		frame, err := next()
		if frame != nil {
			if _, ok := frame.Payload.(*pb.WriteFileRequest_Chunk); ok {
				chunks++
			}
			if p.onFrame != nil {
				p.onFrame(frame)
			}
		}
		return frame, err
	})
	if err == nil && p.after != nil {
		p.after()
	}
	if err == nil && p.lostACK {
		return nil, connect.NewError(connect.CodeUnavailable, "lost commit ACK")
	}
	return result, err
}
func (p *mutationProbe) DeleteFile(ctx context.Context, in *pb.DeleteFileRequest) error {
	p.deletes++
	if p.before != nil {
		p.before()
	}
	err := p.FileService.DeleteFile(ctx, in)
	if err == nil && p.lostACK {
		return connect.NewError(connect.CodeUnavailable, "lost delete ACK")
	}
	return err
}

type journalFixture struct {
	task               *task
	remoteRoot, ledger string
	probe              *mutationProbe
}

func newJournalFixture(t *testing.T) *journalFixture {
	t.Helper()
	root := t.TempDir()
	localRoot, remoteRoot := filepath.Join(root, "local"), filepath.Join(root, "remote")
	for _, r := range []string{localRoot, remoteRoot} {
		if err := os.Mkdir(r, 0700); err != nil {
			t.Fatal(err)
		}
		write(t, r, "file.txt", "baseline")
	}
	trees := make([]*filetree.Tree, 2)
	for i, r := range []string{localRoot, remoteRoot} {
		tree, err := filetree.Open(r, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(root, "tree-"+string(rune('0'+i)))})
		if err != nil {
			t.Fatal(err)
		}
		trees[i] = tree
		t.Cleanup(func() { tree.Close() })
	}
	ledger := filepath.Join(root, "ledger")
	store, err := storage.Open(ledger, ledgerOptions())
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{stateDir: root, store: store, core: journalCore{}, maxFileBytes: 10 << 20, lifetime: t.Context(), tasks: map[string]*task{}}
	t.Cleanup(func() { w.store.Close() })
	staging := filepath.Join(root, "staging")
	if err := storage.EnsurePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	stageDir, err := openStagingDirectory(staging)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stageDir.Close() })
	probe := &mutationProbe{FileService: trees[1]}
	task := &task{worker: w, local: trees[0], remote: probe, staging: staging, stageDir: stageDir, data: record{LegacyUnverified: map[string]bool{}, Mapping: &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", Revision: "rev_test", LocalRoot: localRoot, SyncEnabled: true}, Baselines: map[string]*ppb.FileVersion{}, Conflicts: map[string]*ppb.Conflict{}, Copies: map[string]string{}, Operations: map[string]*fileOperation{}, State: "active"}}
	w.tasks["map_test"] = task
	base, _, err := observation(t.Context(), probe, "proj_test", "file.txt", w.maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	task.data.Baselines["file.txt"] = base
	if err := task.persist(); err != nil {
		t.Fatal(err)
	}
	return &journalFixture{task: task, remoteRoot: remoteRoot, ledger: ledger, probe: probe}
}
func (f *journalFixture) restart(t *testing.T) {
	t.Helper()
	f.task.worker.store.Close()
	store, err := storage.Open(f.ledger, ledgerOptions())
	if err != nil {
		t.Fatal(err)
	}
	f.task.worker.store = store
	snapshot, err := store.View()
	if err != nil {
		t.Fatal(err)
	}
	var r record
	if err := snapshot.Get(mappingsBucket, "map_test", &r); err != nil {
		t.Fatal(err)
	}
	f.task.data = r
	if err := verifyStages(t.Context(), f.task.stageDir, r.Operations); err != nil {
		t.Fatal(err)
	}
	if err := cleanStaging(f.task.stageDir, r.Operations); err != nil {
		t.Fatal(err)
	}
}
func (f *journalFixture) transfer(t *testing.T, download bool) error {
	t.Helper()
	source, destination := sdk.FileService(f.task.local), f.task.remote
	if download {
		source, destination = destination, source
	}
	from, _, err := observation(t.Context(), source, "proj_test", "file.txt", f.task.worker.maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	to, _, err := observation(t.Context(), destination, "proj_test", "file.txt", f.task.worker.maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	return f.task.transfer(t.Context(), source, destination, "file.txt", "file.txt", from, to)
}
func (f *journalFixture) saved(t *testing.T) record {
	t.Helper()
	s, err := f.task.worker.store.View()
	if err != nil {
		t.Fatal(err)
	}
	var r record
	if err := s.Get(mappingsBucket, "map_test", &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (p *mutationProbe) ReadFile(ctx context.Context, in *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	p.readCalls++
	if p.readCalls == p.failReadAt {
		return connect.NewError(connect.CodeUnavailable, "source disconnected between copy steps")
	}
	return p.FileService.ReadFile(ctx, in, send)
}
