package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestInterruptedRuntimeRequestIsReplayedWithoutLosingUserIntent(t *testing.T) {
	for _, interrupted := range []string{"disable-accepted", "enable-accepted", "newer-user-disabled"} {
		t.Run(interrupted, func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			probe := attachInstallerRuntime(t, market, true)
			store, err := client.OpenPluginInstallStore(t.Context(), market.base)
			if err != nil {
				t.Fatal(err)
			}
			stage, err := os.MkdirTemp(market.base, ".plugin-stage-replay-")
			if err != nil {
				store.Close()
				t.Fatal(err)
			}
			tx, err := store.Begin("weather", "update", filepath.Base(stage), true, "1")
			if err != nil {
				store.Close()
				t.Fatal(err)
			}
			action, actionName := pb.ManagePluginRequest_DISABLE, "disable"
			if interrupted == "enable-accepted" {
				for _, name := range []string{"manifest.json", ".install-meta.json", ".artifact-integrity.json", "shiki-plugin-weather"} {
					data, err := os.ReadFile(filepath.Join(market.base, "weather", name))
					if err != nil {
						store.Close()
						t.Fatal(err)
					}
					mode := os.FileMode(0600)
					if name == "shiki-plugin-weather" {
						mode = 0700
					}
					if err := writePluginBytes(filepath.Join(stage, name), data, mode); err != nil {
						store.Close()
						t.Fatal(err)
					}
				}
				if err := syncPluginDirectory(stage); err != nil {
					store.Close()
					t.Fatal(err)
				}
				probe.mu.Lock()
				probe.enabled, probe.state, probe.revision = false, "stopped", 2
				probe.mu.Unlock()
				if err := store.ObserveRuntimeAction(tx, "2", true); err != nil {
					store.Close()
					t.Fatal(err)
				}
				if err := store.Publish(tx); err != nil {
					store.Close()
					t.Fatal(err)
				}
				action, actionName = pb.ManagePluginRequest_ENABLE, "enable"
			}
			if err := store.PrepareRuntimeAction(tx, actionName); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if interrupted == "newer-user-disabled" {
				probe.mu.Lock()
				probe.enabled, probe.state, probe.revision = false, "stopped", 3
				probe.mu.Unlock()
			} else {
				frontend, err := client.Open(t.Context(), market.a.configPath, client.Options{AutoStart: false, Timeout: time.Second})
				if err != nil {
					store.Close()
					t.Fatal(err)
				}
				_, err = frontend.Agent().ManagePlugin(t.Context(), &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: &pb.ManagePluginRequest{PluginId: "weather", Action: action, ExpectedRevision: tx.RuntimeExpectedRevision, RequestId: tx.RuntimeRequestID}})
				frontend.Close()
				if err != nil {
					store.Close()
					t.Fatal(err)
				}
				// No ObserveRuntimeAction: model a crash after IPC acceptance
				// but before the client can persist or receive its completion.
			}
			store.Close()
			if err := market.a.withPluginMutation(t.Context(), func(*client.PluginInstallSession, *client.PluginInstallStore) error { return nil }); err != nil {
				t.Fatal(err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			status := probe.statusLocked()
			wantEnabled := interrupted != "newer-user-disabled"
			if status.Enabled != wantEnabled || wantEnabled && status.State != "ready" || !wantEnabled && status.State != "stopped" {
				t.Fatalf("recovery lost accepted request or overrode newer desired state: %v", status)
			}
			wantActions := 2
			if interrupted == "enable-accepted" {
				wantActions = 1
			} else if interrupted == "newer-user-disabled" {
				wantActions = 0
			}
			if len(probe.actions) != wantActions {
				t.Fatalf("journaled IPC was duplicated or newer state overwritten: %v", probe.actions)
			}
		})
	}
}
