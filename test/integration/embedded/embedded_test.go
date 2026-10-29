//go:build shiki_embedded

package embedded_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/pluginhost"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEmbeddedPluginExtractsAndRunsThroughNativeHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-embedded-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	pluginhost.Write(t, root, "embedded.txt", "embedded process content")
	core := &pluginhost.Core{Root: root}
	supervisor, err := host.NewSupervisor(ctx, host.SupervisorOptions{StateDir: filepath.Join(dir, "intent"), Validate: func(string, *structpb.Struct) error { return nil }, Config: func(id string, values *structpb.Struct) (host.Config, error) {
		manifest, err := host.ResolveBuiltinManifest(filepath.Join(dir, "plugins"), id, "server", buildinfo.Version)
		if err != nil {
			return host.Config{}, err
		}
		return host.Config{Manifest: manifest, RunDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "state"), HostInstanceID: "embedded-test", Core: core, Values: values, Policy: host.Policy{Side: "server", Capabilities: []string{"core.projects.read"}, Scopes: []*ppb.Scope{{ProjectId: "proj_test"}}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	supervisor.Start()
	list, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, item := range list.Plugins {
		if item.PluginId == "sync" {
			revision = item.Revision
		}
	}
	if _, err = supervisor.Manage(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "embedded-enable"}); err != nil {
		t.Fatal(err)
	}
	pluginhost.WaitFor(t, func() bool {
		return supervisor.Call(ctx, "sync", sdk.FileCapability, func(ctx context.Context, lifecycle sdk.Lifecycle) error {
			info, err := lifecycle.Files().StatFile(ctx, &pb.StatFileRequest{ProjectId: "proj_test", Path: "embedded.txt"})
			if err != nil {
				return err
			}
			if info.ContentHash == "" || info.Size != int64(len("embedded process content")) {
				t.Error("invalid embedded worker file observation")
			}
			return nil
		}) == nil
	})
}
