package diagnostics

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/encoding/protojson"
)

// BrowserDiagnosticURL is a fixed, network-free target in the managed server
// Chrome's existing profile context. The runtime creates and closes this target;
// no diagnostic code is evaluated in a user page or the local noVNC browser.
const BrowserDiagnosticURL = "about:blank#shikigami-diagnostics-v1"

const browserSourceName = "fixed diagnostic JavaScript in managed server Chrome"

//go:embed browser.js
var browserSource string

// BrowserScript produces only the embedded, fixed-source program plus JSON data.
// Evaluate it with CDP awaitPromise=true and returnByValue=true, only in a target
// whose URL is BrowserDiagnosticURL in the requested managed Chrome profile.
// The resolved value is a JSON string for DecodeBrowserReport. The runtime must
// gate native external requests using ValidateBrowserProbeURL and must not
// change the profile's resolver/proxy to make a probe pass. See package docs for
// the accepted native-browser DNS-rebinding limitation. No caller script, STUN
// server, cookies, credentials, report upload, or hidden provider is supported.
func BrowserScript(req *epb.DiagnosticRequest) (string, error) {
	if req == nil || !validProfileID(req.GetProfileId()) {
		return "", errors.New("browser diagnostics require an explicit managed profile id")
	}
	providers, err := providersFor(req)
	if err != nil {
		return "", err
	}
	config := struct {
		TargetURL     string     `json:"targetURL"`
		ProfileID     string     `json:"profileId"`
		AllowExternal bool       `json:"allowExternal"`
		AllowWebRTC   bool       `json:"allowWebRTC"`
		Providers     []provider `json:"providers"`
	}{BrowserDiagnosticURL, req.GetProfileId(), req.GetAllowExternal(), req.GetAllowWebrtc(), providers}
	content, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return "(" + browserSource + ")(" + string(content) + ")", nil
}

func validProfileID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return id != "." && id != ".."
}

// DecodeBrowserReport accepts a fresh result of the fixed program, not an
// arbitrary page's JSON or a CDP exception description. Bounds, provenance
// labels, timestamps and states are checked; "pass" is deliberately rejected.
// These structural checks do not authenticate a Chrome process: the runtime
// must already have verified its private CDP pipe and dedicated target identity.
func DecodeBrowserReport(content []byte) (*epb.DiagnosticReport, error) {
	if len(content) == 0 || len(content) > 256<<10 {
		return nil, errors.New("browser diagnostic report exceeds bounds or is empty")
	}
	report := &epb.DiagnosticReport{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(content, report); err != nil {
		return nil, errors.New("browser diagnostic report is not the documented typed JSON")
	}
	if len(report.GetObservations()) == 0 || len(report.GetObservations()) > 128 || len(report.GetWarnings()) > 32 {
		return nil, errors.New("browser diagnostic observation count exceeds bounds")
	}
	now := time.Now().Unix()
	if report.GetObservedAt() < now-600 || report.GetObservedAt() > now+60 {
		return nil, errors.New("browser diagnostic report is stale or has an invalid timestamp")
	}
	profile := ""
	contextCount := 0
	for _, item := range report.GetObservations() {
		if item == nil || !validCheck(item.GetCheck()) || len(item.GetValue()) > 32<<10 || len(item.GetExpected()) > 4096 || len(item.GetSource()) == 0 || len(item.GetSource()) > 1024 || len(item.GetVantage()) > 256 || len(item.GetLimitation()) == 0 || len(item.GetLimitation()) > 4096 {
			return nil, errors.New("browser diagnostic observation is malformed or exceeds bounds")
		}
		if item.GetState() != "observed" && item.GetState() != "unknown" && item.GetState() != "warning" {
			return nil, errors.New("browser evidence must be observed, unknown, or warning, never an inferred pass")
		}
		if item.GetObservedAt() < report.GetObservedAt()-5 || item.GetObservedAt() > now+60 {
			return nil, errors.New("browser observation timestamp is outside the diagnostic execution")
		}
		if item.GetCheck() == "browser.context" {
			contextCount++
			var identity struct {
				ProfileID string `json:"profile_id"`
				TargetURL string `json:"target_url"`
			}
			if item.GetState() != "observed" || item.GetSource() != browserSourceName || json.Unmarshal([]byte(item.GetValue()), &identity) != nil || !validProfileID(identity.ProfileID) || identity.TargetURL != BrowserDiagnosticURL || item.GetVantage() != "server-chrome:"+identity.ProfileID {
				return nil, errors.New("browser diagnostic target provenance is missing")
			}
			profile = identity.ProfileID
		}
	}
	if contextCount != 1 {
		return nil, errors.New("browser diagnostic report must contain exactly one dedicated target observation")
	}
	for _, item := range report.GetObservations() {
		if item.GetVantage() == "server-chrome:"+profile {
			continue
		}
		prefix, suffix := "external-observer:", ":server-chrome:"+profile
		if !strings.HasPrefix(item.GetVantage(), prefix) || !strings.HasSuffix(item.GetVantage(), suffix) {
			return nil, errors.New("browser report mixes host, local viewer, or unrelated profile vantages")
		}
		id := strings.TrimSuffix(strings.TrimPrefix(item.GetVantage(), prefix), suffix)
		if !identifier.MatchString(id) {
			return nil, errors.New("browser external observer id is invalid")
		}
	}
	for _, warning := range report.GetWarnings() {
		if len(warning) > 4096 {
			return nil, errors.New("browser diagnostic warning exceeds bounds")
		}
	}
	return report, nil
}

func validCheck(check string) bool {
	if len(check) == 0 || len(check) > 96 {
		return false
	}
	for _, char := range check {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '.' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
