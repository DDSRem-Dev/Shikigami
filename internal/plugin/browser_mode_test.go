package plugin

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type modeBrowserClient struct {
	sdk.BrowserClient
	calls int
}

func (c *modeBrowserClient) StartBrowser(_ context.Context, request *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	c.calls++
	return &epb.BrowserStatus{Mode: "diagnostic"}, nil
}

func TestBrowserStartChecksActualWorkerModeCapability(t *testing.T) {
	for _, modeAware := range []bool{false, true} {
		store, err := storage.Open(filepath.Join(t.TempDir(), "intent"), storage.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		want := desiredPlugin{ID: "browser", Enabled: true, Revision: "one"}
		if err := store.Update(func(s *storage.Snapshot) error { return s.Put("plugins", "browser", want) }); err != nil {
			t.Fatal(err)
		}
		capabilities := []string{sdk.BrowserCapability}
		if modeAware {
			capabilities = append(capabilities, sdk.BrowserLaunchModeCapability)
		}
		client := &modeBrowserClient{}
		instance := &Instance{grant: &grant{valid: true, lifetime: t.Context(), deadline: time.Now().Add(time.Hour)}, done: make(chan struct{}), lifecycle: uploadLifecycle{client: client}, snapshot: Snapshot{State: "ready", Protocol: &pb.ProtocolAgreement{Capabilities: capabilities}}}
		supervisor := &Supervisor{store: store, slots: map[string]*supervised{"browser": {status: &pb.PluginStatus{State: "ready", Revision: want.Revision}, instance: instance}}}
		data := &DaemonDataPlane{Supervisor: supervisor}
		_, err = data.StartBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", Diagnostics: true})
		if modeAware {
			if err != nil || client.calls != 1 {
				t.Fatal("new worker not called", err, client.calls)
			}
		} else if connect.CodeOf(err) != connect.CodeFailedPrecondition || client.calls != 0 {
			t.Fatal("old worker silently ignored diagnostics", err, client.calls)
		}
	}
}
