package helper

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestNormalizePresetsAndExplicitUS(t *testing.T) {
	jp, err := NormalizeConfig(&epb.RegionConfig{Country: "jp"})
	if err != nil {
		t.Fatal(err)
	}
	if jp.Country != "JP" || jp.Timezone != "Asia/Tokyo" || jp.Locale != "ja_JP.UTF-8" || strings.Join(jp.Languages, ":") != "ja_JP:ja" {
		t.Fatalf("unexpected JP preset: %v", jp)
	}
	for _, input := range []*epb.RegionConfig{
		{Country: "US"}, {Country: "US", Timezone: "America/New_York"},
		{Country: "US", Timezone: "America/New_York", Locale: "en_US.UTF-8"},
	} {
		if _, err := NormalizeConfig(input); err == nil {
			t.Fatalf("US acquired implicit timezone/locale/language: %v", input)
		}
	}
	us, err := NormalizeConfig(&epb.RegionConfig{Country: "US", Timezone: "America/Chicago", Locale: "en_US.UTF8", Languages: []string{"en_US", "en"}, Formats: map[string]string{"LC_TIME": "ja_JP.UTF-8"}})
	if err != nil || us.Locale != "en_US.UTF-8" {
		t.Fatalf("explicit US: %v %v", us, err)
	}
}

func TestRejectUnsafeRegionInputs(t *testing.T) {
	cases := []*epb.RegionConfig{
		{Country: "CN"}, {Country: "JP", Timezone: "../../etc/passwd"}, {Country: "JP", Timezone: "Not/A_Zone"},
		{Country: "JP", Locale: "ja_JP.SJIS"}, {Country: "JP", Languages: []string{"ja;id"}},
		{Country: "JP", Formats: map[string]string{"LC_ALL": "ja_JP.UTF-8"}},
		{Country: "JP", Formats: map[string]string{"PATH": "ja_JP.UTF-8"}},
		{Country: "JP", Formats: map[string]string{"LC_TIME": "C"}},
		{Country: "JP", Dns: &epb.DNSConfig{ManageSystem: true, Servers: []string{"1.1.1.1"}}},
		{Country: "JP", Dns: &epb.DNSConfig{Servers: []string{"1.1.1.1"}}},
		{Country: "JP", Dns: &epb.DNSConfig{ManageSystem: true, Servers: []string{"0.0.0.0"}, CheckDomains: []string{"example.com"}}},
		{Country: "JP", Dns: &epb.DNSConfig{ManageSystem: true, Servers: []string{"1.1.1.1"}, CheckDomains: []string{"-bad.example"}}},
		{Country: "JP", Dns: &epb.DNSConfig{ChromeMode: "secure"}},
		{Country: "JP", Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{"https://user:secret@dns.example/dns-query"}}},
		{Country: "JP", Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{"http://dns.example/dns-query"}}},
		{Country: "JP", Dns: &epb.DNSConfig{ChromeMode: "system", ChromeTemplates: []string{"https://dns.example/dns-query"}}},
	}
	for index, input := range cases {
		if _, err := NormalizeConfig(input); err == nil {
			t.Errorf("case %d unexpectedly accepted: %v", index, input)
		}
	}
	config, err := NormalizeConfig(&epb.RegionConfig{Country: "JP", Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{"https://dns.example/dns-query{?dns}"}}})
	if err != nil || config.Dns.ChromeTemplates[0] != "https://dns.example/dns-query{?dns}" {
		t.Fatalf("explicit DoH template rejected: %v", err)
	}
}

func TestHelperProtocolIsBoundedAndClosed(t *testing.T) {
	for _, input := range []string{
		`{"command":"/bin/sh"}`, `{"root":"/tmp"}`, `{"uid":0}`, `{} {}`,
		`{"config":{"country":"JP","arbitrary_file":"/etc/shadow"}}`, `null`,
		strings.Repeat(" ", MaxRequestBytes+1),
	} {
		request, err := DecodeRequest(strings.NewReader(input))
		if input == "null" {
			if err == nil && validateActionRequest("plan", request) == nil {
				t.Fatal("null request became a plan")
			}
			continue
		}
		if err == nil {
			t.Errorf("unsafe JSON accepted: %.120s", input)
		}
	}
	if _, err := DecodeRequest(strings.NewReader(`{"request_id":"safe-request-1","confirm":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := validateActionRequest("guard", Request{}); err == nil {
		t.Fatal("guard exposed through public request dispatch")
	}
	if err := validateActionRequest("inspect", Request{Confirm: true}); err == nil {
		t.Fatal("inspect accepted mutation fields")
	}
	var output bytes.Buffer
	if err := EncodeResponse(&output, Response{Error: strings.Repeat("x", MaxResponseBytes+1)}); err == nil || output.Len() != 0 {
		t.Fatal("oversized response was written")
	}
	encoded, _ := json.Marshal(Request{Config: &epb.RegionConfig{Country: "JP"}})
	request, err := DecodeRequest(bytes.NewReader(encoded))
	if err != nil || request.Config.Country != "JP" {
		t.Fatalf("typed helper round-trip: %v", err)
	}
}

func TestExplicitLocalAndCloudResolversAreNotCountrySelected(t *testing.T) {
	for _, server := range []string{"127.0.0.1", "::1", "169.254.169.253", "10.0.0.53"} {
		config, err := NormalizeConfig(&epb.RegionConfig{Country: "JP", Dns: &epb.DNSConfig{ManageSystem: true, Servers: []string{server}, CheckDomains: []string{"example.com"}}})
		if err != nil || config.Dns.Servers[0] != server {
			t.Fatalf("explicit local/cloud resolver was arbitrarily replaced or rejected: %s %v", server, err)
		}
	}
}
