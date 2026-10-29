//go:build linux && e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Python runs the actual privileged installer and real service managers. This
// ordinary-user probe permits each switch only after all live data-plane
// conditions are observable; a static file or an open-but-idle RPC is not
// evidence of an in-flight upload.
func TestSystemdUpgradeWithActiveTransfers(t *testing.T) {
	path := os.Getenv("SHIKI_UPGRADE_TEST_CONFIG")
	if path == "" {
		t.Skip("opt-in active upgrade acceptance: run scripts/test-systemd.sh")
	}
	if os.Geteuid() == 0 {
		t.Fatal("active transfer probe must use the ordinary service user")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	a := &upgradeActivity{t: t, ctx: ctx}
	if err := json.Unmarshal(data, &a.cfg); err != nil {
		t.Fatal(err)
	}
	a.proxy = newUpgradeProxy(t, ctx, a.cfg.TLSAddress)
	a.echo = newUpgradeEcho(t)
	a.prepare()
	a.record("initialized", map[string]any{"ready": true})
	for _, boundary := range []struct {
		label     string
		direction upgradeTransferDirection
		host      upgradeReplacedHost
	}{
		{"agent-upgrade", upgradeUpload, upgradeAgent},
		{"agent-rollback", upgradeUpload, upgradeAgent},
		{"daemon-upgrade", upgradeDownload, upgradeDaemon},
		{"daemon-rollback", upgradeDownload, upgradeDaemon},
	} {
		label := boundary.label
		a.waitFile(label + ".begin")
		if !t.Run(label, func(t *testing.T) {
			phase := *a
			phase.t = t
			phase.exercise(label, boundary.direction, boundary.host)
		}) {
			return
		}
	}
	// Persist a usable direct endpoint before the test proxy is removed. The
	// mapping and listener were owned by the product and are stopped explicitly.
	a.cli("sync", "pause", a.mapping.Id)
	a.cli("ports", "stop", a.forward.Spec.Id)
	a.cli("servers", "update", a.cfg.Server, "--tls-address", a.cfg.TLSAddress)
	a.record("complete", map[string]any{"verified_boundaries": 4})
}
