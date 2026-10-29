package config

import (
	"testing"
	"time"
)

func TestAutoCleanupDefaultsStrictDurationsAndLegacyConfig(t *testing.T) {
	account := ordinaryAccount(t)
	prefix := "run_user: " + account.Username + "\nstate_dir: /tmp/shiki-cleanup-config\n"
	cfg, err := LoadDaemon(privateConfig(t, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoCleanup.Enabled || cfg.AutoCleanup.IdleThreshold.Value() != 168*time.Hour || cfg.AutoCleanup.WarnBeforeCleanup.Value() != 24*time.Hour {
		t.Fatal("unsafe cleanup defaults")
	}
	for _, extra := range []string{"enabled: true\n  idle_threshold: 10s\n  warn_before_cleanup: 3s", "enabled: false\n  idle_threshold: 168h\n  warn_before_cleanup: 24h"} {
		if _, err := LoadDaemon(privateConfig(t, prefix+"auto_cleanup:\n  "+extra+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range []string{"idle_threshold: 7d", "idle_threshold: -1h", "warn_before_cleanup: 0s", "warn_before_cleanup: 168h", "warn_before_cleanup: 500ms", "unknown: true"} {
		if _, err := LoadDaemon(privateConfig(t, prefix+"auto_cleanup:\n  "+extra+"\n")); err == nil {
			t.Fatal("invalid cleanup config accepted", extra)
		}
	}
}
