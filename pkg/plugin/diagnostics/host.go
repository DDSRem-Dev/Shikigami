package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type localEnvironment struct {
	goos     string
	readFile func(string) ([]byte, error)
	readlink func(string) (string, error)
	getenv   func(string) string
	command  func(context.Context, string, ...string) ([]byte, error)
	lookup   ipLookup
}

func realEnvironment() localEnvironment {
	return localEnvironment{goos: runtime.GOOS, readFile: readLocalFile, readlink: os.Readlink, getenv: os.Getenv, command: readCommand, lookup: defaultLookup}
}

// InspectHost reports the plugin process's real environment and OS configuration
// separately from effective running Chrome state. It does not modify files,
// routes, DNS, locale, time, or existing processes. No external DNS or HTTP probe
// is initiated without allow_external and an explicitly configured provider.
func InspectHost(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	return inspectHost(ctx, req, realEnvironment())
}

func inspectHost(ctx context.Context, req *epb.DiagnosticRequest, env localEnvironment) (*epb.DiagnosticReport, error) {
	providers, err := providersFor(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, inspectionTimeout)
	defer cancel()
	report := &epb.DiagnosticReport{ObservedAt: time.Now().Unix()}
	add := func(check, state, value, source, limitation string) {
		report.Observations = append(report.Observations, observation(check, state, value, source, "host", limitation))
	}
	add("host.platform", "observed", env.goos+"/"+runtime.GOARCH, "plugin process Go runtime", "Host process evidence, not the target Chrome executable, client machine, or noVNC browser.")
	now := time.Now()
	zone, offset := now.Zone()
	add("host.timezone.process", "observed", jsonText(map[string]any{"location": time.Local.String(), "zone": zone, "offset_seconds": offset, "TZ": env.getenv("TZ")}), "plugin process time.Local and TZ", "Process-start environment can differ from current system configuration and existing applications; this does not change or validate NTP.")
	locale := make(map[string]string)
	for _, key := range []string{"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES", "LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION"} {
		if value := env.getenv(key); value != "" {
			locale[key] = value
		}
	}
	add("host.locale.environment", "observed", jsonText(locale), "allowlisted plugin process environment variables", "Only actual set values are shown. An empty object is not evidence of a usable UTF-8 locale or running application language.")
	if env.goos != "linux" {
		add("host.system.configuration", "unknown", "", "OS platform", "System configuration inspection supports Linux; process facts do not substitute for system DNS or locale configuration.")
	} else {
		inspectLinux(ctx, env, add)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req == nil || !req.GetAllowExternal() {
		add("external.authorization", "observed", "disabled", "DiagnosticRequest.allow_external", "No external HTTP, authoritative DNS observer, geo provider, STUN, or fallback service was contacted.")
		add("dns.authoritative", "unknown", "", "no authorized DNS observer", "Configured nameservers do not reveal an authoritative observer's actual recursive source IP, ECS, or geographic location.")
		return report, nil
	}
	add("external.authorization", "observed", "explicitly enabled", "DiagnosticRequest.allow_external and providers", "Only the listed endpoints are used. They receive connection IP, TLS and HTTP request metadata; the DNS observer additionally receives a random challenge nonce. No account credentials, cookies, tokens, report, or combined fingerprint is sent.")
	if len(providers) == 0 {
		add("external.providers", "unknown", "", "explicit provider list", "No providers configured; enabling external diagnostics does not select a public service or trigger a fallback.")
	}
	inspectExternal(ctx, report, providers, env.lookup)
	return report, nil
}

type localObservation func(string, string, string, string, string)

func inspectLinux(ctx context.Context, env localEnvironment, add localObservation) {
	read := func(check, path, limitation string, parse func([]byte) string) {
		content, err := env.readFile(path)
		if err != nil || len(content) > maxBodyBytes {
			add(check, "unknown", "", path, "File is missing, unreadable, or exceeds the bounded read; no default is assumed. "+limitation)
			return
		}
		value := strings.TrimSpace(string(content))
		if parse != nil {
			value = parse(content)
		}
		add(check, "observed", value, path, limitation)
	}
	command := func(check, executable, limitation string, args ...string) bool {
		if ctx.Err() != nil {
			return false
		}
		output, err := env.command(ctx, executable, args...)
		if err != nil || len(output) > maxBodyBytes || len(strings.TrimSpace(string(output))) == 0 {
			add(check, "unknown", "", executable+" "+strings.Join(args, " "), "Manager or command unavailable, failed, empty, or timed out; no successful configuration is inferred. "+limitation)
			return false
		}
		add(check, "observed", strings.TrimSpace(string(output)), executable+" "+strings.Join(args, " "), limitation)
		return true
	}
	read("host.timezone.file", "/etc/timezone", "Configuration file, not proof that an already-running process adopted it.", nil)
	if target, err := env.readlink("/etc/localtime"); err == nil {
		add("host.timezone.localtime", "observed", target, "readlink /etc/localtime", "Symlink target is actual configuration, not a synthesized timezone name.")
	} else {
		add("host.timezone.localtime", "unknown", "", "readlink /etc/localtime", "A regular or unavailable localtime file is not mapped to a guessed IANA timezone.")
	}
	command("host.timezone.manager", "timedatectl", "Manager report is observed independently from process time.Local.", "show", "--property=Timezone", "--property=LocalRTC", "--property=NTPSynchronized")
	for _, path := range []string{"/etc/default/locale", "/etc/locale.conf"} {
		read("host.locale.config", path, "Only locale assignment keys are reported; configuration need not have been adopted by existing processes.", func(content []byte) string { return jsonText(localeAssignments(content)) })
	}
	command("host.locale.effective", "locale", "Effective locale of a child inheriting this plugin's current environment; this is not target Chrome language.")
	command("host.locale.installed", "locale", "Installed locale listing, not proof of account region or application selection.", "-a")
	link, linkErr := env.readlink("/etc/resolv.conf")
	if linkErr == nil {
		add("dns.resolv-conf.link", "observed", link, "readlink /etc/resolv.conf", "An actual manager-owned path is evidence, not proof the corresponding service is active.")
	} else {
		add("dns.resolv-conf.link", "unknown", "", "readlink /etc/resolv.conf", "No symlink target available; do not guess the DNS manager from configured IPs.")
	}
	read("dns.resolv-conf", "/etc/resolv.conf", "Actual configured stub/upstream addresses, search list, and options. A loopback stub is not the recursive resolver's public egress IP.", resolvConf)
	read("dns.resolved.upstream-file", "/run/systemd/resolve/resolv.conf", "Manager-generated file may exist without being used by this process; active-manager output is separate.", resolvConf)
	read("dns.resolved.base-config", "/etc/systemd/resolved.conf", "Base file only; drop-ins and per-link effective settings are represented by manager output when available.", func(content []byte) string {
		return jsonText(iniSettings(content, "Resolve", map[string]bool{"DNS": true, "FallbackDNS": true, "Domains": true, "DNSSEC": true, "DNSOverTLS": true, "LLMNR": true, "MulticastDNS": true}))
	})
	read("dns.networkmanager.base-config", "/etc/NetworkManager/NetworkManager.conf", "Base manager-selection keys only, not connection secrets or a claim that NetworkManager is active.", func(content []byte) string {
		return jsonText(iniSettings(content, "main", map[string]bool{"dns": true, "rc-manager": true, "systemd-resolved": true}))
	})
	resolved := command("dns.resolved.effective", "resolvectl", "Actual manager reply, including per-link DNS, transport, DNSSEC, and routing domains; not authoritative recursive egress or fixed country.", "status", "--no-pager")
	networkManager := command("dns.networkmanager.effective", "nmcli", "Actual device DNS reply, not an assumption that a public resolver's registration country is its egress country.", "--terse", "--fields", "GENERAL.DEVICE,GENERAL.STATE,IP4.DNS,IP6.DNS,IP4.DOMAIN,IP6.DOMAIN", "device", "show")
	if resolved || networkManager || linkErr == nil {
		add("dns.manager.evidence", "observed", jsonText(map[string]any{"resolv_conf_link": link, "resolvectl_replied": resolved, "nmcli_replied": networkManager}), "actual symlink and bounded manager queries", "Multiple managers can coexist. This evidence is not an invented single-manager identity, routing test, DNS-leak verdict, or IP geolocation.")
	} else {
		add("dns.manager.evidence", "unknown", "", "actual symlink and bounded manager queries", "No manager identity could be established; resolv.conf addresses alone do not identify the active manager.")
	}
}

func localeAssignments(content []byte) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES", "LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION":
			result[key] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return result
}

func iniSettings(content []byte, section string, allowed map[string]bool) map[string]string {
	result := make(map[string]string)
	current := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = line[1 : len(line)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if key = strings.TrimSpace(key); ok && current == section && allowed[key] {
			result[key] = strings.TrimSpace(value)
		}
	}
	return result
}

func resolvConf(content []byte) string {
	result := map[string][]string{"nameservers": {}, "search": {}, "options": {}, "invalid_nameserver_entries": {}}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.SplitN(strings.SplitN(line, "#", 2)[0], ";", 2)[0]
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			if _, err := netip.ParseAddr(fields[1]); err == nil {
				result["nameservers"] = append(result["nameservers"], fields[1])
			} else {
				result["invalid_nameserver_entries"] = append(result["invalid_nameserver_entries"], fields[1])
			}
		case "search", "domain":
			result["search"] = append(result["search"], fields[1:]...)
		case "options":
			result["options"] = append(result["options"], fields[1:]...)
		}
	}
	return jsonText(result)
}

func readLocalFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBodyBytes+1))
	if err != nil || len(content) > maxBodyBytes {
		return nil, errors.New("diagnostic file exceeds bounded read")
	}
	return content, nil
}

type boundedOutput struct{ bytes []byte }

func (w *boundedOutput) Write(content []byte) (int, error) {
	if len(w.bytes)+len(content) > maxBodyBytes {
		return 0, errors.New("diagnostic command exceeds output limit")
	}
	w.bytes = append(w.bytes, content...)
	return len(content), nil
}

func readCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	output := &boundedOutput{}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.bytes, nil
}

func observation(check, state, value, source, vantage, limitation string) *epb.DiagnosticObservation {
	return &epb.DiagnosticObservation{Check: check, State: state, Value: value, Source: source, Vantage: vantage, ObservedAt: time.Now().Unix(), Limitation: limitation}
}

func jsonText(value any) string {
	content, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(content)
}
