package runtime

import (
	"strings"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestDiagnosticConfigurationIsNotNativeSecureDNSProof(t *testing.T) {
	now := time.Now().Unix()
	nativeDNS := &epb.DiagnosticObservation{Check: "browser.secure-dns.native", State: "unknown", Vantage: "server-chrome:one", ObservedAt: now, Limitation: "Native setting not observable."}
	report := &epb.DiagnosticReport{ObservedAt: now, Observations: []*epb.DiagnosticObservation{nativeDNS, {Check: "browser.languages", State: "observed", Value: `{"language":"ja-JP","languages":["ja-JP"]}`, Vantage: "server-chrome:one"}, {Check: "browser.intl", State: "observed", Value: `{"date_time":{"timeZone":"UTC"}}`, Vantage: "server-chrome:one"}}}
	spec := Spec{ProfileID: "one", ProfileRevision: "profile-rev", InstanceID: "instance", RegionRevision: "region-rev", Token: "DO-NOT-REPORT", ProfileDir: "/private/COOKIE-PROFILE", Region: &epb.RegionConfig{Timezone: "Etc/UTC", Languages: []string{"ja_JP", "ja"}, Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{"https://user:SECRET@resolver.example/private-profile?token=SECRET#private"}}}}
	appendLaunchConfiguration(report, spec)
	if nativeDNS.State != "unknown" {
		t.Fatal("configured Secure DNS was mislabeled as effective native proof")
	}
	configurationSeen := false
	for _, observation := range report.Observations {
		if strings.Contains(observation.Value, "SECRET") || strings.Contains(observation.Value, "COOKIE-PROFILE") || strings.Contains(observation.Value, "DO-NOT-REPORT") || strings.Contains(observation.Value, "private-profile") {
			t.Fatal("diagnostics included credential/profile secrets")
		}
		if observation.Check == "browser.configuration.launch" {
			configurationSeen = true
			if !strings.HasPrefix(observation.Vantage, "configuration:") || observation.State != "observed" {
				t.Fatal("configuration masqueraded as native Chrome")
			}
		}
		if observation.Check == "browser.consistency.languages" && observation.State != "observed" {
			t.Fatalf("base-language/privacy reduction caused a false alarm: %#v", observation)
		}
		if observation.Check == "browser.consistency.timezone" && observation.State != "observed" {
			t.Fatal("UTC alias caused a false mismatch")
		}
		if observation.State == "pass" {
			t.Fatal("diagnostic invented a pass")
		}
	}
	if !configurationSeen {
		t.Fatal("immutable launch record evidence missing")
	}
}
func TestLanguageMismatchAndUnknownStayDistinct(t *testing.T) {
	if state, _ := compareLanguages([]string{"en_US", "en"}, "ja-JP", []string{"ja-JP"}); state != "warning" {
		t.Fatal("meaningful language mismatch not reported")
	}
	if state, _ := compareLanguages([]string{"en_US", "en"}, "en", []string{"en"}); state != "observed" {
		t.Fatal("language privacy reduction treated as mismatch")
	}
	if state, _ := compareLanguages([]string{"iw_IL"}, "he-IL", []string{"he-IL", "he"}); state != "observed" {
		t.Fatal("canonical language alias treated as mismatch")
	}
	if state, _ := compareLanguages([]string{"en_US"}, "%%%", nil); state != "unknown" {
		t.Fatal("unparseable native evidence was guessed")
	}
	if state, _ := compareTimezones("UTC", "Not/A_Real_Zone"); state != "unknown" {
		t.Fatal("unknown native timezone became a match/pass")
	}
	report := &epb.DiagnosticReport{Observations: []*epb.DiagnosticObservation{{Check: "browser.intl", State: "unknown", Value: `{}`, Vantage: "server-chrome:one"}}}
	appendLaunchConfiguration(report, Spec{ProfileID: "one", Region: &epb.RegionConfig{Timezone: "UTC"}})
	for _, observation := range report.Observations {
		if observation.Check == "browser.consistency.timezone" && observation.State != "unknown" {
			t.Fatal("unknown native evidence promoted to observed match")
		}
	}
}
