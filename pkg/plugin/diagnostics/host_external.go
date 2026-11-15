package diagnostics

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func inspectExternal(ctx context.Context, report *epb.DiagnosticReport, providers []provider, lookup ipLookup) {
	hasDNS := false
	for _, p := range providers {
		report.Observations = append(report.Observations, observation("external.provider", "observed", jsonText(p), "explicit DiagnosticRequest.providers", "host", "This is the selected supplier and protocol, not an endorsement. It receives connection IP, TLS/HTTP metadata and, for DNS, a random session nonce. No credentials, report, or fingerprint is sent; no hidden provider fallback exists."))
		switch p.Kind {
		case "ip-echo":
			families := []string{p.IPFamily}
			if p.IPFamily == "" {
				families = []string{"ipv4", "ipv6"}
			}
			for _, family := range families {
				probeHostIP(ctx, report, p, family, lookup)
			}
		case "geo":
			probeHostGeo(ctx, report, p, lookup)
		case "dns-observer":
			hasDNS = true
			probeHostDNS(ctx, report, p, lookup)
		}
	}
	if !hasDNS {
		report.Observations = append(report.Observations, observation("dns.authoritative", "unknown", "", "no configured DNS observer", "host", "No DNS observer was selected. Local nameserver configuration and HTTPS egress do not reveal actual authoritative recursive source IPs or ECS."))
	}
	if len(providers) != 0 {
		report.Warnings = append(report.Warnings, "External data is provider-attributed evidence, not an account-eligibility, identity, fixed-country, reputation, or anti-ban score. Conflicting geo claims remain separate raw observations.")
	}
}

func probeHostIP(ctx context.Context, report *epb.DiagnosticReport, p provider, family string, lookup ipLookup) {
	client := probeClient(p.parsed, family, lookup)
	content, err := fetchJSON(ctx, client, http.MethodGet, p.URL, nil)
	state, limitation := "unknown", "The configured HTTPS endpoint was unreachable, blocked, timed out, or returned an invalid/oversized response; no fallback was contacted. This is host egress, not Chrome or authoritative DNS egress."
	value := ""
	if err == nil {
		state, value, limitation = interpretIP(content, family)
		limitation += " Observed through a pinned host " + family + " connection; it does not establish target Chrome routing or recursive DNS egress."
	}
	report.Observations = append(report.Observations, observation("https.egress."+family, state, value, providerSource(p), "external-observer:"+p.ID+":host", limitation))
}

func interpretIP(content []byte, family string) (string, string, string) {
	var response struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(content, &response) != nil {
		return "unknown", "", "IP echo response does not match the documented JSON object protocol."
	}
	addr, err := netip.ParseAddr(response.IP)
	if err != nil || addr.Zone() != "" {
		return "unknown", "", "IP echo response did not contain a literal IP address; no IP or country is inferred."
	}
	addr = addr.Unmap()
	value := compactJSON(content)
	if family == "ipv4" && !addr.Is4() || family == "ipv6" && !addr.Is6() {
		return "unknown", value, "Provider returned an address from the wrong requested family; the raw claim is retained, not accepted as a successful family probe."
	}
	if !publicAddress(addr) {
		return "warning", value, "Provider claims a non-public source address; this untrusted claim is not proof of public egress."
	}
	return "observed", value, "Raw provider-attributed source IP and optional request metadata; not a country, identity, or consistency pass."
}

func probeHostGeo(ctx context.Context, report *epb.DiagnosticReport, p provider, lookup ipLookup) {
	client := probeClient(p.parsed, p.IPFamily, lookup)
	content, err := fetchJSON(ctx, client, http.MethodGet, p.URL, nil)
	state, value := "unknown", ""
	limitation := "Configured geo provider unavailable, blocked, timed out, or returned an invalid/oversized JSON object; no fallback was used."
	if err == nil {
		state, value = "observed", compactJSON(content)
		limitation = "Raw provider claim only. Other databases may disagree. No combined country verdict, reputation, Claude endorsement, or risk score is calculated; a database result is not proof of residency or eligibility."
	}
	report.Observations = append(report.Observations, observation("geo.raw", state, value, providerSource(p), "external-observer:"+p.ID+":host", limitation))
}

type dnsSession struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

type dnsQuery struct {
	Hostname   string `json:"hostname"`
	ResolverIP string `json:"resolver_ip"`
	QType      string `json:"qtype"`
	ObservedAt int64  `json:"observed_at"`
	ECS        string `json:"ecs,omitempty"`
}

type dnsResult struct {
	Version  int        `json:"version"`
	ID       string     `json:"id"`
	Complete bool       `json:"complete"`
	Queries  []dnsQuery `json:"queries"`
}

func probeHostDNS(ctx context.Context, report *epb.DiagnosticReport, p provider, lookup ipLookup) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	state, value := "unknown", ""
	limitation := "No fresh, matching authoritative callback was observed. Empty/unavailable/malformed sessions are unknown, not evidence of no DNS leak. No resolver IP or ECS is inferred from local configuration."
	queries, hostname, err := observeHostDNS(ctx, p, lookup, probeClient(p.parsed, p.IPFamily, lookup))
	if err == nil {
		state = "observed"
		value = jsonText(map[string]any{"hostname": hostname, "queries": queries})
		limitation = "Actual configured observer's fresh session callbacks: resolver source and optional ECS are raw independent evidence. They are not the configured stub address, HTTPS source IP, Chrome Secure DNS, a fixed resolver country, or proof that all DNS follows this path."
	}
	report.Observations = append(report.Observations, observation("dns.authoritative", state, value, providerSource(p), "external-observer:"+p.ID+":host", limitation))
}

func observeHostDNS(ctx context.Context, p provider, lookup ipLookup, client *http.Client) ([]dnsQuery, string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	nonce := hex.EncodeToString(random[:])
	hostname := nonce + "." + p.DNSZone
	started := time.Now().Unix()
	content, err := fetchJSON(ctx, client, http.MethodPost, observerURL(p, ""), strings.NewReader(jsonText(map[string]any{"version": 1, "nonce": nonce})))
	if err != nil {
		return nil, "", err
	}
	var session dnsSession
	if strictJSON(content, &session) != nil || session.Version != 1 || !sessionIdentifier.MatchString(session.ID) || session.Hostname != hostname {
		return nil, "", errProviderResponse
	}
	// The random challenge prevents a cache hit from being mistaken for a fresh
	// authoritative observation. Resolution failure is not itself a negative
	// verdict: an observer may have seen the query before returning NXDOMAIN.
	_, _ = lookup(ctx, "ip", hostname)
	for attempt := range 3 {
		if attempt > 0 {
			timer := time.NewTimer(300 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, "", ctx.Err()
			case <-timer.C:
			}
		}
		content, err := fetchJSON(ctx, client, http.MethodGet, observerURL(p, session.ID), nil)
		if err != nil {
			return nil, "", err
		}
		var result dnsResult
		if strictJSON(content, &result) != nil || result.Version != 1 || result.ID != session.ID {
			return nil, "", errProviderResponse
		}
		if !result.Complete {
			continue
		}
		if err := validateDNSQueries(result.Queries, hostname, started, time.Now().Unix()); err != nil {
			return nil, "", err
		}
		return result.Queries, hostname, nil
	}
	return nil, "", errors.New("no completed DNS observer session")
}

func validateDNSQueries(queries []dnsQuery, hostname string, started, now int64) error {
	if len(queries) == 0 || len(queries) > 32 {
		return errProviderResponse
	}
	for _, query := range queries {
		if query.Hostname != hostname || query.ObservedAt < started-5 || query.ObservedAt > now+5 {
			return errProviderResponse
		}
		addr, err := netip.ParseAddr(query.ResolverIP)
		if err != nil || addr.Zone() != "" {
			return errProviderResponse
		}
		switch query.QType {
		case "A", "AAAA", "HTTPS", "SVCB":
		default:
			return errProviderResponse
		}
		if query.ECS != "" {
			if _, err := netip.ParsePrefix(query.ECS); err != nil {
				return errProviderResponse
			}
		}
	}
	return nil
}

func strictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func compactJSON(content []byte) string {
	var compact bytes.Buffer
	if json.Compact(&compact, content) != nil {
		return ""
	}
	return compact.String()
}
