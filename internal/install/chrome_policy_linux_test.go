//go:build linux

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMandatoryChromePolicyHasClosedTypesAndSettings(t *testing.T) {
	if !exactChromeManagedPolicy(chromeManagedPolicy()) {
		t.Fatal("installer policy does not satisfy mandatory settings")
	}
	for _, data := range []string{
		`{"PasswordManagerEnabled":true}`,
		`{"SyncDisabled":false}`,
		`{"BrowserSignin":1}`,
		`{"PasswordManagerEnabled":null}`,
		`{"BrowserSignin":null}`,
		`{"SyncDisabled":"true"}`,
		`{"CloudPolicyOverridesPlatformPolicy":true}`,
		`{"CloudPolicyOverridesPlatformPolicy":null}`,
		`{"SyncDisabled":false,"SyncDisabled":true}`,
	} {
		if _, err := parseChromePolicy([]byte(data)); err == nil {
			t.Fatalf("unenforceable policy accepted: %s", data)
		}
	}
	if _, err := parseChromePolicy([]byte(`{"MetricsReportingEnabled":false,"UnrelatedSetting":null}`)); err != nil {
		t.Fatal("installer should preserve unrelated administrator policy", err)
	}
}

func TestPolicyPlanRejectsConflictsWithoutOverwritingOtherPolicies(t *testing.T) {
	root := extensionRootFixture(t)
	directory := filepath.Join(root, "etc/opt/chrome/policies/managed")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "administrator.json")
	original := []byte(`{"PasswordManagerEnabled":true,"MetricsReportingEnabled":false}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	options := ExtensionOptions{Component: "browser", User: ordinaryExtensionTestUser(t), StageOnly: true, RootPrefix: root}
	plan, err := ExtensionPlatformPlan(t.Context(), "setup", options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Blocked, " "), "PasswordManagerEnabled") {
		t.Fatal("conflicting machine policy not disclosed before confirmation", plan.Blocked)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatal("read-only planning overwrote administrator policy", err)
	}
	if err := os.WriteFile(path, []byte(`{"MetricsReportingEnabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := inspectChromeManagedPolicies(root, true); err != nil {
		t.Fatal("unrelated telemetry policy should remain untouched", err)
	}
}

func TestPolicyInspectionRejectsSymlinksAndExtraReservedSettings(t *testing.T) {
	root := extensionRootFixture(t)
	directory := filepath.Join(root, "etc/opt/chrome/policies/managed")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, chromeManagedPolicy(), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "symlink.json")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := inspectChromeManagedPolicies(root, true); err == nil {
		t.Fatal("symlink policy was trusted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reserved := filepath.Join(directory, filepath.Base(chromePolicyPath))
	if err := os.WriteFile(reserved, []byte(`{"PasswordManagerEnabled":false,"SyncDisabled":true,"BrowserSignin":0,"MetricsReportingEnabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := inspectChromeManagedPolicies(root, true); err == nil {
		t.Fatal("reserved policy with administrator additions would be overwritten")
	}
}
