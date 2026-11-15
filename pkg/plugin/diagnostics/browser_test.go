package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/encoding/protojson"
)

func scriptConfig(t *testing.T, req *epb.DiagnosticRequest) map[string]any {
	t.Helper()
	script, err := BrowserScript(req)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "(" + browserSource + ")("
	if !strings.HasPrefix(script, prefix) || !strings.HasSuffix(script, ")") {
		t.Fatal("caller influenced fixed executable script")
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(script, prefix), ")")), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestBrowserProgramIsFixedAndExternalConsentIsIndependent(t *testing.T) {
	config := scriptConfig(t, &epb.DiagnosticRequest{ProfileId: "managed-profile", Providers: []*epb.ProbeProvider{{Id: "ignored", Kind: "ip-echo", Url: "https://127.0.0.1/"}}})
	if config["allowExternal"] != false || config["allowWebRTC"] != false || config["providers"] != nil {
		t.Fatalf("unapproved network providers in program: %v", config)
	}
	if config["targetURL"] != BrowserDiagnosticURL {
		t.Fatal("diagnostic script is not target-bound")
	}
	config = scriptConfig(t, &epb.DiagnosticRequest{ProfileId: "managed-profile", AllowWebrtc: true})
	if config["allowWebRTC"] != true || config["allowExternal"] != false {
		t.Fatal("WebRTC authorization silently enables third parties")
	}
	for _, required := range []string{`credentials: "omit"`, `redirect: "error"`, `referrerPolicy: "no-referrer"`, `iceServers: []`, `location.href !== config.targetURL`, `size > (parseJSON ? 16384 : 512)`} {
		if !strings.Contains(browserSource, required) {
			t.Fatalf("fixed program missing security boundary: %s", required)
		}
	}
	for _, forbidden := range []string{"eval(", "new Function", "document.cookie", "localStorage", "getUserMedia", "toDataURL", "toBlob", "stun:", "turn:"} {
		if strings.Contains(browserSource, forbidden) {
			t.Fatalf("unexpected active or privacy-sensitive browser feature: %s", forbidden)
		}
	}
	for _, id := range []string{"", "../profile", `profile");throw new Error("injected`} {
		if _, err := BrowserScript(&epb.DiagnosticRequest{ProfileId: id}); err == nil {
			t.Fatalf("invalid profile id accepted: %q", id)
		}
	}
}

func TestProviderURLIsJSONDataNeverCallerCode(t *testing.T) {
	req := &epb.DiagnosticRequest{ProfileId: "managed-profile", AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "fixture", Kind: "geo", Url: `https://one.example.org/path/");globalThis.injected=true;("`}}}
	config := scriptConfig(t, req)
	providers, ok := config["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatal("explicit provider data lost")
	}
	if config["profileId"] != "managed-profile" || config["allowExternal"] != true {
		t.Fatal("script configuration mutated")
	}
}

func browserFixture() *epb.DiagnosticReport {
	now := time.Now().Unix()
	return &epb.DiagnosticReport{ObservedAt: now, Observations: []*epb.DiagnosticObservation{
		{Check: "browser.context", State: "observed", Value: jsonText(map[string]string{"profile_id": "managed-profile", "target_url": BrowserDiagnosticURL}), Source: browserSourceName, Vantage: "server-chrome:managed-profile", ObservedAt: now, Limitation: "Runtime must attest its private CDP target; not client noVNC."},
		{Check: "browser.secure-dns.native", State: "unknown", Source: browserSourceName, Vantage: "server-chrome:managed-profile", ObservedAt: now, Limitation: "Page JavaScript cannot observe effective Secure DNS."},
		{Check: "https.egress.ipv4", State: "observed", Value: `{"ip":"1.1.1.1"}`, Source: "provider fixture (ip-echo), https://one.example.org/ip", Vantage: "external-observer:fixture:server-chrome:managed-profile", ObservedAt: now, Limitation: "Raw external claim; not geography or consistency pass."},
	}}
}

func encodedBrowserFixture(t *testing.T, report *epb.DiagnosticReport) []byte {
	t.Helper()
	content, err := protojson.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestDecodeBrowserReportKeepsUnknownAndExternalVantage(t *testing.T) {
	decoded, err := DecodeBrowserReport(encodedBrowserFixture(t, browserFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if got := findObservation(t, decoded, "browser.secure-dns.native"); got.GetState() != "unknown" || got.GetValue() != "" {
		t.Fatalf("unobservable Secure DNS became pass: %v", got)
	}
	if got := findObservation(t, decoded, "https.egress.ipv4"); got.GetVantage() != "external-observer:fixture:server-chrome:managed-profile" {
		t.Fatalf("observer mislabeled: %v", got)
	}
}

func TestDecodeBrowserReportRejectsFalsePassAndMixedVantages(t *testing.T) {
	for name, mutate := range map[string]func(*epb.DiagnosticReport){
		"pass":              func(report *epb.DiagnosticReport) { report.Observations[1].State = "pass" },
		"host substitution": func(report *epb.DiagnosticReport) { report.Observations[2].Vantage = "host" },
		"local noVNC":       func(report *epb.DiagnosticReport) { report.Observations[1].Vantage = "local-browser" },
		"other profile":     func(report *epb.DiagnosticReport) { report.Observations[1].Vantage = "server-chrome:another-profile" },
		"no context":        func(report *epb.DiagnosticReport) { report.Observations = report.Observations[1:] },
		"duplicate context": func(report *epb.DiagnosticReport) {
			report.Observations = append(report.Observations, report.Observations[0])
		},
		"wrong page": func(report *epb.DiagnosticReport) {
			report.Observations[0].Value = jsonText(map[string]string{"profile_id": "managed-profile", "target_url": "https://127.0.0.1/noVNC"})
		},
		"stale":              func(report *epb.DiagnosticReport) { report.ObservedAt -= 3600 },
		"future":             func(report *epb.DiagnosticReport) { report.ObservedAt += 3600 },
		"missing timestamp":  func(report *epb.DiagnosticReport) { report.Observations[1].ObservedAt = 0 },
		"oversized value":    func(report *epb.DiagnosticReport) { report.Observations[1].Value = strings.Repeat("x", 33<<10) },
		"missing limitation": func(report *epb.DiagnosticReport) { report.Observations[1].Limitation = "" },
	} {
		t.Run(name, func(t *testing.T) {
			report := browserFixture()
			mutate(report)
			if _, err := DecodeBrowserReport(encodedBrowserFixture(t, report)); err == nil {
				t.Fatal("untrusted browser evidence accepted")
			}
		})
	}
	content := string(encodedBrowserFixture(t, browserFixture()))
	content = strings.TrimSuffix(content, "}") + `,"caller_script":"alert(1)"}`
	if _, err := DecodeBrowserReport([]byte(content)); err == nil {
		t.Fatal("unknown report fields accepted")
	}
	if _, err := DecodeBrowserReport([]byte(`{}`)); err == nil {
		t.Fatal("empty browser result accepted")
	}
	if _, err := DecodeBrowserReport([]byte(`"CDP exception, not report"`)); err == nil {
		t.Fatal("CDP exception accepted as report")
	}
}

func TestConflictingGeoClaimsRemainSeparateRawValues(t *testing.T) {
	report := browserFixture()
	now := report.ObservedAt
	for _, claim := range []struct{ id, raw string }{{"one", `{"country":"JP","database":"one"}`}, {"two", `{"country":"US","database":"two"}`}} {
		report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "geo.raw", State: "observed", Value: claim.raw, Source: "provider " + claim.id, Vantage: "external-observer:" + claim.id + ":server-chrome:managed-profile", ObservedAt: now, Limitation: "Independent raw claim; no score or aggregate country verdict."})
	}
	decoded, err := DecodeBrowserReport(encodedBrowserFixture(t, report))
	if err != nil {
		t.Fatal(err)
	}
	claims := make(map[string]bool)
	for _, item := range decoded.GetObservations() {
		if item.GetCheck() == "geo.raw" {
			claims[item.GetValue()] = true
			if item.GetExpected() != "" || item.GetState() != "observed" {
				t.Fatal("raw claim scored or reconciled")
			}
		}
	}
	if len(claims) != 2 {
		t.Fatal("conflicting geo evidence collapsed")
	}
}
