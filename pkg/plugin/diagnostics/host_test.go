package diagnostics

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func fixtureEnvironment(files, links, variables, commands map[string]string) localEnvironment {
	return localEnvironment{
		goos: "linux",
		readFile: func(path string) ([]byte, error) {
			if value, ok := files[path]; ok {
				return []byte(value), nil
			}
			return nil, os.ErrNotExist
		},
		readlink: func(path string) (string, error) {
			if value, ok := links[path]; ok {
				return value, nil
			}
			return "", os.ErrNotExist
		},
		getenv: func(key string) string { return variables[key] },
		command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if value, ok := commands[name+" "+strings.Join(args, " ")]; ok {
				return []byte(value), nil
			}
			return nil, errors.New("fixture manager unavailable")
		},
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			panic("unexpected external DNS in a local-only diagnostic")
		},
	}
}

func findObservation(t *testing.T, report *epb.DiagnosticReport, check string) *epb.DiagnosticObservation {
	t.Helper()
	for _, item := range report.GetObservations() {
		if item.GetCheck() == check {
			return item
		}
	}
	t.Fatalf("observation %q missing", check)
	return nil
}

func TestInspectHostDefaultsToLocalUnknowns(t *testing.T) {
	env := fixtureEnvironment(nil, nil, nil, nil)
	// Supplying an endpoint is not consent, and must not even trigger DNS.
	req := &epb.DiagnosticRequest{Providers: []*epb.ProbeProvider{{Id: "not-authorized", Kind: "ip-echo", Url: "http://127.0.0.1/secret"}}}
	report, err := inspectHost(context.Background(), req, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := findObservation(t, report, "external.authorization"); got.GetValue() != "disabled" {
		t.Fatalf("external consent: %v", got)
	}
	for _, check := range []string{"dns.resolv-conf", "dns.manager.evidence", "dns.authoritative", "host.locale.effective", "host.timezone.localtime"} {
		if got := findObservation(t, report, check); got.GetState() != "unknown" || got.GetValue() != "" {
			t.Fatalf("missing evidence became a result: %v", got)
		}
	}
	for _, item := range report.GetObservations() {
		if item.GetState() == "pass" || strings.Contains(item.GetVantage(), "chrome") {
			t.Fatalf("host evidence mislabeled: %v", item)
		}
	}
}

func TestInspectHostReadsActualDNSAndLocaleEvidence(t *testing.T) {
	env := fixtureEnvironment(map[string]string{
		"/etc/resolv.conf":                 "nameserver 127.0.0.53\nsearch fixture.lan\noptions edns0 trust-ad\n",
		"/run/systemd/resolve/resolv.conf": "nameserver 9.9.9.9\n",
		"/etc/default/locale":              "LANG=ja_JP.UTF-8\nLANGUAGE=ja:en\nTOKEN=do-not-report\n",
		"/etc/timezone":                    "Asia/Tokyo\n",
		"/etc/systemd/resolved.conf":       "[Resolve]\nDNS=9.9.9.9\nDNSOverTLS=yes\n[Other]\nSecret=do-not-report\n",
	}, map[string]string{"/etc/resolv.conf": "/run/systemd/resolve/stub-resolv.conf", "/etc/localtime": "/usr/share/zoneinfo/Asia/Tokyo"},
		map[string]string{"LANG": "en_US.UTF-8", "LC_TIME": "ja_JP.UTF-8", "TOKEN": "do-not-report"},
		map[string]string{"resolvectl status --no-pager": "Global\n Current DNS Server: 9.9.9.9\n DNSOverTLS setting: yes\n", "locale ": "LANG=en_US.UTF-8\nLC_TIME=ja_JP.UTF-8\n"})
	report, err := inspectHost(context.Background(), nil, env)
	if err != nil {
		t.Fatal(err)
	}
	stub := findObservation(t, report, "dns.resolv-conf")
	if stub.GetState() != "observed" || !strings.Contains(stub.GetValue(), "127.0.0.53") || strings.Contains(stub.GetValue(), "9.9.9.9") {
		t.Fatalf("stub configuration was invented or replaced: %v", stub)
	}
	if got := findObservation(t, report, "dns.resolved.upstream-file"); !strings.Contains(got.GetValue(), "9.9.9.9") {
		t.Fatalf("upstream file missing: %v", got)
	}
	if got := findObservation(t, report, "dns.manager.evidence"); !strings.Contains(got.GetValue(), `"resolvectl_replied":true`) || !strings.Contains(got.GetValue(), `"nmcli_replied":false`) {
		t.Fatalf("manager evidence changed: %v", got)
	}
	if got := findObservation(t, report, "dns.authoritative"); got.GetState() != "unknown" || got.GetValue() != "" {
		t.Fatalf("configured resolver became an authoritative source: %v", got)
	}
	if got := findObservation(t, report, "host.locale.environment"); !strings.Contains(got.GetValue(), "en_US.UTF-8") || strings.Contains(got.GetValue(), "TOKEN") {
		t.Fatalf("process locale or environment filtering incorrect: %v", got)
	}
	for _, item := range report.GetObservations() {
		if strings.Contains(item.GetValue(), "do-not-report") {
			t.Fatalf("unrelated configuration secret emitted: %v", item)
		}
	}
}

func TestExternalAuthorizationDoesNotChooseSuppliers(t *testing.T) {
	report, err := inspectHost(context.Background(), &epb.DiagnosticRequest{AllowExternal: true}, fixtureEnvironment(nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := findObservation(t, report, "external.providers"); got.GetState() != "unknown" {
		t.Fatalf("no provider treated as a pass: %v", got)
	}
	if got := findObservation(t, report, "dns.authoritative"); got.GetState() != "unknown" {
		t.Fatalf("DNS observer invented: %v", got)
	}
}

func TestUnsupportedOSDoesNotInventLinuxConfiguration(t *testing.T) {
	env := fixtureEnvironment(nil, nil, nil, nil)
	env.goos = "unsupported-fixture"
	env.command = func(context.Context, string, ...string) ([]byte, error) { panic("Linux command on unsupported OS") }
	env.readFile = func(string) ([]byte, error) { panic("Linux file on unsupported OS") }
	report, err := inspectHost(context.Background(), nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := findObservation(t, report, "host.system.configuration"); got.GetState() != "unknown" {
		t.Fatalf("unsupported system passed: %v", got)
	}
}
