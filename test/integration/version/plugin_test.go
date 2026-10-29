package version_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/types/known/structpb"
)

func pluginConfiguration(t *testing.T, offer, peer, init *pb.ProtocolOffer) (host.Config, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shiki-ver-plugin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	marker := filepath.Join(root, "calls")
	path := filepath.Join(root, "peer.json")
	writeJSON(t, path, fixtureConfiguration{Mode: "plugin", Offer: peer, InitOffer: init, Build: "synthetic-plugin-b", Marker: marker})
	return host.Config{Manifest: host.Manifest{Name: "sync", Version: "synthetic-plugin-b", Executable: executable, SHA256: hex.EncodeToString(sum[:]), Side: "client", Protocol: offer}, Args: []string{"-test.run=^TestVersionProcessFixture$"}, Env: []string{"SHIKI_VERSION_FIXTURE=" + path, "GORACE=halt_on_error=1 log_path=" + filepath.Join(root, "race")}, RunDir: filepath.Join(root, "r"), StateDir: filepath.Join(root, "s"), HostInstanceID: "synthetic-host-a", Core: prpc.UnimplementedCoreAPIHandler{}, Policy: host.Policy{Side: "client"}, StartupTimeout: 5 * time.Second}, marker
}
func markerText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func pluginGone(t *testing.T, marker string) {
	t.Helper()
	first := strings.Split(markerText(t, marker), "\n")[0]
	pid, err := strconv.Atoi(strings.TrimPrefix(first, "pid:"))
	if err != nil || pid <= 1 {
		t.Fatal("invalid fixture PID")
	}
	waitFor(t, func() error {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("rejected plugin %d is still alive", pid)
	})
}
func TestRealPluginProcessVersionAndInitAgreement(t *testing.T) {
	local := &pb.ProtocolOffer{Major: 1, MinMinor: 2, MaxMinor: 5, Capabilities: []string{"core.scope.v1", "synthetic.optional.v1"}, RequiredCapabilities: []string{"core.scope.v1"}}
	for _, test := range []struct {
		name       string
		peer, init *pb.ProtocolOffer
		accept     bool
	}{
		{"overlapping-minor", &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 4, Capabilities: []string{"core.scope.v1"}}, nil, true},
		{"major", &pb.ProtocolOffer{Major: 2, MinMinor: 2, MaxMinor: 5, Capabilities: []string{"core.scope.v1"}}, nil, false},
		{"disjoint", &pb.ProtocolOffer{Major: 1, MinMinor: 6, MaxMinor: 7, Capabilities: []string{"core.scope.v1"}}, nil, false},
		{"missing-host-required", &pb.ProtocolOffer{Major: 1, MinMinor: 2, MaxMinor: 5}, nil, false},
		{"missing-worker-required", &pb.ProtocolOffer{Major: 1, MinMinor: 2, MaxMinor: 5, Capabilities: []string{"core.scope.v1", "synthetic.worker-required.v1"}, RequiredCapabilities: []string{"synthetic.worker-required.v1"}}, nil, false},
		{"init-rechecks-agreement", &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 4, Capabilities: []string{"core.scope.v1"}}, &pb.ProtocolOffer{Major: 2, Capabilities: []string{"core.scope.v1"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, marker := pluginConfiguration(t, local, test.peer, test.init)
			instance, err := host.Launch(t.Context(), cfg)
			if test.accept {
				if err != nil {
					t.Fatal(err)
				}
				if instance.Snapshot().Protocol.Minor != 4 {
					t.Fatal("host did not select greatest common minor")
				}
				if !strings.Contains(markerText(t, marker), "Init:4\nStart\n") {
					t.Fatal("selected agreement did not reach Init/Start")
				}
				if err := instance.Stop(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					instance.Stop(t.Context())
					t.Fatal("incompatible plugin started")
				}
				requireCode(t, err, connect.CodeFailedPrecondition)
				if strings.Contains(markerText(t, marker), "Init:") || strings.Contains(markerText(t, marker), "Start\n") {
					t.Fatal("incompatible plugin ran business initialization")
				}
			}
			pluginGone(t, marker)
			reports, err := filepath.Glob(filepath.Join(filepath.Dir(marker), "race.*"))
			if err != nil || len(reports) > 0 {
				t.Fatal("plugin race report", reports, err)
			}
		})
	}
}
func TestSupervisorMarksIncompatibleWithoutDataFallback(t *testing.T) {
	local := &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1"}, RequiredCapabilities: []string{"core.scope.v1"}}
	cfg, marker := pluginConfiguration(t, local, &pb.ProtocolOffer{Major: 2, Capabilities: []string{"core.scope.v1"}}, nil)
	supervisor, err := host.NewSupervisor(t.Context(), host.SupervisorOptions{StateDir: filepath.Join(filepath.Dir(marker), "supervisor"), Validate: func(string, *structpb.Struct) error { return nil }, Config: func(string, *structpb.Struct) (host.Config, error) { return cfg, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	supervisor.Start()
	list, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, p := range list.Plugins {
		if p.PluginId == "sync" {
			revision = p.Revision
		}
	}
	if _, err := supervisor.Manage(t.Context(), &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "synthetic-enable"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() error {
		list, err := supervisor.List()
		if err != nil {
			return err
		}
		for _, p := range list.Plugins {
			if p.PluginId == "sync" && p.State == "incompatible" {
				return nil
			}
		}
		return errors.New("waiting for incompatible state")
	})
	called := false
	err = supervisor.Call(t.Context(), "sync", "core.scope.v1", func(context.Context, sdk.Lifecycle) error { called = true; return nil })
	requireCode(t, err, connect.CodeFailedPrecondition)
	if called {
		t.Fatal("incompatible worker fell back into caller business implementation")
	}
	if strings.Contains(markerText(t, marker), "Init:") {
		t.Fatal("incompatible worker initialized")
	}
	pluginGone(t, marker)
}
