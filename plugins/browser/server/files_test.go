//go:build linux

package server

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
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func uploadFrames(frames ...*epb.BrowserFileChunk) func(context.Context) (*epb.BrowserFileChunk, error) {
	index := 0
	return func(ctx context.Context) (*epb.BrowserFileChunk, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if index == len(frames) {
			return nil, io.EOF
		}
		frame := frames[index]
		index++
		return frame, nil
	}
}
func checksum(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func TestFileStreamingChecksumFinishAndDedupe(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	data := bytes.Repeat([]byte("payload"), 10000)
	frames := []*epb.BrowserFileChunk{{ProfileId: "one", Name: "upload.txt", TotalSize: int64(len(data)), RequestId: "upload"}, {Data: data[:60000]}, {Data: data[60000:], Sha256: checksum(data), Finish: true}}
	file, err := worker.WriteBrowserFile(context.Background(), uploadFrames(frames...))
	if err != nil || file.Sha256 != checksum(data) {
		t.Fatalf("upload %#v %v", file, err)
	}
	replayed, err := worker.WriteBrowserFile(context.Background(), uploadFrames(frames...))
	if err != nil || replayed.Sha256 != file.Sha256 {
		t.Fatalf("dedupe %#v %v", replayed, err)
	}
	var output bytes.Buffer
	var metadata, finished bool
	err = worker.ReadBrowserFile(context.Background(), &epb.BrowserFileRequest{ProfileId: "one", Area: "uploads", Name: "upload.txt"}, func(frame *epb.BrowserFileChunk) error {
		if !metadata {
			metadata = true
			if frame.TotalSize != int64(len(data)) || frame.Sha256 != checksum(data) || len(frame.Data) != 0 {
				t.Fatal("missing read header")
			}
		}
		if len(frame.Data) > 64<<10 {
			t.Fatal("unbounded read frame")
		}
		output.Write(frame.Data)
		if frame.Finish {
			finished = true
			if frame.Sha256 != checksum(data) {
				t.Fatal("missing terminal checksum")
			}
		}
		return nil
	})
	if err != nil || !finished || !bytes.Equal(output.Bytes(), data) {
		t.Fatalf("download complete=%v err=%v", finished, err)
	}
}
func TestFileRejectsUnsafeLinksNamesChecksumAndTruncation(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	for _, name := range []string{"../outside", "dir/file", "dir\\file", ".hidden", "bad\nname", ".."} {
		_, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: name, RequestId: "bad-name", Finish: true, Sha256: checksum(nil)}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("unsafe name %q: %v", name, err)
		}
	}
	_, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "bad.txt", RequestId: "bad-hash", TotalSize: 3, Data: []byte("abc"), Sha256: strings.Repeat("0", 64), Finish: true}))
	if connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatalf("checksum mismatch: %v", err)
	}
	_, err = worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "partial.txt", RequestId: "partial", TotalSize: 3, Data: []byte("abc")}))
	if connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatalf("missing finish: %v", err)
	}
	directory, _ := worker.stage(context.Background(), "one", "uploads")
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Fatalf("failed uploads left data: %#v", entries)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if err := worker.ReadBrowserFile(context.Background(), &epb.BrowserFileRequest{ProfileId: "one", Area: "uploads", Name: "link"}, func(*epb.BrowserFileChunk) error { t.Fatal("symlink bytes disclosed"); return nil }); err == nil {
		t.Fatal("symlink accepted")
	}
	os.Remove(filepath.Join(directory, "link"))
	if err := os.Link(outside, filepath.Join(directory, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ListBrowserFiles(context.Background(), &epb.BrowserFileRequest{ProfileId: "one", Area: "uploads"}); err == nil {
		t.Fatal("hardlink accepted")
	}
}
func TestFileCancelQuotaAndTerminalEOF(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	worker.config.MaxFile = 4
	worker.config.MaxStaging = 4
	data := []byte("1234")
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "first", RequestId: "first", TotalSize: 4, Data: data, Sha256: checksum(data), Finish: true})); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "second", RequestId: "second", TotalSize: 1, Data: []byte("x"), Sha256: checksum([]byte("x")), Finish: true})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("quota: %v", err)
	}
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "extra", RequestId: "extra", Sha256: checksum(nil), Finish: true}, &epb.BrowserFileChunk{Data: []byte("x")})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("extra frame accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := worker.WriteBrowserFile(ctx, func(ctx context.Context) (*epb.BrowserFileChunk, error) {
		calls++
		if calls == 1 {
			cancel()
			return &epb.BrowserFileChunk{ProfileId: "one", Name: "cancel", RequestId: "cancel", TotalSize: 1}, nil
		}
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancel receive calls=%d err=%v", calls, err)
	}
	if _, err := os.Stat(filepath.Join(worker.stateDir, "profiles", "one", "uploads", "cancel")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled upload published")
	}
}

func TestPluginStopCancelsBlockedDownloadWithoutStoppingBrowser(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	directory, err := worker.stage(context.Background(), "one", "downloads")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "download.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	result := make(chan error, 1)
	go func() {
		result <- worker.ReadBrowserFile(context.Background(), &epb.BrowserFileRequest{ProfileId: "one", Area: "downloads", Name: "download.txt"}, func(*epb.BrowserFileChunk) error { close(entered); <-release; return context.Canceled })
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("download never reached send")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Stop(ctx); err != nil {
		t.Fatalf("Stop deadlocked on transport Send: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("download handler did not return after plugin stop")
	}
	if manager.stops != 0 {
		t.Fatal("plugin Stop stopped browser")
	}
}

func TestUploadRejectsProgresslessOrOversizedMetadataFrames(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "file", RequestId: "empty", TotalSize: 1}, &epb.BrowserFileChunk{})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("progressless upload accepted: %v", err)
	}
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "file", RequestId: "large", ServerId: string(make([]byte, (64<<10)+4096)), Finish: true, Sha256: checksum(nil)})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unbounded metadata accepted: %v", err)
	}
}
