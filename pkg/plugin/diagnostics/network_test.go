package diagnostics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fixtureResponse(req *http.Request, status int, content string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(content)), Request: req}
}

func TestProviderValidationRejectsSSRFAndCredentialURLs(t *testing.T) {
	for _, raw := range []string{
		"http://one.example.org/", "https://user:password@one.example.org/", "https://one.example.org/?token=secret", "https://one.example.org/#fragment",
		"https://one.example.org:8443/", "https://127.0.0.1/", "https://127.1/", "https://2130706433/", "https://0x7f000001/",
		"https://10.0.0.1/", "https://172.16.0.1/", "https://192.168.1.1/", "https://100.100.100.200/", "https://169.254.169.254/",
		"https://0.0.0.0/", "https://224.0.0.1/", "https://198.18.0.1/", "https://192.0.2.1/", "https://[::1]/", "https://[::]/",
		"https://[::ffff:127.0.0.1]/", "https://[fe80::1]/", "https://[fc00::1]/", "https://[64:ff9b::7f00:1]/", "https://[2001:db8::1]/",
		"https://[2002:7f00:1::]/", "https://[fe80::1%25eth0]/", "https://metadata.google.internal/", "https://one.local/", "https://one.example.org./",
		"https://one.example.org/\nsecret", "https://one.example.org/%00secret", "https://one.example.org/path\\secret",
	} {
		t.Run(raw, func(t *testing.T) {
			req := &epb.DiagnosticRequest{AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "fixture", Kind: "ip-echo", Url: raw}}}
			if _, err := providersFor(req); err == nil {
				t.Fatalf("unsafe URL accepted: %q", raw)
			}
		})
	}
	for _, raw := range []string{"https://one.example.org/check", "https://1.1.1.1/", "https://[2606:4700:4700::1111]/"} {
		if _, err := publicHTTPSURL(raw); err != nil {
			t.Fatalf("public URL rejected: %q: %v", raw, err)
		}
	}
}

func TestProviderProtocolValidation(t *testing.T) {
	base := func() *epb.DiagnosticRequest {
		return &epb.DiagnosticRequest{AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "fixture", Kind: "ip-echo", Url: "https://one.example.org/"}}}
	}
	for name, mutate := range map[string]func(*epb.DiagnosticRequest){
		"unknown kind":     func(req *epb.DiagnosticRequest) { req.Providers[0].Kind = "run-script" },
		"invalid id":       func(req *epb.DiagnosticRequest) { req.Providers[0].Id = "../secret" },
		"duplicate id":     func(req *epb.DiagnosticRequest) { req.Providers = append(req.Providers, req.Providers[0]) },
		"bad family":       func(req *epb.DiagnosticRequest) { req.Providers[0].IpFamily = "auto-force" },
		"unrelated zone":   func(req *epb.DiagnosticRequest) { req.Providers[0].DnsZone = "dns.example.org" },
		"missing DNS zone": func(req *epb.DiagnosticRequest) { req.Providers[0].Kind = "dns-observer" },
		"private DNS zone": func(req *epb.DiagnosticRequest) {
			req.Providers[0].Kind = "dns-observer"
			req.Providers[0].DnsZone = "metadata.internal"
		},
		"too many": func(req *epb.DiagnosticRequest) { req.Providers = make([]*epb.ProbeProvider, maxProviders+1) },
	} {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(req)
			if _, err := providersFor(req); err == nil {
				t.Fatal("invalid protocol accepted")
			}
		})
	}
}

func TestDNSRebindingAndMixedAnswersCannotReachPrivateHost(t *testing.T) {
	calls, dials := 0, 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := pinnedDial("one.example.org", "ipv4", lookup, func(_ context.Context, network, target string) (net.Conn, error) {
		dials++
		if network != "tcp4" || target != "1.1.1.1:443" {
			t.Fatalf("hostname re-resolved by dialer or wrong family: %s %s", network, target)
		}
		left, right := net.Pipe()
		right.Close()
		return left, nil
	})
	conn, err := dial(context.Background(), "tcp", "one.example.org:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if _, err := dial(context.Background(), "tcp", "one.example.org:443"); err == nil {
		t.Fatal("rebound private destination accepted")
	}
	if calls != 2 || dials != 1 {
		t.Fatalf("unexpected DNS/dial calls: %d/%d", calls, dials)
	}
	mixed := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("fd00::1")}, nil
	}
	if _, err := resolvePublic(context.Background(), "one.example.org", "ipv4", mixed); err == nil {
		t.Fatal("mixed private IPv6 answer accepted after family filtering")
	}
}

func TestAddressFamilyUnavailableIsNotSuccess(t *testing.T) {
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	if _, err := resolvePublic(context.Background(), "one.example.org", "ipv6", lookup); !errors.Is(err, errNoFamily) {
		t.Fatalf("missing IPv6 family accepted: %v", err)
	}
	for _, test := range []struct{ body, family, state string }{
		{`{"ip":"1.1.1.1"}`, "ipv6", "unknown"}, {`{"ip":"hostname.example.org"}`, "ipv4", "unknown"},
		{`{"ip":"127.0.0.1"}`, "ipv4", "warning"}, {`{"ip":"1.1.1.1"}`, "ipv4", "observed"},
		{`{"ip":"2606:4700:4700::1111"}`, "ipv6", "observed"},
	} {
		if state, _, _ := interpretIP([]byte(test.body), test.family); state != test.state {
			t.Fatalf("%s/%s -> %s, want %s", test.body, test.family, state, test.state)
		}
	}
}

func TestHostTransportRejectsRedirectsWithoutSecondRequest(t *testing.T) {
	u, _ := url.Parse("https://one.example.org/")
	client := probeClient(u, "", defaultLookup)
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || !transport.DisableKeepAlives || !transport.DisableCompression {
		t.Fatal("unsafe host transport defaults")
	}
	calls := 0
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Referer() != "" {
			t.Fatal("credential or referrer emitted")
		}
		response := fixtureResponse(req, http.StatusFound, `{}`)
		response.Header.Set("Location", "https://169.254.169.254/latest/meta-data/")
		return response, nil
	})
	if _, err := fetchJSON(context.Background(), client, http.MethodGet, u.String(), nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 {
		t.Fatalf("redirect destination contacted: %d requests", calls)
	}
}

func TestProviderResponsesAreBoundedObjects(t *testing.T) {
	for _, test := range []struct{ name, body, contentType, encoding string }{
		{"oversized", `{"value":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "application/json", ""},
		{"array", `[]`, "application/json", ""}, {"null", `null`, "application/json", ""},
		{"trailing", `{} {}`, "application/json", ""}, {"HTML", `<html>not JSON</html>`, "text/html", ""},
		{"compressed", `{}`, "application/json", "gzip"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				response := fixtureResponse(req, 200, test.body)
				response.Header.Set("Content-Type", test.contentType)
				response.Header.Set("Content-Encoding", test.encoding)
				return response, nil
			})}
			if _, err := fetchJSON(context.Background(), client, http.MethodGet, "https://one.example.org/", nil); err == nil {
				t.Fatal("unsafe provider body accepted")
			}
		})
	}
}

func TestBrowserGateAllowsOnlyExplicitVersionedEndpoints(t *testing.T) {
	req := &epb.DiagnosticRequest{AllowExternal: true, Providers: []*epb.ProbeProvider{
		{Id: "echo", Kind: "ip-echo", Url: "https://echo.example.org/ip", IpFamily: "ipv4"},
		{Id: "dns", Kind: "dns-observer", Url: "https://observer.example.org/v1", DnsZone: "dns.example.org"},
	}}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	for _, raw := range []string{"https://echo.example.org/ip", "https://observer.example.org/v1/sessions", "https://observer.example.org/v1/sessions/session_1", "https://0123456789abcdef0123456789abcdef.dns.example.org/"} {
		if err := validateBrowserProbeURL(context.Background(), req, raw, lookup); err != nil {
			t.Fatalf("protocol endpoint rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://echo.example.org/other", "https://other.example.org/ip", "https://echo.example.org/ip?secret=1", "https://observer.example.org/v1/sessions/../../admin", "https://arbitrary.dns.example.org/", "https://0123456789abcdef0123456789abcdef.dns.example.org/secret", "https://127.0.0.1/ip"} {
		if err := validateBrowserProbeURL(context.Background(), req, raw, lookup); err == nil {
			t.Fatalf("unapproved browser endpoint accepted: %s", raw)
		}
	}
	privateLookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	if err := validateBrowserProbeURL(context.Background(), req, "https://echo.example.org/ip", privateLookup); err == nil {
		t.Fatal("browser public-DNS preflight accepted link-local answer")
	}
	if err := validateBrowserProbeURL(context.Background(), &epb.DiagnosticRequest{}, "https://echo.example.org/ip", lookup); err == nil {
		t.Fatal("browser external request accepted without consent")
	}
}

func TestRemoteAddressChecksRejectNonPublicOrMalformedEvidence(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::ffff:127.0.0.1", "169.254.1.1", "100.64.0.1", "fe80::1", "fc00::1", "2001:db8::1", "not-an-ip", "]1.1.1.1["} {
		if IsPublicAddress(raw) {
			t.Fatalf("unsafe remote IP accepted: %s", raw)
		}
	}
	if !IsPublicAddress("1.1.1.1") || !IsPublicAddress("[2606:4700:4700::1111]") {
		t.Fatal("public observed remote IP rejected")
	}
}
