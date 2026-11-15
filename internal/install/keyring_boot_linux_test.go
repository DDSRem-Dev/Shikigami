//go:build linux

package install

import (
	"context"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func keyringUnitSettings(data []byte) map[string][]string {
	values := map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = append(values[key], value)
		}
	}
	return values
}

func TestEncryptedKeyringUnitsKeepSecretsOutOfArgumentsAndLogs(t *testing.T) {
	account := &user.User{Uid: "1000", Gid: "1001", Username: "daemon", HomeDir: "/home/daemon"}
	for _, enrollment := range []bool{false, true} {
		data, err := renderKeyringUnlockUnit(account, 1000, enrollment, enrollment)
		if err != nil {
			t.Fatal(err)
		}
		settings := keyringUnitSettings(data)
		for key, want := range map[string]string{"User": "1000", "Group": "1001", "Type": "oneshot", "RemainAfterExit": "yes", "UMask": "0077", "LimitCORE": "0", "Restart": "no", "StandardOutput": "null", "StandardError": "null", "KillMode": "control-group", "Requires": "user@1000.service", "After": "user@1000.service", "Before": "shiki-daemon.service"} {
			if len(settings[key]) != 1 || settings[key][0] != want {
				t.Fatalf("%s=%v want %s", key, settings[key], want)
			}
		}
		action, credential := "unlock-keyring", keyringCredentialPath(1000)
		if enrollment {
			action, credential = "provision-keyring", keyringEnrollmentCredentialPath(1000)
		}
		if settings["ExecStart"][0] != BrowserRuntimePath+" "+action || settings["LoadCredentialEncrypted"][0] != "keyring-password:"+credential {
			t.Fatal("secret service permits unexpected command/path", settings)
		}
		if enrollment && len(settings["WantedBy"]) != 0 {
			t.Fatal("provisioning action must never be enabled for automatic boot")
		}
		if !enrollment && settings["WantedBy"][0] != "multi-user.target" {
			t.Fatal("normal unlock is not an explicit boot service")
		}
	}
}

func TestBootUnlockOptionsRequireExplicitSafeMode(t *testing.T) {
	base := ExtensionOptions{Component: "browser", User: "ordinary", BootUnlock: true, KeyringMode: "dedicated"}
	if err := validateExtensionOptions("maintenance", base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ExtensionOptions){
		func(o *ExtensionOptions) { o.Component = "region" },
		func(o *ExtensionOptions) { o.KeyringMode = "" },
		func(o *ExtensionOptions) { o.KeyringMode = "arbitrary" },
		func(o *ExtensionOptions) { o.KeyringMode = "existing-login"; o.ProvisionKeyring = true },
		func(o *ExtensionOptions) { o.ProvisionKeyring = true; o.ReuseKeyringCredential = true },
		func(o *ExtensionOptions) { o.BootUnlock = false },
	} {
		options := base
		change(&options)
		if err := validateExtensionOptions("setup", options); err == nil {
			t.Fatalf("unsafe boot unlock options accepted: %+v", options)
		}
	}
}

func TestBootUnlockFixtureNeverReadsOrFakesCredentialMaterial(t *testing.T) {
	options := signedExtensionFixture(t, optionalBundleFixture(t))
	options.BootUnlock, options.ProvisionKeyring, options.KeyringMode = true, true, "dedicated"
	options.ReadKeyringSecret = func(context.Context) ([]byte, error) { t.Fatal("inert staging read a password"); return nil, nil }
	plan, err := ExtensionPlatformPlan(t.Context(), "setup", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blocked) != 0 || !plan.BootUnlock || plan.KeyringMode != "dedicated" || len(plan.Commands) != 0 {
		t.Fatalf("unexpected inert plan: %+v", plan)
	}
	if err = ApplyExtensionPlan(t.Context(), "setup", options); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{keyringPolicyPath(plan.UID), "/etc/systemd/system/" + keyringUnit(plan.UID, false), "/etc/systemd/system/" + keyringUnit(plan.UID, true), BrowserRuntimeInstallLock} {
		if _, err := os.Stat(filepath.Join(options.RootPrefix, strings.TrimPrefix(path, "/"))); err != nil {
			t.Fatal("missing inert template", path, err)
		}
	}
	for _, path := range []string{keyringCredentialPath(plan.UID), keyringEnrollmentCredentialPath(plan.UID), "/var/lib/systemd/credential.secret"} {
		if _, err := os.Lstat(filepath.Join(options.RootPrefix, strings.TrimPrefix(path, "/"))); !os.IsNotExist(err) {
			t.Fatal("staging fabricated or leaked credential material", path, err)
		}
	}
	data, err := json.Marshal(options)
	if err != nil {
		t.Fatal("secret callback was serialized", err)
	}
	if strings.Contains(string(data), "ReadKeyringSecret") {
		t.Fatal("secret input callback entered JSON configuration")
	}
}
