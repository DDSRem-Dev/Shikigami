package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

func browserAuthorizationCore(t testing.TB, unrelated int) *pluginCore {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Update(func(snapshot *storage.Snapshot) error {
		for index := range unrelated {
			if err := snapshot.Put("unrelated", fmt.Sprint(index), map[string]string{"content": strings.Repeat("x", 4096)}); err != nil {
				return err
			}
		}
		return snapshot.Put("servers", "srv_browser", &apb.ServerRecord{Config: &apb.ServerConfig{Id: "srv_browser", Name: "browser"}, CredentialPresent: true, CredentialRevision: "cred_one"})
	}); err != nil {
		t.Fatal(err)
	}
	return &pluginCore{id: "browser", resources: &Resources{store: store}}
}

func BenchmarkBrowserFrameAuthorization(b *testing.B) {
	for _, count := range []int{0, 1024} {
		b.Run(fmt.Sprintf("unrelated_%dKiB", count*4), func(b *testing.B) {
			core := browserAuthorizationCore(b, count)
			scope := &ppb.Scope{ServerId: "srv_browser"}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := core.authorize(ctx, scope); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestBrowserFrameAuthorizationReadsLiveServerAndHonorsRevokedContext(t *testing.T) {
	core := browserAuthorizationCore(t, 0)
	scope := &ppb.Scope{ServerId: "srv_browser"}
	if err := core.authorize(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	// Host grant/route revocation cancels the callback context. No read cache
	// may admit subsequent frames on that context, even while the server exists.
	revoked, revoke := context.WithCancel(t.Context())
	revoke()
	if err := core.authorize(revoked, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("revoked frame admitted: %v", err)
	}
	for _, forbidden := range []*ppb.Scope{nil, {}, {ServerId: "srv_browser", ProjectId: "project"}, {ServerId: "srv_browser", SessionId: "session"}, {ServerId: "srv_browser", MappingId: "mapping"}} {
		if err := core.authorize(t.Context(), forbidden); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("invalid browser scope admitted: %v", err)
		}
	}
	if err := core.resources.store.Update(func(snapshot *storage.Snapshot) error { delete(snapshot.Buckets["servers"], "srv_browser"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := core.authorize(t.Context(), scope); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("deleted server still authorized: %v", err)
	}
	if err := core.resources.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := core.authorize(t.Context(), scope); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("closed store admitted: %v", err)
	}
}
