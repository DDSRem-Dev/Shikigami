//go:build linux

package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestChromePolicyRequiresEnforcementAndRejectsConflicts(t *testing.T) {
	valid := []byte(`{"PasswordManagerEnabled":false,"SyncDisabled":true,"BrowserSignin":0}`)
	if err := validateChromePolicy(valid); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{`{}`, `{"PasswordManagerEnabled":true,"SyncDisabled":true,"BrowserSignin":0}`, `{"PasswordManagerEnabled":false,"SyncDisabled":false,"BrowserSignin":0}`, `{"PasswordManagerEnabled":false,"SyncDisabled":true,"BrowserSignin":1}`} {
		if validateChromePolicy([]byte(policy)) == nil {
			t.Fatalf("accepted unsafe policy %s", policy)
		}
	}
	for _, policy := range []string{`{"PasswordManagerEnabled":true}`, `{"SyncDisabled":false}`, `{"BrowserSignin":2}`, `{"CloudPolicyOverridesPlatformPolicy":true}`, `{"SyncDisabled":"true"}`} {
		if validatePolicyConflicts([]byte(policy)) == nil {
			t.Fatalf("accepted conflicting policy %s", policy)
		}
	}
	if err := validatePolicyConflicts([]byte(`{"UnrelatedExistingAdminPolicy":true,"PasswordManagerEnabled":false}`)); err != nil {
		t.Fatal(err)
	}
}
func TestChromeArgumentsKeepSandboxPrivateDebuggingAndSecretStorage(t *testing.T) {
	spec := Spec{ProfileDir: t.TempDir(), Region: &epb.RegionConfig{Languages: []string{"ja_JP", "ja"}}}
	arguments := chromeArguments(spec)
	joined := strings.Join(arguments, " ")
	for _, forbidden := range []string{"--no-sandbox", "--password-store=basic", "--remote-debugging-port", "--disable-web-security"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unsafe Chrome flag %s", forbidden)
		}
	}
	for _, required := range []string{"--remote-debugging-pipe", "--password-store=gnome-libsecret", "--disable-sync", "--lang=ja-JP"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %s", required)
		}
	}
	service := strings.Join(serviceArguments(Spec{Unit: "shiki-browser-test", InstanceID: "instance", ConfigPath: "/private/runtime.json"}), " ")
	for _, required := range []string{"--user", "--property=Restart=no", "--property=KillMode=mixed", "--property=TimeoutStopSec=15", "--property=UMask=0077", Binary + " --config /private/runtime.json"} {
		if !strings.Contains(service, required) {
			t.Fatalf("unsafe service launch missing %s", required)
		}
	}
}

func TestOrdinaryChromeHasNoDebuggingOrFingerprintOverride(t *testing.T) {
	for _, mode := range []string{ModeBrowse, ModeDiagnostic, ""} {
		arguments := chromeArguments(Spec{ProfileDir: "/private/profile", Mode: mode})
		joined := strings.Join(arguments, " ")
		if strings.Contains(joined, "--remote-debugging-pipe") != (mode != ModeBrowse) {
			t.Fatalf("incorrect debugging mode %q: %v", mode, arguments)
		}
		for _, forbidden := range []string{"--remote-debugging-port", "--no-sandbox", "AutomationControlled", "--user-agent", "--disable-web-security", "--password-store=basic"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("unsafe/fingerprint-altering option %q", forbidden)
			}
		}
		if !strings.Contains(joined, "--password-store=gnome-libsecret") {
			t.Fatal("secret storage gate removed")
		}
	}
}
func TestChromePreferencesPreserveCookiesAndUnrelatedUserChoices(t *testing.T) {
	profile := t.TempDir()
	directory := filepath.Join(profile, "chrome", "Default")
	if err := PrivateDir(directory); err != nil {
		t.Fatal(err)
	}
	preferences := []byte(`{"profile":{"password_manager_enabled":true,"user_chosen_setting":"preserve"},"unrelated_telemetry_choice":false}`)
	if err := os.WriteFile(filepath.Join(directory, "Preferences"), preferences, 0600); err != nil {
		t.Fatal(err)
	}
	cookie := []byte("persistent encrypted cookie fixture, never copied into diagnostics")
	if err := os.WriteFile(filepath.Join(directory, "Cookies"), cookie, 0600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{ProfileDir: profile, Region: &epb.RegionConfig{Languages: []string{"ja_JP", "ja"}, Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{"https://resolver.example/dns-query{?dns}"}}}}
	if err := configureChrome(spec); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(directory, "Cookies"))
	if err != nil || string(actual) != string(cookie) {
		t.Fatal("cookie store was changed")
	}
	var values map[string]any
	data, _ := os.ReadFile(filepath.Join(directory, "Preferences"))
	if json.Unmarshal(data, &values) != nil {
		t.Fatal("invalid preferences")
	}
	if values["unrelated_telemetry_choice"] != false || object(values, "profile")["user_chosen_setting"] != "preserve" || object(values, "profile")["password_manager_enabled"] != false || object(values, "intl")["accept_languages"] != "ja-JP,ja" {
		t.Fatalf("preferences not preserved/configured: %s", data)
	}
	var state map[string]any
	data, _ = os.ReadFile(filepath.Join(profile, "chrome", "Local State"))
	json.Unmarshal(data, &state)
	if object(state, "dns_over_https")["mode"] != "secure" {
		t.Fatal("explicit Chrome DNS not applied")
	}
}
func TestLaunchEnvironmentSeparatesIMEAndExplicitRegion(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "en_US:en")
	t.Setenv("LC_TIME", "en_US.UTF-8")
	spec := Spec{SocketDir: "/private/runtime", Region: &epb.RegionConfig{Timezone: "Asia/Tokyo", Locale: "ja_JP.UTF-8", Languages: []string{"ja_JP", "ja"}, Formats: map[string]string{"LC_NUMERIC": "en_US.UTF-8"}}}
	environment, err := launchEnvironment(spec)
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["LANG"] != "ja_JP.UTF-8" || values["LANGUAGE"] != "ja_JP:ja" || values["LC_TIME"] != "" || values["LC_NUMERIC"] != "en_US.UTF-8" || values["TZ"] != "Asia/Tokyo" || values["DBUS_SESSION_BUS_ADDRESS"] != "unix:path=/private/runtime/dbus.sock" || values["XMODIFIERS"] != "@im=fcitx" {
		t.Fatalf("unexpected region/IME env %#v", values)
	}
}
func TestChromeOutputNeverRetainsPageSecretsAndFlagsKeyringFailure(t *testing.T) {
	problems := newChromeProblems()
	problems.Write([]byte("page-cookie-secret\nERROR libsecret failed to obtain keyring key\n"))
	select {
	case <-problems.failure:
	default:
		t.Fatal("keyring failure was suppressed")
	}
	if strings.Contains(problems.summary(), "page-cookie-secret") {
		t.Fatal("page/profile output leaked")
	}
}

func TestServerIMERejectsNonUTF8InsteadOfPretendingCandidatesWork(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	if _, err := launchEnvironment(Spec{SocketDir: "/private/runtime"}); err == nil {
		t.Fatal("non-UTF8 XIM accepted without an actionable locale preflight")
	}
	t.Setenv("LC_ALL", "C.UTF-8")
	if _, err := launchEnvironment(Spec{SocketDir: "/private/runtime"}); err != nil {
		t.Fatalf("existing effective UTF-8 LC_ALL was not preserved: %v", err)
	}
}
