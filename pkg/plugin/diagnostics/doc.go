// Package diagnostics collects evidence, not a region-consistency or account-risk
// score. A successful observation is "observed", never an inferred "pass".
// Unknown evidence remains unknown. Host, dedicated server Chrome, and external
// observers are separate vantages; a host HTTPS request is not a Chrome request.
//
// External requests are disabled unless DiagnosticRequest.allow_external is true.
// There are no built-in providers or third-party fallbacks. Each explicit
// ProbeProvider has a unique id, kind (ip-echo, geo, or dns-observer), and an HTTPS
// URL without userinfo, query, fragment, or non-443 port. Providers must not need
// credentials. ip_family, when present, is ipv4 or ipv6. Host connections resolve
// DNS for every connection, reject every non-public answer, and pin a validated
// IP to that connection. Host redirects and environment proxies are disabled.
// Each operation is bounded by a 5-second timeout, a 16 KiB response limit, and
// an overall 30-second inspection lifetime. At most eight providers are allowed.
//
// Provider protocol version 1:
//
//   - ip-echo: GET the exact URL; return application/json {"ip":"203.0.113.1"}.
//     The example is documentation-only, not an address used by this package.
//     Optional request metadata is {"request":{"user_agent":"...",
//     "accept_language":"..."}}. It is raw observer evidence, not a browser
//     header inferred from a host request. Host inspection probes both families
//     when ip_family is empty. Chrome never changes its native network path to
//     force a family: use explicitly configured family-specific HTTPS endpoints.
//     Its returned address must match ip_family when specified.
//   - geo: GET the exact URL; return an application/json object. The bounded raw
//     object is reported verbatim, independently for each provider; disagreeing
//     data is never converted into a country verdict, reputation, or risk score.
//   - dns-observer: dns_zone is an explicitly configured public authoritative
//     zone. POST URL + "/sessions" with application/json
//     {"version":1,"nonce":"<32 lowercase hex characters>"}. Return
//     {"version":1,"id":"<1-96 safe identifier characters>",
//     "hostname":"<nonce>.<dns_zone>"}. The client actually resolves that exact
//     name (host) or attempts a native Chrome HTTPS fetch of it, then
//     polls GET URL + "/sessions/" + id up to three times. Return
//     {"version":1,"id":"...","complete":true,"queries":[
//     {"hostname":"<nonce>.<dns_zone>","resolver_ip":"...","qtype":"A",
//     "observed_at":<Unix seconds>,"ecs":"optional raw ECS prefix"}]}. Only
//     fresh, matching callbacks count as observed. Empty, stale, mismatched,
//     malformed, or unavailable sessions are unknown, not a DNS-leak pass.
//     ecs is optional evidence, not a fabricated client address. dns_zone must
//     be empty for the other provider kinds. Providers must support CORS for
//     an opaque dedicated Chrome page, including OPTIONS for the session POST.
//
// Privacy: approved services receive the connection IP, TLS and ordinary HTTP
// request metadata, and the DNS observer receives a random session nonce and
// authoritative queries. No account identifiers, cookies, authorization headers,
// profile contents, canvas/WebGL output, report, or combined fingerprint is sent.
// Browser fetches omit credentials and referrers. Native browser facts remain in
// the report. Optional WebRTC gathers candidates without STUN/TURN or a peer;
// lack of a candidate does not prove absence of a leak.
//
// BrowserScript MUST run only in BrowserDiagnosticURL in the managed server
// Chrome process and original profile context. The runtime must create and close
// a dedicated diagnostic target, gate requests with ValidateBrowserProbeURL,
// block redirects, and inspect observed remote IPs when available. It must not
// evaluate caller code or run this script in the local noVNC browser, nor change
// the resolver, proxy, credentials, or profile preferences to obtain a result.
// Native Chrome has no pre-connect address-pinning API: public-DNS preflight and
// post-connect IP checks cannot guarantee prevention of DNS rebinding. Browser
// hostname providers therefore require explicit trust; this accepted limitation
// is reported, never represented as a security pass. Missing remote-IP evidence
// is unknown. A host HTTPS result is never substituted for a Chrome observation.
package diagnostics
