package runtime

import (
	"encoding/json"
	"strings"
	"testing"
)

func modeSpec() Spec {
	return Spec{ProfileID: "profile", InstanceID: "instance", Token: "token", Unit: "unit", ConfigPath: "/private/config.json", SocketDir: "/private/runtime", ProfileDir: "/private/profile", ExecutableSHA256: strings.Repeat("a", 64), Width: 1280, Height: 720}
}

func TestBrowserModesPreserveLegacyAndRejectUnknownLaunches(t *testing.T) {
	for _, mode := range []string{"", ModeBrowse, ModeDiagnostic, "stealth"} {
		spec := modeSpec()
		spec.Mode = mode
		err := spec.Validate()
		if (err != nil) != (mode == "stealth") {
			t.Fatalf("wrong mode validation for %q: %v", mode, err)
		}
		if mode == "" && spec.BrowserMode() != ModeDiagnostic {
			t.Fatal("legacy debugging instance was relabeled as ordinary browsing")
		}
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "browser_mode") != (mode != "") {
			t.Fatalf("legacy/new JSON shape changed: %s", data)
		}
	}
	for _, mode := range []string{"", "unknown"} {
		_, err := (SystemManager{}).Prepare(t.Context(), "/private/state", "profile", "revision", "instance", nil, "region", 1280, 720, mode)
		if err == nil || !strings.Contains(err.Error(), "explicit browse or diagnostic") {
			t.Fatal("new launch accepted implicit/unknown mode", err)
		}
	}
}

func TestOrdinaryModeDiagnoseRequiresExplicitRestart(t *testing.T) {
	spec := modeSpec()
	spec.Mode = ModeBrowse
	report, err := (SystemManager{}).Diagnose(t.Context(), spec, nil)
	if report != nil || err == nil || err.Error() != DiagnosticsDisabledReason {
		t.Fatalf("ordinary mode fabricated diagnostics or used a generic error: %#v %v", report, err)
	}
}

func TestNewBrowserLaunchRecordCannotLoseMode(t *testing.T) {
	spec := modeSpec()
	spec.ConfigPath = "/private/" + spec.InstanceID + ".launch.json"
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "missing its explicit mode") {
		t.Fatal("new launch silently fell back to legacy diagnostics", err)
	}
	for _, mode := range []string{ModeBrowse, ModeDiagnostic} {
		spec.Mode = mode
		if err := spec.Validate(); err != nil {
			t.Fatal("valid new launch rejected", err)
		}
	}
	spec.Mode = ""
	spec.ConfigPath = "/private/" + spec.InstanceID + ".json"
	if err := spec.Validate(); err != nil || spec.BrowserMode() != ModeDiagnostic {
		t.Fatal("legacy launch compatibility lost", err)
	}
}
