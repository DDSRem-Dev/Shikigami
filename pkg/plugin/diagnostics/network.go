package diagnostics

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

const (
	maxProviders      = 8
	maxBodyBytes      = 16 << 10
	inspectionTimeout = 30 * time.Second
	probeTimeout      = 5 * time.Second
)

var (
	identifier           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	sessionIdentifier    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
	nonceLabel           = regexp.MustCompile(`^[0-9a-f]{32}$`)
	errUnsafeDestination = errors.New("diagnostic destination is not a permitted public HTTPS endpoint")
	errNoFamily          = errors.New("provider has no permitted address for the requested IP family")
	errProviderResponse  = errors.New("provider response is unavailable, oversized, or does not match its documented protocol")
	errRedirect          = errors.New("diagnostic redirects are disabled")
)

// Deliberately conservative: special-use, documentation, transition, multicast,
// shared-address, and non-global ranges are not external diagnostic targets.
var deniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

// IsPublicAddress validates an observed address, not a hostname or a URL. It is
// also used by the runtime for post-connect Chrome remoteIPAddress evidence.
// That check alone cannot prevent native Chrome's pre-connect DNS rebinding.
func IsPublicAddress(raw string) bool {
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	addr, err := netip.ParseAddr(raw)
	return err == nil && publicAddress(addr)
}

var publicIPv6Prefix = netip.MustParsePrefix("2000::/3")

func publicAddress(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && !publicIPv6Prefix.Contains(addr) {
		return false
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

type provider struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	DNSZone  string `json:"dnsZone,omitempty"`
	IPFamily string `json:"ipFamily,omitempty"`
	parsed   *url.URL
}

func providersFor(req *epb.DiagnosticRequest) ([]provider, error) {
	if req == nil || !req.GetAllowExternal() {
		return nil, nil
	}
	if len(req.GetProviders()) > maxProviders {
		return nil, errors.New("at most eight explicit diagnostic providers are allowed")
	}
	providers := make([]provider, 0, len(req.GetProviders()))
	seen := make(map[string]bool, len(req.GetProviders()))
	for _, input := range req.GetProviders() {
		if input == nil || !identifier.MatchString(input.GetId()) || seen[input.GetId()] {
			return nil, errors.New("diagnostic provider ids must be unique safe identifiers of at most 64 characters")
		}
		seen[input.GetId()] = true
		if input.GetKind() != "ip-echo" && input.GetKind() != "geo" && input.GetKind() != "dns-observer" {
			return nil, errors.New("diagnostic provider kind must be ip-echo, geo, or dns-observer")
		}
		u, err := publicHTTPSURL(input.GetUrl())
		if err != nil {
			return nil, err
		}
		family := input.GetIpFamily()
		if family != "" && family != "ipv4" && family != "ipv6" {
			return nil, errors.New("diagnostic ip_family must be ipv4, ipv6, or empty")
		}
		zone := input.GetDnsZone()
		if input.GetKind() == "dns-observer" {
			if len(zone) > 190 || !validDNSName(zone) {
				return nil, errors.New("dns-observer requires an explicit public authoritative dns_zone")
			}
		} else if zone != "" {
			return nil, errors.New("dns_zone is only valid for dns-observer providers")
		}
		providers = append(providers, provider{ID: input.GetId(), Kind: input.GetKind(), URL: u.String(), DNSZone: zone, IPFamily: family, parsed: u})
	}
	return providers, nil
}

func validDNSName(host string) bool {
	if host == "" || host != strings.ToLower(host) || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".invalid", ".test", ".example", ".onion"} {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	for _, char := range labels[len(labels)-1] {
		if char >= 'a' && char <= 'z' {
			return true
		}
	}
	return false
}

func publicHTTPSURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t\\") {
		return nil, errUnsafeDestination
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Port() != "" && u.Port() != "443" {
		return nil, errUnsafeDestination
	}
	if strings.ContainsAny(u.Path, "\x00\r\n\t\\") {
		return nil, errUnsafeDestination
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(addr) {
			return nil, errUnsafeDestination
		}
	} else if !validDNSName(host) {
		return nil, errUnsafeDestination
	}
	return u, nil
}

type ipLookup func(context.Context, string, string) ([]netip.Addr, error)
type connectionDial func(context.Context, string, string) (net.Conn, error)

func defaultLookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

func resolvePublic(ctx context.Context, host, family string, lookup ipLookup) ([]netip.Addr, error) {
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		var err error
		addresses, err = lookup(ctx, "ip", host)
		if err != nil {
			return nil, errUnsafeDestination
		}
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, errUnsafeDestination
	}
	selected := make([]netip.Addr, 0, len(addresses))
	for _, addr := range addresses {
		if !publicAddress(addr) {
			return nil, errUnsafeDestination
		}
		addr = addr.Unmap()
		if family == "ipv4" && !addr.Is4() || family == "ipv6" && !addr.Is6() {
			continue
		}
		selected = append(selected, addr)
	}
	if len(selected) == 0 {
		return nil, errNoFamily
	}
	return selected, nil
}

func pinnedDial(host, family string, lookup ipLookup, dial connectionDial) connectionDial {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		requestedHost, port, err := net.SplitHostPort(address)
		if err != nil || requestedHost != host || port != "443" || network != "tcp" && network != "tcp4" && network != "tcp6" {
			return nil, errUnsafeDestination
		}
		addresses, err := resolvePublic(ctx, host, family, lookup)
		if err != nil {
			return nil, err
		}
		for _, addr := range addresses {
			network := "tcp6"
			if addr.Is4() {
				network = "tcp4"
			}
			conn, err := dial(ctx, network, net.JoinHostPort(addr.String(), "443"))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("approved provider could not be reached")
	}
}

func probeClient(u *url.URL, family string, lookup ipLookup) *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         pinnedDial(u.Hostname(), family, lookup, dialer.DialContext),
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()},
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
		MaxResponseHeaderBytes: 16 << 10, DisableKeepAlives: true, DisableCompression: true,
	}
	return &http.Client{Transport: transport, Timeout: probeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect }}
}

func fetchJSON(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errUnsafeDestination
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Shikigami-Diagnostics/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return nil, errProviderResponse
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, errProviderResponse
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil || len(content) > maxBodyBytes {
		return nil, errProviderResponse
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(content, &object) != nil || object == nil {
		return nil, errProviderResponse
	}
	return content, nil
}

func observerURL(p provider, session string) string {
	endpoint := strings.TrimRight(p.URL, "/") + "/sessions"
	if session != "" {
		endpoint += "/" + session
	}
	return endpoint
}

// ValidateBrowserProbeURL is the runtime's per-request CDP interception gate.
// It permits only configured exact endpoints and the fixed DNS-session protocol.
// It performs bounded public-DNS preflight, but cannot pin native Chrome's later
// connection. The runtime must reject redirects and check observed remote IPs.
// It is not a general-purpose URL validator or an authorization substitute.
func ValidateBrowserProbeURL(ctx context.Context, req *epb.DiagnosticRequest, rawURL string) error {
	return validateBrowserProbeURL(ctx, req, rawURL, defaultLookup)
}

func validateBrowserProbeURL(ctx context.Context, req *epb.DiagnosticRequest, rawURL string, lookup ipLookup) error {
	if req == nil || !req.GetAllowExternal() {
		return errors.New("external diagnostics are not authorized")
	}
	providers, err := providersFor(req)
	if err != nil {
		return err
	}
	u, err := publicHTTPSURL(rawURL)
	if err != nil {
		return err
	}
	family := ""
	allowed := false
	for _, p := range providers {
		if p.Kind != "dns-observer" {
			if u.String() == p.URL {
				allowed, family = true, p.IPFamily
				break
			}
			continue
		}
		if u.String() == observerURL(p, "") {
			allowed, family = true, p.IPFamily
			break
		}
		prefix := observerURL(p, "") + "/"
		if strings.HasPrefix(u.String(), prefix) && sessionIdentifier.MatchString(strings.TrimPrefix(u.String(), prefix)) {
			allowed, family = true, p.IPFamily
			break
		}
		suffix := "." + p.DNSZone
		if strings.HasSuffix(u.Hostname(), suffix) && nonceLabel.MatchString(strings.TrimSuffix(u.Hostname(), suffix)) && (u.Path == "" || u.Path == "/") && u.Port() == "" {
			allowed = true
			break
		}
	}
	if !allowed {
		return errUnsafeDestination
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err = resolvePublic(ctx, u.Hostname(), family, lookup)
	return err
}

func providerSource(p provider) string {
	return fmt.Sprintf("provider %s (%s), %s", p.ID, p.Kind, p.URL)
}
