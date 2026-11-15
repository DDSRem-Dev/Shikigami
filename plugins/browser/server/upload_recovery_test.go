//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

func TestUploadPublicationCrashRecoveryBeforeLinkAndEntryValidation(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot", manager)
	putProfile(t, worker, "one", false)
	directory, err := worker.stage(context.Background(), "one", "uploads")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("published before the process exited")
	temporary := filepath.Join(directory, ".upload-123456789")
	if err := os.WriteFile(temporary, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(temporary, filepath.Join(directory, "published.txt")); err != nil {
		t.Fatal(err)
	}
	for index := range stagingEntryLimit - 1 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("committed-%03d", index)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for index := range stagingEntryLimit + 1 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf(".upload-%d", index)), []byte("interrupted"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if file, err := browserruntime.OpenStage(directory, "published.txt"); err == nil {
		file.Close()
		t.Fatal("fixture does not reproduce the two-link publication window")
	}
	if _, err := boundedEntries(directory); err == nil {
		t.Fatal("fixture does not reproduce the interrupted entry-limit overflow")
	}
	if err := worker.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker = newWorker(t, state, "boot", manager)
	entries, err := boundedEntries(directory)
	if err != nil || len(entries) != stagingEntryLimit {
		t.Fatalf("interrupted names were not recovered before the entry limit: %d %v", len(entries), err)
	}
	file, err := browserruntime.OpenStage(directory, "published.txt")
	if err != nil {
		t.Fatalf("committed upload remains unreadable after recovery: %v", err)
	}
	_, gotChecksum, err := hashFile(context.Background(), file, worker.config.MaxFile)
	file.Close()
	if err != nil || gotChecksum != checksum(payload) {
		t.Fatalf("recovery changed the committed upload: %s %v", gotChecksum, err)
	}
	if err := cleanInterruptedUploads(state, worker.db.Profiles); err != nil {
		t.Fatalf("recovery is not idempotent: %v", err)
	}
	retry := &epb.BrowserFileChunk{ProfileId: "one", Name: "published.txt", RequestId: "recovered-upload", TotalSize: int64(len(payload)), Data: payload, Sha256: checksum(payload), Finish: true}
	if _, err := worker.WriteBrowserFile(context.Background(), uploadFrames(retry)); err != nil {
		t.Fatalf("lost upload response cannot be reconciled: %v", err)
	}
	conflicting := []byte("different upload")
	_, err = worker.WriteBrowserFile(context.Background(), uploadFrames(&epb.BrowserFileChunk{ProfileId: "one", Name: "published.txt", RequestId: "conflicting-upload", TotalSize: int64(len(conflicting)), Data: conflicting, Sha256: checksum(conflicting), Finish: true}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("recovery allowed replacing a committed filename: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "published.txt"))
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("conflicting retry changed the committed upload: %q %v", data, err)
	}
}

func TestUploadRecoveryUnlinksTemporaryEntriesWithoutFollowingThem(t *testing.T) {
	worker := newWorker(t, t.TempDir(), "boot", &testManager{})
	putProfile(t, worker, "one", false)
	directory, err := worker.stage(context.Background(), "one", "uploads")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Chmod(outside, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("must remain untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{".upload-1": secret, ".upload-3": outside, ".upload-not-temporary": secret} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(secret, filepath.Join(directory, ".upload-2")); err != nil {
		t.Fatal(err)
	}
	if err := cleanInterruptedUploads(worker.stateDir, worker.db.Profiles); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".upload-1", ".upload-2", ".upload-3"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary entry %q remains: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(directory, ".upload-not-temporary")); err != nil {
		t.Fatalf("unrecognized name was removed: %v", err)
	}
	file, err := browserruntime.OpenStage(outside, "secret")
	if err != nil {
		t.Fatalf("rooted unlink changed the outside file or left a hard link: %v", err)
	}
	file.Close()
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "must remain untouched" {
		t.Fatalf("followed an interrupted-upload link: %q %v", data, err)
	}
}
