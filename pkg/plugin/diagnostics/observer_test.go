package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestDNSObserverRequiresRealMatchingSessionChallengeAndCallback(t *testing.T) {
	for _, mode := range []string{"fresh", "empty", "stale", "wrong-host", "wrong-session", "malformed-resolver", "no-session"} {
		t.Run(mode, func(t *testing.T) {
			providers, err := providersFor(&epb.DiagnosticRequest{AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "dns", Kind: "dns-observer", Url: "https://observer.example.org/v1", DnsZone: "dns.example.org"}}})
			if err != nil {
				t.Fatal(err)
			}
			p := providers[0]
			hostname := ""
			resolved := false
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Referer() != "" {
					t.Fatal("DNS session leaked credentials")
				}
				if req.Method == http.MethodPost {
					if mode == "no-session" {
						return fixtureResponse(req, http.StatusServiceUnavailable, `{}`), nil
					}
					if req.URL.String() != "https://observer.example.org/v1/sessions" {
						t.Fatalf("wrong session URL: %s", req.URL)
					}
					body, _ := io.ReadAll(req.Body)
					var request struct {
						Version int    `json:"version"`
						Nonce   string `json:"nonce"`
					}
					if json.Unmarshal(body, &request) != nil || request.Version != 1 || !nonceLabel.MatchString(request.Nonce) {
						t.Fatalf("session request not versioned random challenge: %s", body)
					}
					hostname = request.Nonce + ".dns.example.org"
					responseHost := hostname
					if mode == "wrong-session" {
						responseHost = "unrelated.dns.example.org"
					}
					return fixtureResponse(req, 201, jsonText(dnsSession{Version: 1, ID: "fixture_session", Hostname: responseHost})), nil
				}
				if !resolved {
					t.Fatal("observer polled without an actual challenge resolution")
				}
				if req.Method != http.MethodGet || req.URL.String() != "https://observer.example.org/v1/sessions/fixture_session" {
					t.Fatalf("wrong polling request: %s %s", req.Method, req.URL)
				}
				query := dnsQuery{Hostname: hostname, ResolverIP: "9.9.9.9", QType: "A", ObservedAt: time.Now().Unix(), ECS: "1.1.1.0/24"}
				if mode == "stale" {
					query.ObservedAt -= 600
				}
				if mode == "wrong-host" {
					query.Hostname = "cached.dns.example.org"
				}
				if mode == "malformed-resolver" {
					query.ResolverIP = "guess-a-country"
				}
				queries := []dnsQuery{query}
				if mode == "empty" {
					queries = nil
				}
				return fixtureResponse(req, 200, jsonText(dnsResult{Version: 1, ID: "fixture_session", Complete: true, Queries: queries})), nil
			})}
			lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if hostname == "" || host != hostname || network != "ip" {
					t.Fatalf("unexpected challenge: %s/%s", network, host)
				}
				resolved = true
				// NXDOMAIN still reaches an authority. A fresh matching callback is
				// evidence; successful local resolution is not fabricated or required.
				return nil, errors.New("fixture NXDOMAIN")
			}
			queries, gotHost, err := observeHostDNS(context.Background(), p, lookup, client)
			if mode == "fresh" {
				if err != nil || len(queries) != 1 || !resolved || gotHost != hostname {
					t.Fatalf("fresh callback lost: %v %v %s", queries, err, gotHost)
				}
			} else if err == nil || len(queries) != 0 {
				t.Fatalf("%s became an authoritative observation: %v %v", mode, queries, err)
			}
			if (mode == "wrong-session" || mode == "no-session") && resolved {
				t.Fatal("unverified observer session triggered DNS")
			}
		})
	}
}

func TestDNSCallbacksDoNotInferECSOrCountry(t *testing.T) {
	now := time.Now().Unix()
	queries := []dnsQuery{{Hostname: "challenge.dns.example.org", ResolverIP: "9.9.9.9", QType: "AAAA", ObservedAt: now}}
	if err := validateDNSQueries(queries, "challenge.dns.example.org", now, now); err != nil {
		t.Fatal(err)
	}
	if queries[0].ECS != "" {
		t.Fatal("ECS invented from resolver IP")
	}
	queries[0].ECS = "invented-prefix"
	if err := validateDNSQueries(queries, "challenge.dns.example.org", now, now); err == nil {
		t.Fatal("invalid ECS accepted")
	}
}

func TestObserverEndpointKeepsExplicitEscapedPath(t *testing.T) {
	providers, err := providersFor(&epb.DiagnosticRequest{AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "dns", Kind: "dns-observer", Url: "https://observer.example.org/v1%2Fzone/", DnsZone: "dns.example.org"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := observerURL(providers[0], "fixture"); got != "https://observer.example.org/v1%2Fzone/sessions/fixture" {
		t.Fatalf("changed provider path: %s", got)
	}
}
