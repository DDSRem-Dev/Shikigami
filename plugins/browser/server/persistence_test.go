//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

func TestProfileSaveRetainsPublishedRevisionAndReceiptAfterSyncFailure(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot", manager)
	first := putProfile(t, worker, "one", false)
	updated := proto.Clone(first).(*epb.BrowserProfile)
	updated.Name = "committed update"
	request := &epb.PutBrowserProfileRequest{Profile: updated, ExpectedRevision: first.Revision, RequestId: "committed-update"}
	writeJSON := worker.writeJSON
	syncFailure := errors.New("injected directory sync failure")
	worker.writeJSON = func(path string, value any, maximum int64) error {
		if err := writeJSON(path, value, maximum); err != nil {
			return err
		}
		return &browserruntime.PublishedError{Err: syncFailure}
	}
	_, err := worker.PutBrowserProfile(context.Background(), request)
	worker.writeJSON = writeJSON
	if !errors.Is(err, syncFailure) {
		t.Fatalf("lost the post-publication error: %v", err)
	}
	var disk database
	if err := browserruntime.ReadJSON(filepath.Join(state, "profiles.json"), &disk, maxDatabaseBytes); err != nil {
		t.Fatal(err)
	}
	committed := disk.Profiles["one"]
	if committed == nil || committed.Name != updated.Name || committed.Revision == first.Revision {
		t.Fatalf("fixture did not publish the update: %#v", committed)
	}
	if worker.db.Profiles["one"].Revision != committed.Revision || worker.persisted.Profiles["one"].Revision != committed.Revision || worker.db.Receipts[request.RequestId].Digest == "" {
		t.Fatal("worker reverted the published revision or deduplication receipt")
	}
	if _, err := worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: first, ExpectedRevision: first.Revision, RequestId: "stale-revision"}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("accepted the obsolete revision after publication: %v", err)
	}
	changedIntent := proto.Clone(request).(*epb.PutBrowserProfileRequest)
	changedIntent.Profile.Name = "different intent"
	if _, err := worker.PutBrowserProfile(context.Background(), changedIntent); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("reused the committed request ID with another intent: %v", err)
	}
	replay, err := worker.PutBrowserProfile(context.Background(), request)
	if err != nil || replay.Revision != committed.Revision {
		t.Fatalf("lost response cannot be replayed: %#v %v", replay, err)
	}
	prePublication := errors.New("injected pre-publication failure")
	worker.writeJSON = func(string, any, int64) error { return prePublication }
	failed := proto.Clone(committed).(*epb.BrowserProfile)
	failed.Name = "must not publish"
	_, err = worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: failed, ExpectedRevision: committed.Revision, RequestId: "not-published"})
	worker.writeJSON = writeJSON
	if !errors.Is(err, prePublication) || worker.db.Profiles["one"].Revision != committed.Revision || worker.persisted.Profiles["one"].Revision != committed.Revision {
		t.Fatalf("later failed write restored stale pre-commit state: %v", err)
	}
	if _, exists := worker.db.Receipts["not-published"]; exists {
		t.Fatal("failed write retained an uncommitted receipt")
	}
	if err := worker.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker = newWorker(t, state, "boot", manager)
	replay, err = worker.PutBrowserProfile(context.Background(), request)
	if err != nil || replay.Revision != committed.Revision {
		t.Fatalf("restart lost the published receipt/revision: %#v %v", replay, err)
	}
}

func TestProfileDatabaseEncodedLimitPreservesReadableStateAndReceipts(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	worker := newWorker(t, state, "boot", manager)
	template := "https://resolver.example/dns-query?x=" + strings.Repeat("&", 2000)
	region := &epb.RegionConfig{Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{template, template, template, template, template, template, template, template}}}
	if err := validateRegion(region); err != nil {
		t.Fatalf("fixture is not an accepted profile: %v", err)
	}
	accepted := 0
	for index := range 128 {
		id := fmt.Sprintf("profile-%03d", index)
		requestID := "put-" + id
		_, err := worker.PutBrowserProfile(context.Background(), &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Id: id, Name: id, Region: region}, RequestId: requestID})
		if err == nil {
			accepted++
			continue
		}
		if !strings.Contains(err.Error(), "encoded browser JSON exceeds its reader limit") || accepted == 0 {
			t.Fatalf("did not reject the encoded database bound: accepted=%d err=%v", accepted, err)
		}
		if worker.db.Profiles[id] != nil || len(worker.db.Profiles) != accepted || len(worker.persisted.Profiles) != accepted {
			t.Fatal("failed write changed in-memory profile membership")
		}
		if _, exists := worker.db.Receipts[requestID]; exists {
			t.Fatal("oversized write retained its receipt")
		}
		var disk database
		path := filepath.Join(state, "profiles.json")
		if err := browserruntime.ReadJSON(path, &disk, maxDatabaseBytes); err != nil {
			t.Fatalf("encoded overflow made the last database unreadable: %v", err)
		}
		if len(disk.Profiles) != accepted || len(disk.Receipts) != accepted || disk.Profiles[id] != nil {
			t.Fatal("oversized profile/receipt replaced the committed database")
		}
		encoded, err := json.Marshal(worker.db)
		if err != nil {
			t.Fatal(err)
		}
		bytesOnDisk, err := os.ReadFile(path)
		if err != nil || string(encoded) != string(bytesOnDisk) || len(bytesOnDisk) > maxDatabaseBytes {
			t.Fatalf("live database diverged from readable committed bytes: %v", err)
		}
		if err := worker.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		worker = newWorker(t, state, "boot", manager)
		if len(worker.db.Profiles) != accepted || len(worker.db.Receipts) != accepted {
			t.Fatal("overflow damaged restart readability or deduplication receipts")
		}
		return
	}
	t.Fatal("accepted profiles exceeded the database reader limit without rejection")
}
