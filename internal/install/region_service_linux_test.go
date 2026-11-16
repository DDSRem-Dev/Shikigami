//go:build linux

package install

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRegionServicePolicyRequiresExactOrdinaryDaemon(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{"[Service]\nUser=shiki\n", true},
		{"[Service]\nUser=root\n", false},
		{"[Service]\nUser=shiki\nUser=other\n", false},
		{"[Unit]\nUser=shiki\n[Service]\n", false},
		{"[Service]\nUser=shiki # comment\n", false},
	} {
		if err := checkRegionDaemonUnit([]byte(tc.body), "shiki"); (err == nil) != tc.valid {
			t.Fatalf("unit validity=%v error=%v", tc.valid, err)
		}
	}
	plan := Plan{Role: "daemon", User: "shiki", Home: "/home/shiki", Config: "/etc/shiki/daemon.yaml", Path: "/usr/bin:/bin"}
	core, err := RenderService(plan, "/opt/shiki")
	if err != nil || !strings.Contains(string(core), "NoNewPrivileges=true") {
		t.Fatal("core isolation changed", err)
	}
}

func TestSignedRegionStageIncludesScopedDaemonPolicy(t *testing.T) {
	bundle := optionalBundleFixture(t)
	options := signedExtensionFixture(t, bundle)
	options.Component = "region"
	plan, err := ExtensionPlatformPlan(t.Context(), "setup", options)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyExtensionPlan(t.Context(), "setup", options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(options.RootPrefix, regionDaemonDropIn))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(regionDaemonServicePolicy(plan.UID)) || !strings.Contains(string(data), strconv.Itoa(plan.UID)) {
		t.Fatal("wrong daemon policy")
	}
	if len(plan.Authorization) != 6 {
		t.Fatal("helper authorization expanded")
	}
	for _, action := range plan.Authorization {
		if len(action.Args) != 1 || action.Args[0] == "guard" || action.Program != RegionHelperPath {
			t.Fatal("unexpected root action")
		}
	}
	options2 := signedExtensionFixture(t, bundle)
	options2.Component = "browser"
	if err = ApplyExtensionPlan(t.Context(), "setup", options2); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(options2.RootPrefix, regionDaemonDropIn)); !os.IsNotExist(err) {
		t.Fatal("browser-only support relaxed daemon", err)
	}
}
