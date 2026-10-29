package filetree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

func fixture(t *testing.T, adjust func(*Options)) (*Tree, string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "state")
	opts := Options{ProjectID: "p", StateDir: state, PollInterval: time.Millisecond * 5}
	if adjust != nil {
		adjust(&opts)
	}
	tr, err := Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr, root, state
}
func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func write(t *Tree, ctx context.Context, p, base string, b []byte) (*pb.FileInfo, error) {
	offset := -1
	return t.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if offset < 0 {
			offset = 0
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "p", Path: p, BaseRevision: base, ContentHash: hash(b), Size: int64(len(b)), Mode: 0640}}}, nil
		}
		if offset == len(b) {
			return nil, io.EOF
		}
		n := min(len(b)-offset, ChunkSize)
		chunk := b[offset : offset+n]
		offset += n
		return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: chunk}}, nil
	})
}
func stat(t *testing.T, tr *Tree, p string) *pb.FileInfo {
	t.Helper()
	v, err := tr.StatFile(context.Background(), &pb.StatFileRequest{ProjectId: "p", Path: p})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func code(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if connect.CodeOf(err) != want {
		t.Fatalf("error=%v, code=%v want=%v", err, connect.CodeOf(err), want)
	}
}
func disk(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0640); err != nil {
		t.Fatal(err)
	}
}

func TestStreamRevisionHistoryAndDelete(t *testing.T) {
	tr, root, state := fixture(t, nil)
	ctx := context.Background()
	body := bytes.Repeat([]byte("abc123"), 100000)
	first, err := write(tr, ctx, "nested/data", "", body)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != hash(body) || first.Mode != 0640 {
		t.Fatal(first)
	}
	var got bytes.Buffer
	frames := 0
	err = tr.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: "p", Path: "nested/data", ExpectedRevision: first.Revision}, func(d *pb.FileData) error {
		if frames == 0 && d.GetInfo() == nil {
			t.Fatal("missing metadata")
		}
		if len(d.GetChunk()) > ChunkSize {
			t.Fatal("oversize chunk")
		}
		got.Write(d.GetChunk())
		frames++
		return nil
	})
	if err != nil || !bytes.Equal(got.Bytes(), body) || frames < 4 {
		t.Fatalf("read frames=%d err=%v", frames, err)
	}
	second, err := write(tr, ctx, "nested/data", first.Revision, []byte("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	records, err := tr.History()
	if err != nil || len(records) != 1 {
		t.Fatalf("history %v %v", records, err)
	}
	saved, err := os.ReadFile(filepath.Join(state, "history", records[0].ID))
	if err != nil || !bytes.Equal(saved, body) {
		t.Fatal("old content not preserved", err)
	}
	info, err := os.Stat(filepath.Join(state, "history", records[0].ID))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("history permissions", err)
	}
	code(t, tr.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: "p", Path: "nested/data", ExpectedRevision: first.Revision}), connect.CodeAborted)
	if err = tr.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: "p", Path: "nested/data", ExpectedRevision: second.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "nested/data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	records, err = tr.History()
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
}

func TestActualDiskRevisionsAndReadRace(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	p := filepath.Join(root, "a")
	disk(t, p, []byte("111"))
	before := stat(t, tr, "a")
	s, _ := os.Stat(p)
	disk(t, p, []byte("222"))
	if err := os.Chtimes(p, s.ModTime(), s.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := stat(t, tr, "a")
	if before.Revision == after.Revision {
		t.Fatal("mtime hid content change")
	}
	_, err := write(tr, context.Background(), "a", before.Revision, []byte("stale"))
	code(t, err, connect.CodeAborted)
	err = tr.ReadFile(context.Background(), &pb.ReadFileRequest{ProjectId: "p", Path: "a", ExpectedRevision: after.Revision}, func(d *pb.FileData) error {
		if d.GetInfo() != nil {
			disk(t, p, []byte("333"))
		}
		return nil
	})
	code(t, err, connect.CodeAborted)
	if err = os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	modeChanged := stat(t, tr, "a")
	if modeChanged.Revision == after.Revision {
		t.Fatal("mode absent from revision")
	}
}

func TestConfinedPathsAndSpecialFiles(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	outside := t.TempDir()
	disk(t, filepath.Join(outside, "secret"), []byte("untouched"))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret", "/tmp/secret", "a/../../secret", "a\\b", "a//b", ".shiki-sync-transfer-secret"} {
		_, err := write(tr, context.Background(), p, "", []byte("x"))
		code(t, err, connect.CodeInvalidArgument)
	}
	for _, p := range []string{"filelink", "link/secret", "fifo"} {
		_, err := tr.StatFile(context.Background(), &pb.StatFileRequest{ProjectId: "p", Path: p})
		if connect.CodeOf(err) != connect.CodeFailedPrecondition && connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("special file %s became %v", p, err)
		}
	}
	_, err := write(tr, context.Background(), "link/secret", "", []byte("evil"))
	if err == nil {
		t.Fatal("followed symlink")
	}
	listing, err := tr.ListFiles(context.Background(), &pb.ListFilesRequest{ProjectId: "p"})
	if err != nil || len(listing.Files) != 0 {
		t.Fatal(listing, err)
	}
	b, _ := os.ReadFile(filepath.Join(outside, "secret"))
	if string(b) != "untouched" {
		t.Fatal("escaped root")
	}
}

func TestPaginationLimitsAndOversizeMetadata(t *testing.T) {
	tr, root, _ := fixture(t, func(o *Options) { o.MaxFileBytes = 8; o.MaxPageSize = 2; o.MaxEntries = 5 })
	for _, p := range []string{"a", "b", "c"} {
		disk(t, filepath.Join(root, p), []byte("123"))
	}
	disk(t, filepath.Join(root, "large"), []byte("123456789"))
	ctx := context.Background()
	first, err := tr.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: "p", PageSize: 999})
	if err != nil || len(first.Files) != 2 || first.NextPageToken == "" {
		t.Fatal(first, err)
	}
	second, err := tr.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: "p", PageToken: first.NextPageToken})
	if err != nil || len(second.Files) != 2 || second.NextPageToken != "" {
		t.Fatal(second, err)
	}
	v := stat(t, tr, "large")
	if v.Size != 9 || v.Revision != "" || v.ContentHash != "" {
		t.Fatal(v)
	}
	err = tr.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: "p", Path: "large", ExpectedRevision: "anything"}, func(*pb.FileData) error { return nil })
	code(t, err, connect.CodeResourceExhausted)
	_, err = write(tr, ctx, "new", "", []byte("123456789"))
	code(t, err, connect.CodeResourceExhausted)
	disk(t, filepath.Join(root, "d"), nil)
	disk(t, filepath.Join(root, "e"), nil)
	_, err = tr.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: "p"})
	code(t, err, connect.CodeResourceExhausted)
}

func TestConditionalConcurrentWrites(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	ctx := context.Background()
	disk(t, filepath.Join(root, "a"), []byte("base"))
	base := stat(t, tr, "a")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, b := range []string{"one", "two"} {
		go func(b string) { <-start; _, err := write(tr, ctx, "a", base.Revision, []byte(b)); results <- err }(b)
	}
	close(start)
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatalf("both/neither writer succeeded: %v %v", a, b)
	}
	if a != nil {
		code(t, a, connect.CodeAborted)
	}
	if b != nil {
		code(t, b, connect.CodeAborted)
	}
	records, err := tr.History()
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
}

func TestCancelledAndMalformedTransfersLeaveNoTemporaryFiles(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	i := 0
	_, err := tr.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		i++
		if i == 1 {
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "p", Path: "a", Size: 1, ContentHash: hash([]byte("x")), Mode: 0600}}}, nil
		}
		cancel()
		return nil, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal(entries, e)
	}
	for _, oversize := range []bool{false, true} {
		i = 0
		_, err = tr.WriteFile(context.Background(), func() (*pb.WriteFileRequest, error) {
			i++
			if i == 1 {
				return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "p", Path: "b", Size: ChunkSize + 1, ContentHash: strings.Repeat("0", 64), Mode: 0600}}}, nil
			}
			if i > 2 {
				return nil, io.EOF
			}
			chunk := []byte("wrong")
			if oversize {
				chunk = make([]byte, ChunkSize+1)
			}
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: chunk}}, nil
		})
		if oversize {
			code(t, err, connect.CodeResourceExhausted)
		} else {
			code(t, err, connect.CodeDataLoss)
		}
	}
	entries, e = os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal(entries, e)
	}
}

func TestWatchCancellationAndPermissionErrors(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	err := tr.WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: "p"}, func(e *pb.FileEvent) error {
		if e.Type != pb.FileEvent_RESCAN_REQUIRED {
			t.Fatal(e)
		}
		count++
		if count == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || count != 2 {
		t.Fatal(count, err)
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks require ordinary user")
	}
	disk(t, filepath.Join(root, "private"), []byte("secret"))
	if err = os.Chmod(filepath.Join(root, "private"), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(root, "private"), 0600)
	_, err = tr.StatFile(context.Background(), &pb.StatFileRequest{ProjectId: "p", Path: "private"})
	code(t, err, connect.CodePermissionDenied)
}

func TestRecoveryAndHistoryBounds(t *testing.T) {
	tr, root, state := fixture(t, func(o *Options) { o.MaxFileBytes = 10; o.HistoryBytes = 20; o.HistoryVersions = 2 })
	ctx := context.Background()
	v, err := write(tr, ctx, "a", "", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"two", "three", "four", "five"} {
		v, err = write(tr, ctx, "a", v.Revision, []byte(s))
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := tr.History()
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	tmp := internalPrefix + newID()
	disk(t, filepath.Join(root, tmp), []byte("displaced"))
	if err = tr.putPending(pending{ID: tmp, Path: "a", Temp: tmp}); err != nil {
		t.Fatal(err)
	}
	if err = tr.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, Options{ProjectID: "p", StateDir: state, MaxFileBytes: 10, HistoryBytes: 20, HistoryVersions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	b, _ := os.ReadFile(filepath.Join(root, "a"))
	if string(b) != "five" {
		t.Fatal("recovery overwrote current file")
	}
	records, err = reopened.History()
	if err != nil || len(records) > 2 {
		t.Fatal(records, err)
	}
	saved, err := os.ReadFile(filepath.Join(state, "history", tmp))
	if err != nil || string(saved) != "displaced" {
		t.Fatal("recovery lost displaced content", err)
	}
	if _, err = os.Stat(filepath.Join(root, tmp)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary file remains", err)
	}
}

func TestSwappedParentCannotEscape(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	outside := t.TempDir()
	disk(t, filepath.Join(outside, "sentinel"), []byte("safe"))
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "parked"))
			_ = os.Symlink(outside, filepath.Join(root, "dir"))
			_ = os.Remove(filepath.Join(root, "dir"))
			_ = os.Rename(filepath.Join(root, "parked"), filepath.Join(root, "dir"))
		}
	}()
	for i := 0; i < 80; i++ {
		_, _ = write(tr, context.Background(), "dir/sentinel", "", []byte("evil"))
	}
	close(stop)
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(outside, "sentinel"))
	if err != nil || string(b) != "safe" {
		t.Fatal("parent swap escaped", err)
	}
}

func TestChangesDuringUploadAreNotOverwritten(t *testing.T) {
	tr, root, _ := fixture(t, nil)
	ctx := context.Background()
	disk(t, filepath.Join(root, "a"), []byte("base"))
	baseline := stat(t, tr, "a")
	i := 0
	_, err := tr.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		i++
		switch i {
		case 1:
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "p", Path: "a", BaseRevision: baseline.Revision, Size: 3, ContentHash: hash([]byte("new")), Mode: 0640}}}, nil
		case 2:
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("new")}}, nil
		default:
			disk(t, filepath.Join(root, "a"), []byte("external edit"))
			return nil, io.EOF
		}
	})
	code(t, err, connect.CodeAborted)
	b, _ := os.ReadFile(filepath.Join(root, "a"))
	if string(b) != "external edit" {
		t.Fatal("external edit was overwritten")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("upload temporary not removed", entries)
	}
}

func TestJournalPreservesDisplacedInodeAfterExchange(t *testing.T) {
	tr, root, state := fixture(t, nil)
	disk(t, filepath.Join(root, "a"), []byte("old version"))
	tmp := internalPrefix + newID()
	disk(t, filepath.Join(root, tmp), []byte("incoming version"))
	j := pending{ID: tmp, Path: "a", Temp: tmp}
	if err := tr.putPending(j); err != nil {
		t.Fatal(err)
	}
	d, err := tr.directory(".", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = exchange(int(d.Fd()), tmp, "a"); err != nil {
		t.Fatal(err)
	}
	if err = d.Sync(); err != nil {
		t.Fatal(err)
	}
	d.Close()
	tr.Close()
	reopened, err := Open(root, Options{ProjectID: "p", StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	old, err := os.ReadFile(filepath.Join(state, "history", tmp))
	if err != nil || string(old) != "old version" {
		t.Fatal(string(old), err)
	}
	current, err := os.ReadFile(filepath.Join(root, "a"))
	if err != nil || string(current) != "incoming version" {
		t.Fatal(string(current), err)
	}
}

func TestPendingPreservationFailsClosedAndStateIdentity(t *testing.T) {
	tr, root, state := fixture(t, nil)
	tmp := internalPrefix + newID()
	disk(t, filepath.Join(root, tmp), []byte("recover me"))
	if err := tr.putPending(pending{ID: tmp, Path: "a", Temp: tmp}); err != nil {
		t.Fatal(err)
	}
	_, err := write(tr, context.Background(), "new", "", []byte("new"))
	code(t, err, connect.CodeFailedPrecondition)
	tr.Close()
	_, err = Open(root, Options{ProjectID: "another", StateDir: state})
	if err == nil {
		t.Fatal("state reused for different project")
	}
	data, err := os.ReadFile(filepath.Join(root, tmp))
	if err != nil || string(data) != "recover me" {
		t.Fatal("failed open lost displaced version", err)
	}
}

func TestSkippedSymlinkEmitsBoundedDiagnostic(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var hints int
	tree, err := Open(root, Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private"), OnSkipped: func(_ context.Context, reason string) {
		if reason != "SYMLINK_OR_SPECIAL_FILE_SKIPPED" {
			t.Errorf("unexpected diagnostic: %s", reason)
		}
		hints++
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	for i := 0; i < 2; i++ {
		list, err := tree.ListFiles(context.Background(), &pb.ListFilesRequest{ProjectId: "proj_test"})
		if err != nil || len(list.GetFiles()) != 0 {
			t.Fatalf("symlink was exposed: %v", err)
		}
	}
	if hints != 1 {
		t.Fatalf("diagnostic count=%d, want one bounded hint", hints)
	}
}

func TestReplacedRootFailsClosed(t *testing.T) {
	tree, root, _ := fixture(t, nil)
	if err := os.WriteFile(filepath.Join(root, "original"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(moved)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.StatFile(context.Background(), &pb.StatFileRequest{ProjectId: "p", Path: "original"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("replaced root was accepted or treated as deletion: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "original")); err != nil {
		t.Fatal("original root evidence lost")
	}
}

func TestIgnorePolicyHasIndependentReadLimit(t *testing.T) {
	tree, root, _ := fixture(t, func(opts *Options) { opts.MaxFileBytes = 4 })
	if err := os.WriteFile(filepath.Join(root, ".shikiignore"), []byte("excluded-directory/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	matcher, err := tree.Ignore(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Ignored("excluded-directory/file", false) {
		t.Fatal("low transfer limit disabled ignore policy")
	}
}
