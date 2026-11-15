package runtime

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"golang.org/x/text/language"
)

// Configuration is deliberately a different provenance from native Chrome.
// No socket token, profile path, cookie, keyring data, or credential is included.
func appendLaunchConfiguration(report *epb.DiagnosticReport, spec Spec) {
	at := time.Now().Unix()
	vantage := "configuration:server-chrome:" + spec.ProfileID
	region := map[string]any{"configured": spec.Region != nil}
	mode := "unchanged"
	var origins []string
	if spec.Region != nil {
		region["country"] = spec.Region.Country
		region["timezone"] = spec.Region.Timezone
		region["locale"] = spec.Region.Locale
		region["languages"] = spec.Region.Languages
		region["formats"] = spec.Region.Formats
		if spec.Region.Dns != nil {
			mode = spec.Region.Dns.ChromeMode
			if mode == "" {
				mode = "unchanged"
			}
			for _, template := range spec.Region.Dns.ChromeTemplates {
				parsed, err := url.Parse(template)
				if err == nil && parsed.Host != "" {
					origins = append(origins, parsed.Scheme+"://"+parsed.Host)
				} else {
					origins = append(origins, "invalid/unobservable template")
				}
			}
		}
	}
	configuration := map[string]any{"profile_id": spec.ProfileID, "instance_id": spec.InstanceID, "profile_revision": spec.ProfileRevision, "region_revision": spec.RegionRevision, "region": region, "initial_width": spec.Width, "initial_height": spec.Height}
	encoded, _ := json.Marshal(configuration)
	report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.configuration.launch", State: "observed", Value: string(encoded), Source: "immutable managed-runtime launch record", Vantage: vantage, ObservedAt: at, Limitation: "Configuration evidence only: a launch revision does not prove Chrome obeyed every setting, current host settings, network routing, country, identity, or service eligibility."})
	dns, _ := json.Marshal(map[string]any{"mode": mode, "template_origins": origins, "template_count": len(origins)})
	report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.secure-dns.launch", State: "observed", Value: string(dns), Source: "immutable Chrome Local State startup request", Vantage: vantage, ObservedAt: at, Limitation: "Requested configuration, not effective native Secure DNS evidence. Existing mandatory policies can override preferences. Template paths/userinfo/query/fragment are redacted; exact configured templates remain in the authorized profile-management surface. Native JavaScript Secure DNS remains unknown."})
	var intl, languages *epb.DiagnosticObservation
	for _, observation := range report.Observations {
		if observation.Vantage != "server-chrome:"+spec.ProfileID {
			continue
		}
		switch observation.Check {
		case "browser.intl":
			intl = observation
		case "browser.languages":
			languages = observation
		}
	}
	timezoneState, timezoneValue, timezoneExpected, timezoneLimit := "unknown", "", "", "No explicit launch timezone or structurally available native Intl timezone; no consistency result is inferred."
	if spec.Region != nil && spec.Region.Timezone != "" {
		timezoneExpected = spec.Region.Timezone
		if intl != nil {
			intl.Expected = timezoneExpected
			var native struct {
				DateTime struct {
					TimeZone string `json:"timeZone"`
				} `json:"date_time"`
			}
			if intl.State == "observed" && json.Unmarshal([]byte(intl.Value), &native) == nil && native.DateTime.TimeZone != "" {
				timezoneValue = native.DateTime.TimeZone
				timezoneState, timezoneLimit = compareTimezones(timezoneExpected, timezoneValue)
			}
		}
	}
	report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.consistency.timezone", State: timezoneState, Value: timezoneValue, Expected: timezoneExpected, Source: "native browser.intl compared with immutable launch timezone", Vantage: "server-chrome:" + spec.ProfileID, ObservedAt: at, Limitation: timezoneLimit})
	languageState, languageValue, languageExpected, languageLimit := "unknown", "", "", "No explicit launch language preferences or structurally available native navigator preferences; no consistency result is inferred."
	if spec.Region != nil && len(spec.Region.Languages) > 0 {
		expected, _ := json.Marshal(spec.Region.Languages)
		languageExpected = string(expected)
		if languages != nil {
			languages.Expected = languageExpected
			var native struct {
				Language  string   `json:"language"`
				Languages []string `json:"languages"`
			}
			if languages.State == "observed" && json.Unmarshal([]byte(languages.Value), &native) == nil && native.Language != "" {
				languageValue = languages.Value
				languageState, languageLimit = compareLanguages(spec.Region.Languages, native.Language, native.Languages)
			}
		}
	}
	report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.consistency.languages", State: languageState, Value: languageValue, Expected: languageExpected, Source: "native browser.languages compared with immutable launch preferences", Vantage: "server-chrome:" + spec.ProfileID, ObservedAt: at, Limitation: languageLimit})
	for _, observation := range report.Observations {
		if observation.Check == "browser.consistency.timezone" && observation.State == "warning" {
			report.Warnings = append(report.Warnings, "Observed Chrome timezone differs from the immutable launch request; existing Chrome was not changed or restarted.")
		}
		if observation.Check == "browser.consistency.languages" && observation.State == "warning" {
			report.Warnings = append(report.Warnings, "Observed Chrome language preferences contain a meaningful launch-config mismatch; no fingerprint/identity repair or browser restart was attempted.")
		}
	}
}

func compareTimezones(expected, actual string) (string, string) {
	if expected == "" || actual == "" || expected == "Local" || actual == "Local" {
		return "unknown", "Timezone identity is unavailable; process-local aliases are not an IANA consistency result."
	}
	if _, err := time.LoadLocation(expected); err != nil {
		return "unknown", "Launch timezone could not be interpreted by the server's IANA database."
	}
	if _, err := time.LoadLocation(actual); err != nil {
		return "unknown", "Native timezone could not be interpreted by the server's IANA database."
	}
	if expected == actual {
		return "observed", "Native timezone matches the requested identifier; this is not country, network, identity, or eligibility proof."
	}
	utc := func(value string) bool {
		switch value {
		case "UTC", "Etc/UTC", "Etc/UCT", "UCT", "Universal", "Zulu", "GMT", "Etc/GMT", "GMT0", "Greenwich", "Etc/Universal", "Etc/Zulu":
			return true
		}
		return false
	}
	if utc(expected) && utc(actual) {
		return "observed", "UTC/IANA aliases are equivalent; identifier spelling differences are not a mismatch or eligibility proof."
	}
	left, right := filepath.Join("/usr/share/zoneinfo", expected), filepath.Join("/usr/share/zoneinfo", actual)
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr != nil || rightErr != nil {
		return "unknown", "Native and launch timezone spellings differ, but canonical IANA alias identity is unavailable; no byte-equality mismatch is inferred."
	}
	leftPath, leftErr := filepath.EvalSymlinks(left)
	rightPath, rightErr := filepath.EvalSymlinks(right)
	if os.SameFile(leftInfo, rightInfo) || (leftErr == nil && rightErr == nil && leftPath == rightPath) {
		return "observed", "Canonical IANA aliases match; spelling differences are not treated as a mismatch."
	}
	return "warning", "Observed native timezone and requested launch timezone resolve to different IANA zone identities; equal current UTC offsets alone would not make them aliases."
}
func canonicalLanguage(value string) (string, string, bool) {
	tag, err := language.Parse(strings.ReplaceAll(value, "_", "-"))
	if err != nil || tag.String() == "und" {
		return "", "", false
	}
	base, _ := tag.Base()
	return tag.String(), base.String(), true
}
func languageCompatible(expected, actual string) (bool, bool) {
	left, leftBase, leftOK := canonicalLanguage(expected)
	right, rightBase, rightOK := canonicalLanguage(actual)
	if !leftOK || !rightOK {
		return false, false
	}
	return left == right || (leftBase == rightBase && (left == leftBase || right == rightBase)), true
}
func compareLanguages(expected []string, actual string, observed []string) (string, string) {
	if len(expected) == 0 {
		return "unknown", "Launch language preferences were not configured."
	}
	compatible, known := languageCompatible(expected[0], actual)
	if !known {
		return "unknown", "Native or configured language identifiers could not be interpreted; no mismatch or match is inferred."
	}
	if !compatible {
		return "warning", "Native primary language differs meaningfully from the configured primary language, after BCP47/gettext normalization and aliases."
	}
	for _, value := range observed {
		allowed, knownValue := false, false
		for _, requested := range expected {
			match, known := languageCompatible(requested, value)
			knownValue = knownValue || known
			if match {
				allowed = true
				break
			}
		}
		if !knownValue {
			return "unknown", "A native language identifier could not be interpreted; no byte-equality alarm is produced."
		}
		if !allowed {
			return "warning", "Native navigator preferences include a language outside the configured languages and their base/reduced aliases."
		}
	}
	return "observed", "Observable primary/preferences are compatible with the launch request. Added base-language fallbacks or missing privacy-reduced preferences are not false mismatches. HTTP Accept-Language is a separate observation and may be reduced; this is not identity or eligibility proof."
}
