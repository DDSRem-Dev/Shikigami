package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

type installerRuntimeProbe struct {
	*managementProbe
	mu                sync.Mutex
	base              string
	enabled           bool
	state, version    string
	revision          uint64
	actions           []pb.ManagePluginRequest_Action
	stopAccepted      chan struct{}
	stopRelease       <-chan struct{}
	failStop          bool
	failVersion       string
	rejectVersion     string
	hideRejectionOnce bool
	requests          map[string]*pb.PluginStatus
	requestBodies     map[string]string
}

func attachInstallerRuntime(t *testing.T, market *installerMarket, enabled bool) *installerRuntimeProbe {
	t.Helper()
	version, err := metaVersion("weather", market.base)
	if err != nil {
		t.Fatal(err)
	}
	state := "stopped"
	if enabled {
		state = "ready"
	}
	probe := &installerRuntimeProbe{managementProbe: &managementProbe{}, base: market.base, enabled: enabled, state: state, version: version, revision: 1, requests: map[string]*pb.PluginStatus{}, requestBodies: map[string]string{}}
	path := managementFixtureHandlers(t, probe, nil)
	cfg, err := config.LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PluginsDir = market.base
	if err := config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	market.a.configPath = path
	return probe
}

func (p *installerRuntimeProbe) statusLocked() *pb.PluginStatus {
	if version, err := metaVersion("weather", p.base); err == nil {
		p.version = version
	}
	if p.state == "stopping" && p.stopRelease != nil {
		select {
		case <-p.stopRelease:
			p.state = "stopped"
		default:
		}
	}
	status := &pb.PluginStatus{PluginId: "weather", BuildVersion: p.version, Revision: fmt.Sprint(p.revision), Enabled: p.enabled, State: p.state}
	if p.enabled && p.version == p.failVersion {
		status.State, status.ReasonCode = "failed", "FIXTURE_START_FAILED"
	}
	return status
}

func (p *installerRuntimeProbe) ListPlugins(_ context.Context, target *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	if target.Side != apb.PluginTarget_LOCAL || target.ServerId != "" {
		return nil, errors.New("installer crossed local runtime boundary")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return &pb.ListPluginsResponse{Plugins: []*pb.PluginStatus{p.statusLocked()}}, nil
}

func (p *installerRuntimeProbe) ManagePlugin(_ context.Context, request *apb.ManagePluginRequest) (*pb.PluginStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	body := fmt.Sprintf("%s/%d", request.Request.ExpectedRevision, request.Request.Action)
	if accepted := p.requests[request.Request.RequestId]; accepted != nil {
		if p.requestBodies[request.Request.RequestId] != body {
			return nil, connect.NewError(connect.CodeAlreadyExists, "request identity changed during replay")
		}
		return proto.Clone(accepted).(*pb.PluginStatus), nil
	}
	if request.Target.Side != apb.PluginTarget_LOCAL || request.Request.PluginId != "weather" || request.Request.ExpectedRevision != fmt.Sprint(p.revision) || request.Request.RequestId == "" || request.Request.Config != nil {
		return nil, connect.NewError(connect.CodeAborted, "wrong plugin/revision or user config overwritten")
	}
	p.actions = append(p.actions, request.Request.Action)
	if p.failStop && request.Request.Action == pb.ManagePluginRequest_DISABLE {
		return nil, connect.NewError(connect.CodeUnavailable, "stop failed")
	}
	if request.Request.Action == pb.ManagePluginRequest_ENABLE {
		version, err := metaVersion("weather", p.base)
		if err == nil && version == p.rejectVersion {
			if p.hideRejectionOnce {
				p.hideRejectionOnce = false
				return nil, connect.NewError(connect.CodeUnavailable, "rejection response lost before receipt")
			}
			return nil, connect.NewError(connect.CodeInvalidArgument, "replacement schema rejects existing config")
		}
	}
	p.revision++
	switch request.Request.Action {
	case pb.ManagePluginRequest_DISABLE:
		p.enabled, p.state = false, "stopped"
		if p.stopRelease != nil {
			p.state = "stopping"
		}
		if p.stopAccepted != nil {
			close(p.stopAccepted)
			p.stopAccepted = nil
		}
	case pb.ManagePluginRequest_ENABLE:
		p.enabled, p.state = true, "ready"
	default:
		return nil, errors.New("unexpected installer lifecycle action")
	}
	status := p.statusLocked()
	p.requests[request.Request.RequestId] = proto.Clone(status).(*pb.PluginStatus)
	p.requestBodies[request.Request.RequestId] = body
	return status, nil
}

func TestMarketplaceUpdateWaitsForStopAndRestoresEnabledState(t *testing.T) {
	for _, wasEnabled := range []bool{true, false} {
		t.Run(fmt.Sprint(wasEnabled), func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			probe := attachInstallerRuntime(t, market, wasEnabled)
			var accepted, release chan struct{}
			if wasEnabled {
				accepted, release = make(chan struct{}), make(chan struct{})
				probe.stopAccepted, probe.stopRelease = accepted, release
			}
			market.publish(t, "1.1.0", "weather", "1.1.0", "publisher-1")
			done := make(chan error, 1)
			go func() { done <- market.update(t, "") }()
			if wasEnabled {
				select {
				case <-accepted:
				case <-time.After(5 * time.Second):
					t.Fatal("installer never requested runtime stop")
				}
				if version, err := metaVersion("weather", market.base); err != nil || version != "1.0.0" {
					t.Fatalf("installer replaced files before stop completion: %q %v", version, err)
				}
				select {
				case err := <-done:
					t.Fatalf("installer returned before process cleanup completed: %v", err)
				default:
				}
				close(release)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("installer did not complete after runtime stop")
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			status := probe.statusLocked()
			if status.Enabled != wasEnabled || status.BuildVersion != "1.1.0" || wasEnabled && status.State != "ready" || !wasEnabled && status.State != "stopped" {
				t.Fatalf("previous enabled state not safely preserved: %v", status)
			}
			if wasEnabled && (len(probe.actions) != 2 || probe.actions[0] != pb.ManagePluginRequest_DISABLE || probe.actions[1] != pb.ManagePluginRequest_ENABLE) {
				t.Fatalf("wrong runtime stop/restore sequence: %v", probe.actions)
			}
			if !wasEnabled && len(probe.actions) != 0 {
				t.Fatalf("disabled update unexpectedly activated runtime: %v", probe.actions)
			}
		})
	}
}

func TestMarketplaceActivationFailureRollsBackPriorGoodPackage(t *testing.T) {
	for _, mode := range []string{"accepted-failed", "rejected-before-acceptance", "rejected-response-lost"} {
		t.Run(mode, func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			probe := attachInstallerRuntime(t, market, true)
			if mode == "accepted-failed" {
				probe.failVersion = "1.1.0"
			} else {
				probe.rejectVersion = "1.1.0"
				probe.hideRejectionOnce = mode == "rejected-response-lost"
			}
			market.publish(t, "1.1.0", "weather", "1.1.0", "publisher-1")
			if err := market.update(t, ""); err == nil || !strings.Contains(err.Error(), "activation failed") {
				t.Fatalf("activation failure was hidden: %v", err)
			}
			if mode == "rejected-response-lost" {
				if _, err := os.Stat(filepath.Join(market.base, ".install-transaction.json")); err != nil {
					t.Fatal("uncertain acceptance lost its recovery journal:", err)
				}
				err := market.a.withPluginMutation(t.Context(), func(*client.PluginInstallSession, *client.PluginInstallStore) error { return nil })
				if err == nil || !strings.Contains(err.Error(), "replacement schema") {
					t.Fatal("recovery did not report the definitive rejection:", err)
				}
			}
			if version, err := validateInstalledArtifact(t.Context(), "weather", market.base); err != nil || version != "1.0.0" {
				t.Fatalf("failed update deleted previous good package: %q %v", version, err)
			}
			probe.mu.Lock()
			status := probe.statusLocked()
			probe.mu.Unlock()
			if !status.Enabled || status.State != "ready" || status.BuildVersion != "1.0.0" {
				t.Fatalf("previous runtime was not restored: %v", status)
			}
			if _, err := os.Stat(filepath.Join(market.base, ".install-transaction.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successful rollback left transaction pending: %v", err)
			}
			market.publish(t, "1.2.0", "weather", "1.2.0", "publisher-1")
			if err := market.update(t, ""); err != nil {
				t.Fatalf("rejected activation blocked a later valid update: %v", err)
			}
		})
	}
}

func TestPluginStopFailureNeverMutatesInstalledCode(t *testing.T) {
	for _, uninstall := range []bool{false, true} {
		t.Run(fmt.Sprint(uninstall), func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			probe := attachInstallerRuntime(t, market, true)
			probe.failStop = true
			market.publish(t, "1.1.0", "weather", "1.1.0", "publisher-1")
			var err error
			if uninstall {
				command := market.a.pluginUninstallCmd()
				command.SetArgs([]string{"weather", "--yes"})
				err = command.ExecuteContext(t.Context())
			} else {
				err = market.update(t, "")
			}
			if err == nil || !strings.Contains(err.Error(), "stop") {
				t.Fatalf("runtime stop failure was ignored: %v", err)
			}
			if version, err := validateInstalledArtifact(t.Context(), "weather", market.base); err != nil || version != "1.0.0" {
				t.Fatalf("stop failure changed active files: %q %v", version, err)
			}
		})
	}
}

func TestPluginUninstallWaitsAndPreservesUserState(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
	market.install(t)
	probe := attachInstallerRuntime(t, market, true)
	accepted, release := make(chan struct{}), make(chan struct{})
	probe.stopAccepted, probe.stopRelease = accepted, release
	userState := filepath.Join(filepath.Dir(market.base), "user-plugin-state")
	if err := os.WriteFile(userState, []byte("retain config and user data"), 0600); err != nil {
		t.Fatal(err)
	}
	command := market.a.pluginUninstallCmd()
	command.SetArgs([]string{"weather", "--yes"})
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(t.Context()) }()
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("uninstall never requested process stop")
	}
	if _, err := os.Stat(filepath.Join(market.base, "weather")); err != nil {
		t.Fatalf("uninstall removed package before process exit: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("uninstall did not complete after cleanup")
	}
	if _, err := os.Stat(filepath.Join(market.base, "weather")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall did not remove package: %v", err)
	}
	if data, err := os.ReadFile(userState); err != nil || string(data) != "retain config and user data" {
		t.Fatalf("uninstall removed user state: %q %v", data, err)
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.enabled {
		t.Fatal("uninstalled plugin remained durably enabled")
	}
}
