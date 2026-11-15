//go:build linux

package helper

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"golang.org/x/sys/unix"
)

// SystemBackend is intentionally fixed to distribution-owned locations. Tests
// inject a different Backend; there is no production filesystem-root option.
type SystemBackend struct{}

var systemPaths = map[string]string{
	"localtime": "/etc/localtime", "timezone": "/etc/timezone", "locale": "/etc/default/locale",
	"resolver": "/etc/resolv.conf", "resolved": "/etc/systemd/resolved.conf.d/90-shikigami-region.conf",
	"locale-canonical": "/etc/locale.conf",
	"resolvconf-owner": StatePath + "/resolvconf-owner",
	"resolvconf-order": "/etc/resolvconf/interface-order",
	"resolvconf-head":  "/etc/resolvconf/resolv.conf.d/head", "resolvconf-tail": "/etc/resolvconf/resolv.conf.d/tail",
}

const managedHeader = "# Managed by Shikigami region; administrator-approved fixed-purpose helper\n"
const resolverRecord = "shikigami.region"

func fixedPath(key string) (string, error) {
	path, ok := systemPaths[key]
	if !ok {
		return "", fail("invalid_argument", "unknown fixed region setting")
	}
	return path, nil
}

func readState(path string) (FileState, error) {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A missing known directory has no file. Its nearest existing
			// ancestor is still checked before a subsequent creation.
			parent := filepath.Dir(path)
			for {
				if _, statErr := os.Lstat(parent); statErr == nil {
					return FileState{Kind: "absent"}, trustedDirectory(parent)
				} else if !errors.Is(statErr, os.ErrNotExist) {
					return FileState{}, statErr
				}
				parent = filepath.Dir(parent)
			}
		}
		return FileState{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileState{Kind: "absent"}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return FileState{Kind: "symlink", Target: target}, err
	}
	if !info.Mode().IsRegular() {
		return FileState{}, errors.New("region setting is not a regular file or owned symlink")
	}
	data, err := readNoFollow(path, 1<<20, true)
	return FileState{Kind: "file", Data: data, Mode: uint32(info.Mode().Perm())}, err
}

func readTrustedResolved(path string, maximum int) ([]byte, error) {
	resolved, err := trustedPath(path)
	if err != nil {
		return nil, err
	}
	return readNoFollow(resolved, maximum, true)
}

func distribution() (string, error) {
	data, err := readTrustedResolved("/etc/os-release", 16384)
	if err != nil {
		return "", err
	}
	values := parseAssignments(string(data))
	switch {
	case values["ID"] == "ubuntu" && values["VERSION_ID"] == "24.04":
		return "ubuntu", nil
	case values["ID"] == "debian" && values["VERSION_ID"] == "12":
		return "debian", nil
	default:
		return "unsupported", nil
	}
}

func parseAssignments(data string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.ContainsAny(key, " \t$`\\") {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		// Never evaluate shell syntax from any root-owned configuration file.
		if strings.ContainsAny(value, "$`\\\r\n") {
			continue
		}
		values[key] = value
	}
	return values
}

func nameservers(data []byte) []string {
	var servers []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if address, err := netip.ParseAddr(fields[1]); err == nil && !seen[address.String()] {
				servers = append(servers, address.String())
				seen[address.String()] = true
			}
		}
	}
	return servers
}

func resolverManager(ctx context.Context) (string, error) {
	distro, err := distribution()
	if err != nil {
		return "", err
	}
	state, err := readState("/etc/resolv.conf")
	if err != nil {
		return "", err
	}
	if state.Kind == "symlink" {
		target := state.Target
		if !filepath.IsAbs(target) {
			target = filepath.Join("/etc", target)
		}
		target = filepath.Clean(target)
		if (distro == "ubuntu" || distro == "debian") && (target == "/run/systemd/resolve/stub-resolv.conf" || target == "/run/systemd/resolve/resolv.conf") {
			if _, err := runFixed(ctx, "/usr/bin/systemctl", nil, "is-active", "--quiet", "systemd-resolved.service"); err == nil {
				return "systemd-resolved", nil
			}
			return "systemd-resolved-inactive", nil
		}
		if distro == "debian" && target == "/run/resolvconf/resolv.conf" {
			version, err := runFixed(ctx, "/usr/sbin/resolvconf", nil, "--version")
			if err != nil {
				return "resolvconf-unavailable", nil
			}
			if strings.Contains(strings.ToLower(string(version)), "openresolv") {
				return "openresolv", nil
			}
			return "resolvconf", nil
		}
		return "unsupported-symlink-owner", nil
	}
	if distro == "debian" && state.Kind == "file" {
		lower := strings.ToLower(string(state.Data))
		for _, marker := range []string{"networkmanager", "dhclient", "dhcpcd", "generated by", "do not edit", "systemd-resolved", "resolvconf"} {
			if strings.Contains(lower, marker) && !bytes.HasPrefix(state.Data, []byte(managedHeader)) {
				return "externally-managed-regular-file", nil
			}
		}
		if _, err := runFixed(ctx, "/usr/bin/systemctl", nil, "is-active", "--quiet", "NetworkManager.service"); err == nil {
			return "NetworkManager-unsupported", nil
		}
		return "debian-static", nil
	}
	return "unsupported", nil
}

func (SystemBackend) Read(ctx context.Context, key string) (FileState, error) {
	if key == "resolver-peers" {
		native, err := openresolvState()
		return native.Peers, err
	}
	if key == "resolved-peers" {
		dns, err := runFixed(ctx, "/usr/bin/resolvectl", nil, "dns")
		if err != nil {
			return FileState{}, err
		}
		domains, err := runFixed(ctx, "/usr/bin/resolvectl", nil, "domain")
		if err != nil {
			return FileState{}, err
		}
		return resolvedLinkEvidence(dns, domains), nil
	}
	if key == "resolvconf-record" {
		manager, err := resolverManager(ctx)
		if err != nil {
			return FileState{}, err
		}
		path := "/run/resolvconf/interface/" + resolverRecord
		if manager == "openresolv" {
			path = "/run/resolvconf/interfaces/" + resolverRecord
		}
		state, err := readState(path)
		if state.Kind == "file" {
			state.Mode = 0
		}
		if state.Kind == "file" && manager == "openresolv" {
			native, nativeErr := openresolvState()
			if nativeErr != nil {
				return FileState{}, nativeErr
			}
			state.Target = native.Metadata
		}
		return state, err
	}
	path, err := fixedPath(key)
	if err != nil {
		return FileState{}, err
	}
	return readState(path)
}

func (b SystemBackend) Inspect(ctx context.Context) (Observation, error) {
	observation := Observation{Actual: &epb.RegionConfig{Formats: make(map[string]string), Dns: &epb.DNSConfig{ChromeMode: "unchanged"}}}
	distro, err := distribution()
	if err != nil {
		return observation, err
	}
	if distro == "unsupported" {
		observation.Warnings = append(observation.Warnings, "Supported region hosts are Ubuntu 24.04 and Debian 12; this distribution is read-only")
	}
	states := make(map[string]FileState)
	for _, key := range []string{"localtime", "timezone", "locale", "locale-canonical", "resolver", "resolved", "resolvconf-order", "resolvconf-head", "resolvconf-tail"} {
		state, err := b.Read(ctx, key)
		if err != nil {
			return observation, err
		}
		states[key] = state
	}
	if os.Geteuid() == 0 {
		provenance, err := b.Read(ctx, "resolvconf-owner")
		if err != nil {
			return observation, err
		}
		states["resolvconf-owner"] = provenance
	}
	localtime := states["localtime"]
	if localtime.Kind == "symlink" {
		target := localtime.Target
		if !filepath.IsAbs(target) {
			target = filepath.Join("/etc", target)
		}
		target = filepath.Clean(target)
		if strings.HasPrefix(target, "/usr/share/zoneinfo/") {
			zone := strings.TrimPrefix(target, "/usr/share/zoneinfo/")
			if data, err := validatedZoneData(zone); err == nil {
				observation.Actual.Timezone = zone
				states["active-zone-data"] = FileState{Kind: "file", Data: data}
			}
		}
	} else if localtime.Kind == "file" && states["timezone"].Kind == "file" {
		zone := strings.TrimSpace(string(states["timezone"].Data))
		if zoneName.MatchString(zone) && !strings.Contains(zone, "..") {
			if data, err := validatedZoneData(zone); err == nil && bytes.Equal(data, localtime.Data) {
				observation.Actual.Timezone = zone
				states["active-zone-data"] = FileState{Kind: "file", Data: data}
			}
		}
	}
	if observation.Actual.Timezone == "" {
		observation.Warnings = append(observation.Warnings, "Actual system IANA timezone could not be verified from /etc/localtime; no timezone is guessed")
	}
	_, locale, localeErr := selectLocaleState(states["locale"], states["locale-canonical"])
	if localeErr != nil {
		observation.Warnings = append(observation.Warnings, localeErr.Error())
	}
	values := parseAssignments(string(locale.Data))
	observation.Actual.Locale = values["LANG"]
	if language := values["LANGUAGE"]; language != "" {
		observation.Actual.Languages = strings.Split(language, ":")
	}
	for _, key := range LocaleCategories {
		if value := values[key]; value != "" {
			observation.Actual.Formats[key] = value
		}
	}
	if values["LC_ALL"] != "" {
		observation.Warnings = append(observation.Warnings, "Existing system LC_ALL overrides category formats; applying a region explicitly removes LC_ALL")
	}
	if environment, err := readTrustedResolved("/etc/environment", 65536); err == nil {
		for key := range parseAssignments(string(environment)) {
			if key == "LANG" || key == "LANGUAGE" || key == "TZ" || strings.HasPrefix(key, "LC_") {
				observation.Warnings = append(observation.Warnings, "/etc/environment contains locale/timezone overrides; account and PAM environments may differ")
				break
			}
		}
	}
	observation.Manager, err = resolverManager(ctx)
	if err != nil {
		return observation, err
	}
	switch observation.Manager {
	case "openresolv":
		observation.Warnings = append(observation.Warnings, "Openresolv uses its native exclusive record; other DNS/VPN records are retained, but exclusive processing can supersede them. This does not prove every application's resolver or the recursive exit IP")
	case "resolvconf":
		observation.Warnings = append(observation.Warnings, "Debian resolvconf has no native exclusivity; conflicting peer sources are rejected rather than deleted. Generated resolv.conf is verified, not assumed to govern every subscriber/application resolver")
	case "debian-static":
		observation.Warnings = append(observation.Warnings, "Static resolv.conf ownership is checked at application/recovery; a future external DHCP/network writer is not controlled by this plugin and can cause a visible rollback conflict")
	}
	resolverData, err := readSystemResolver(observation.Manager)
	if err != nil {
		observation.Warnings = append(observation.Warnings, "System resolver contents cannot be read: "+err.Error())
	} else {
		observation.Actual.Dns.Servers = nameservers(resolverData)
	}
	var resolvedDNS []byte
	var resolvedRouting []byte
	if observation.Manager == "systemd-resolved" {
		resolvedDNS, err = runFixed(ctx, "/usr/bin/resolvectl", nil, "dns")
		if err == nil {
			for _, line := range strings.Split(string(resolvedDNS), "\n") {
				if strings.HasPrefix(line, "Global:") {
					var global []string
					for _, value := range strings.Fields(strings.TrimPrefix(line, "Global:")) {
						if address, err := netip.ParseAddr(value); err == nil {
							global = append(global, address.String())
						}
					}
					if len(global) > 0 {
						observation.Actual.Dns.Servers = global
					}
				}
				if strings.HasPrefix(strings.TrimSpace(line), "Link ") {
					_, addresses, _ := strings.Cut(line, ":")
					if strings.TrimSpace(addresses) != "" {
						observation.Warnings = append(observation.Warnings, "Per-link DNS remains configured; split/VPN routing domains can take priority. Global DNS is not proof of the sole effective upstream: "+strings.TrimSpace(line))
					}
				}
			}
		} else {
			observation.Warnings = append(observation.Warnings, "Resolved upstream inspection unavailable; resolv.conf stub addresses are not upstream DNS evidence")
		}
		resolvedRouting, err = runFixed(ctx, "/usr/bin/resolvectl", nil, "domain")
		if err != nil {
			observation.Warnings = append(observation.Warnings, "Resolved routing-domain inspection is unavailable; effective upstream selection is unknown")
		} else {
			evidence := strings.TrimSpace(string(resolvedRouting))
			if len(evidence) > 4096 {
				evidence = evidence[:4096] + " [truncated]"
			}
			observation.Warnings = append(observation.Warnings, "Resolved routing-domain evidence (configuration, not recursive exit): "+evidence)
		}
	}
	if observation.Manager == "openresolv" {
		native, err := openresolvState()
		if err != nil {
			return observation, err
		}
		state, err := readState("/run/resolvconf/interfaces/" + resolverRecord)
		if err != nil {
			return observation, err
		}
		if state.Kind == "file" {
			state.Mode = 0
			state.Target = native.Metadata
		}
		states["resolvconf-record"], states["resolver-peers"] = state, native.Peers
	} else if observation.Manager == "resolvconf" {
		state, err := b.Read(ctx, "resolvconf-record")
		if err != nil {
			return observation, err
		}
		states["resolvconf-record"] = state
	}
	packages, packageErr := runFixed(ctx, "/usr/bin/dpkg-query", nil, "-W", "-f=${Package} ${db:Status-Status}\\n", "locales", "tzdata", "fonts-noto-cjk", "fonts-noto-color-emoji")
	installed := make(map[string]bool)
	for _, line := range strings.Split(strings.ReplaceAll(string(packages), "\\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "installed" {
			installed[strings.Split(fields[0], ":")[0]] = true
		}
	}
	for _, name := range []string{"locales", "tzdata", "fonts-noto-cjk", "fonts-noto-color-emoji"} {
		if !installed[name] {
			observation.Missing = append(observation.Missing, name)
		}
	}
	if packageErr != nil && len(packages) == 0 {
		observation.Warnings = append(observation.Warnings, "Administrator package inventory is unavailable; dependencies are not presumed installed")
	}
	observation.Fingerprint = digest(struct {
		Files        map[string]FileState
		Resolver     []byte
		Resolved     []byte
		Routing      []byte
		Distribution string
	}{states, resolverData, resolvedDNS, resolvedRouting, distro})
	return observation, nil
}

func managedLocaleKey(key string) bool {
	if key == "LANG" || key == "LANGUAGE" || key == "LC_ALL" {
		return true
	}
	for _, category := range LocaleCategories {
		if key == category {
			return true
		}
	}
	return false
}

func localeFile(before []byte, config *epb.RegionConfig) []byte {
	lines := make([]string, 0)
	for _, line := range strings.Split(string(before), "\n") {
		if strings.TrimSpace(line) == strings.TrimSpace(managedHeader) {
			continue
		}
		key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && managedLocaleKey(key) {
			continue
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	lines = append(lines, strings.TrimSuffix(managedHeader, "\n"), "LANG=\""+config.Locale+"\"", "LANGUAGE=\""+strings.Join(config.Languages, ":")+"\"")
	keys := make([]string, 0, len(config.Formats))
	for key := range config.Formats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+"=\""+config.Formats[key]+"\"")
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func (b SystemBackend) Prepare(ctx context.Context, config *epb.RegionConfig) (Transition, error) {
	var transition Transition
	distro, err := distribution()
	if err != nil {
		return transition, err
	}
	if distro == "unsupported" {
		return transition, fail("failed_precondition", "region writes require Ubuntu 24.04 or Debian 12")
	}
	if _, err := validatedZoneData(config.Timezone); err != nil {
		return transition, fail("failed_precondition", "timezone data is missing, invalid or not root trusted; administrator must install/repair tzdata")
	}
	available, err := runFixed(ctx, "/usr/bin/locale", nil, "-a")
	if err != nil {
		return transition, fail("failed_precondition", "installed locale inventory is unavailable; administrator must install/configure locales")
	}
	installed := make(map[string]bool)
	for _, name := range strings.Fields(string(available)) {
		installed[strings.ToLower(strings.ReplaceAll(name, "-", ""))] = true
	}
	checkLocale := func(name string) error {
		if !installed[strings.ToLower(strings.ReplaceAll(name, "-", ""))] {
			return fail("failed_precondition", "UTF-8 locale "+name+" is not generated; request explicit administrator locales installation/generation")
		}
		return nil
	}
	if err := checkLocale(config.Locale); err != nil {
		return transition, err
	}
	for _, name := range config.Formats {
		if err := checkLocale(name); err != nil {
			return transition, err
		}
	}
	transition.Ownership = make(map[string]FileState)
	add := func(key string, after FileState) error {
		before, err := b.Read(ctx, key)
		if err != nil {
			return err
		}
		if before.Kind == "symlink" && key != "localtime" {
			return fail("failed_precondition", "refusing to replace unexpected setting symlink: "+key)
		}
		if before.Kind == "file" && after.Kind == "file" && key != "resolvconf-record" {
			after.Mode = before.Mode
		}
		if !sameFile(before, after) {
			transition.Entries = append(transition.Entries, Mutation{Key: key, Before: before, After: after})
		}
		return nil
	}
	if err := add("localtime", FileState{Kind: "symlink", Target: "/usr/share/zoneinfo/" + config.Timezone}); err != nil {
		return transition, err
	}
	if err := add("timezone", FileState{Kind: "file", Data: []byte(config.Timezone + "\n"), Mode: 0644}); err != nil {
		return transition, err
	}
	locale, err := b.Read(ctx, "locale")
	if err != nil {
		return transition, err
	}
	canonical, err := b.Read(ctx, "locale-canonical")
	if err != nil {
		return transition, err
	}
	localeKey, selectedLocale, err := selectLocaleState(locale, canonical)
	if err != nil {
		return transition, err
	}
	if localeKey != "locale" {
		transition.Ownership["locale"] = locale
	}
	if err := add(localeKey, FileState{Kind: "file", Data: localeFile(selectedLocale.Data, config), Mode: 0644}); err != nil {
		return transition, err
	}
	if !config.GetDns().GetManageSystem() {
		return transition, nil
	}
	transition.DNS = true
	transition.Manager, err = resolverManager(ctx)
	if err != nil {
		return transition, err
	}
	var input strings.Builder
	input.WriteString(managedHeader)
	for _, server := range config.Dns.Servers {
		input.WriteString("nameserver " + server + "\n")
	}
	owner, err := b.Read(ctx, "resolver")
	if err != nil {
		return transition, err
	}
	switch transition.Manager {
	case "debian-static":
		// Preserve search/options, while replacing only explicit nameservers.
		for _, line := range strings.Split(string(owner.Data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && (fields[0] == "search" || fields[0] == "domain" || fields[0] == "options" || fields[0] == "sortlist") {
				input.WriteString(line + "\n")
			}
		}
		if err := add("resolver", FileState{Kind: "file", Data: []byte(input.String()), Mode: 0644}); err != nil {
			return transition, err
		}
	case "systemd-resolved":
		for _, server := range config.Dns.Servers {
			if server == "127.0.0.53" || server == "127.0.0.54" {
				return transition, fail("failed_precondition", "resolved's own stub address cannot be configured as its upstream DNS server")
			}
		}
		transition.Ownership["resolver"] = owner
		peers, err := b.Read(ctx, "resolved-peers")
		if err != nil {
			return transition, fail("failed_precondition", "resolved per-link DNS/routing evidence is unavailable; refusing an uninspectable owner")
		}
		transition.Ownership["resolved-peers"] = peers
		data := managedHeader + "[Resolve]\nDNS=\nDNS=" + strings.Join(config.Dns.Servers, " ") + "\nFallbackDNS=\nDomains=~.\n"
		if err := add("resolved", FileState{Kind: "file", Data: []byte(data), Mode: 0644}); err != nil {
			return transition, err
		}
	case "resolvconf", "openresolv":
		transition.Ownership["resolver"] = owner
		for _, key := range []string{"resolvconf-head", "resolvconf-tail"} {
			state, err := b.Read(ctx, key)
			if err != nil {
				return transition, err
			}
			if len(nameservers(state.Data)) != 0 {
				return transition, fail("failed_precondition", "resolvconf head/tail contains administrator DNS servers; refusing to shadow them")
			}
			transition.Ownership[key] = state
		}
		if transition.Manager == "resolvconf" {
			// This owner has no exclusive mode. Refuse a concrete conflicting
			// source instead of padding providers or deleting DHCP/VPN records.
			sources, err := resolverSourceServers("/run/resolvconf/interface", true)
			if err != nil {
				return transition, fail("failed_precondition", "cannot inspect Debian resolvconf source records; administrator must repair owner inspection")
			}
			allowed := make(map[string]bool, len(config.Dns.Servers))
			for _, server := range config.Dns.Servers {
				allowed[server] = true
			}
			for _, server := range sources {
				if !allowed[server] {
					return transition, fail("failed_precondition", "Debian resolvconf has another DNS source "+server+" and no native exclusive mode; administrator must select an exclusive owner or explicitly reconcile source records")
				}
			}
			order, err := b.Read(ctx, "resolvconf-order")
			if err != nil {
				return transition, err
			}
			if order.Kind != "file" {
				return transition, fail("failed_precondition", "Debian resolvconf interface-order is unavailable")
			}
			lines := []string{resolverRecord}
			for _, line := range strings.Split(string(order.Data), "\n") {
				if strings.TrimSpace(line) != resolverRecord {
					lines = append(lines, line)
				}
			}
			if err := add("resolvconf-order", FileState{Kind: "file", Data: []byte(strings.Join(lines, "\n")), Mode: order.Mode}); err != nil {
				return transition, err
			}
		}
		record, err := b.Read(ctx, "resolvconf-record")
		if err != nil {
			return transition, err
		}
		if transition.Manager == "resolvconf" {
			provenance, err := b.Read(ctx, "resolvconf-owner")
			if err != nil {
				return transition, err
			}
			if !ownsDebianResolverRecord(record, provenance) {
				return transition, fail("failed_precondition", "reserved resolvconf record lacks matching root ownership evidence")
			}
		} else if record.Kind != "absent" && !bytes.HasPrefix(record.Data, []byte(managedHeader)) {
			return transition, fail("failed_precondition", "reserved resolvconf record already belongs to another writer")
		}
		after := FileState{Kind: "file", Data: []byte(input.String())}
		if transition.Manager == "resolvconf" {
			// Native Debian resolvconf strips comments. Bind the actual record
			// bytes to a separate root-owned, journaled provenance checkpoint.
			after.Data = []byte(strings.TrimPrefix(input.String(), managedHeader))
			if err := add("resolvconf-owner", FileState{Kind: "file", Data: []byte(digest(after.Data)), Mode: 0600}); err != nil {
				return transition, err
			}
		}
		if transition.Manager == "openresolv" {
			if err := validateOpenresolvConfiguration(); err != nil {
				return transition, err
			}
			native, err := openresolvState()
			if err != nil {
				return transition, err
			}
			if native.OtherExclusive {
				return transition, fail("failed_precondition", "another openresolv exclusive owner exists; administrator must explicitly reconcile it without deleting unrelated VPN/split-DNS records")
			}
			if record.Kind == "file" && record.Target != normalExclusiveMetadata {
				return transition, fail("failed_precondition", "reserved openresolv record metadata was externally changed; administrator review is required")
			}
			if record.Kind == "absent" && native.Metadata != "exclusive=false;private=false;deprecated=false;metric=" {
				return transition, fail("failed_precondition", "reserved openresolv metadata exists without an owned record; administrator review is required")
			}
			transition.Ownership["resolver-peers"] = native.Peers
			after.Target = normalExclusiveMetadata
		}
		if err := add("resolvconf-record", after); err != nil {
			return transition, err
		}
	default:
		return transition, fail("failed_precondition", "system DNS owner is not safely supported: "+transition.Manager+"; administrator must select a supported resolver owner")
	}
	return transition, nil
}

func (b SystemBackend) Write(ctx context.Context, key string, state FileState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "resolvconf-record" {
		manager, err := resolverManager(ctx)
		if err != nil {
			return err
		}
		if manager != "resolvconf" && manager != "openresolv" {
			return fail("aborted", "resolvconf ownership changed")
		}
		if manager == "openresolv" {
			if err := validateOpenresolvConfiguration(); err != nil {
				return err
			}
			native, err := openresolvState()
			if err != nil {
				return err
			}
			if native.OtherExclusive {
				return fail("aborted", "another openresolv exclusive owner appeared; refusing to override it")
			}
		}
		if state.Kind == "absent" {
			_, err = runFixed(ctx, "/usr/sbin/resolvconf", nil, "-d", resolverRecord)
			return err
		}
		if state.Kind != "file" {
			return errors.New("invalid fixed resolvconf record snapshot")
		}
		args := []string{"-a", resolverRecord}
		if manager == "openresolv" {
			args = []string{"-x", "-a", resolverRecord}
		}
		_, err = runFixed(ctx, "/usr/sbin/resolvconf", state.Data, args...)
		return err
	}
	path, err := fixedPath(key)
	if err != nil {
		return err
	}
	if key == "resolvconf-head" || key == "resolvconf-tail" {
		return errors.New("resolver ownership checkpoints cannot be written")
	}
	parent := filepath.Dir(path)
	if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) && key == "resolved" {
		if err := trustedDirectory(filepath.Dir(parent)); err != nil {
			return err
		}
		if err := os.Mkdir(parent, 0755); err == nil {
			// The resolver service must traverse the new directory even when
			// the authorized caller uses a private umask.
			if err := os.Chmod(parent, 0755); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if err := trustedDirectory(parent); err != nil {
		return err
	}
	// Recheck the current target before atomically replacing it. Existing
	// non-symlinks must remain ordinary root-owned files, never devices/FIFOs.
	if _, err := readState(path); err != nil {
		return err
	}
	switch state.Kind {
	case "absent":
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	case "file":
		if len(state.Data) > 1<<20 || state.Mode&0022 != 0 {
			return errors.New("invalid root setting snapshot")
		}
		if err := atomicFile(parent, filepath.Base(path), state.Data, os.FileMode(state.Mode)); err != nil {
			return err
		}
	case "symlink":
		if key != "localtime" {
			return errors.New("only fixed timezone symlinks can be restored")
		}
		if !filepath.IsAbs(state.Target) { // Old administrator timezone links can be relative.
			if _, err := trustedPath(filepath.Join(parent, state.Target)); err != nil {
				return err
			}
		} else if _, err := trustedPath(state.Target); err != nil {
			return err
		}
		id, err := randomID(".shiki-region-")
		if err != nil {
			return err
		}
		temporary := filepath.Join(parent, id)
		if err := os.Symlink(state.Target, temporary); err != nil {
			return err
		}
		defer os.Remove(temporary)
		if err := os.Rename(temporary, path); err != nil {
			return err
		}
	default:
		return errors.New("invalid region setting snapshot kind")
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (SystemBackend) Reload(ctx context.Context, transition Transition) error {
	if !transition.DNS {
		return nil
	}
	manager, err := resolverManager(ctx)
	if err != nil {
		return err
	}
	if manager != transition.Manager && !(transition.Manager == "systemd-resolved" && manager == "systemd-resolved-inactive") {
		return fail("aborted", "DNS owner changed during region transaction")
	}
	switch transition.Manager {
	case "systemd-resolved":
		_, err = runFixed(ctx, "/usr/bin/systemctl", nil, "restart", "systemd-resolved.service")
	case "resolvconf", "openresolv":
		if manager == "openresolv" {
			record, readErr := readState("/run/resolvconf/interfaces/" + resolverRecord)
			if readErr != nil {
				return readErr
			}
			if record.Kind == "absent" {
				if _, err := runFixed(ctx, "/usr/sbin/resolvconf", nil, "-f", "-d", resolverRecord); err != nil {
					return err
				}
			}
		}
		_, err = runFixed(ctx, "/usr/sbin/resolvconf", nil, "-u")
	case "debian-static":
		return nil
	default:
		return fail("failed_precondition", "DNS reload owner unsupported")
	}
	return err
}

func (SystemBackend) CheckDNS(parent context.Context, config *epb.RegionConfig) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	manager, err := resolverManager(ctx)
	if err != nil {
		return nil, err
	}
	data, err := readSystemResolver(manager)
	if err != nil {
		return nil, err
	}
	servers := nameservers(data)
	observed, err := (SystemBackend{}).Inspect(ctx)
	if err != nil {
		return nil, err
	}
	if strings.Join(observed.Actual.Dns.Servers, " ") != strings.Join(config.Dns.Servers, " ") {
		return nil, errors.New("effective DNS servers differ from the explicitly authorized list; refusing silent fallback")
	}
	if len(servers) == 0 {
		return nil, errors.New("new resolver has no nameservers")
	}
	var warnings []string
	for _, warning := range observed.Warnings {
		if strings.HasPrefix(warning, "Per-link") || strings.HasPrefix(warning, "Resolved") {
			warnings = append(warnings, warning)
		}
	}
	dnsDialer := &net.Dialer{Timeout: 3 * time.Second}
	for _, domain := range config.Dns.CheckDomains {
		addresses, err := lookupDNSServers(ctx, servers, domain, dnsDialer.DialContext)
		if err != nil || len(addresses) == 0 {
			return warnings, fmt.Errorf("fresh DNS lookup failed for explicitly authorized domain %s", domain)
		}
		connected := false
		for index, address := range addresses {
			if index >= 8 {
				break
			}
			address = address.Unmap()
			if !publicProbeAddress(address) {
				return warnings, fmt.Errorf("DNS check domain %s returned a non-public address; refusing a root connectivity probe", domain)
			}
			dialer := &net.Dialer{Timeout: 4 * time.Second}
			tlsDialer := tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12}}
			connection, err := tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(address.String(), "443"))
			if err == nil {
				connection.Close()
				connected = true
				break
			}
		}
		if !connected {
			return warnings, fmt.Errorf("new verified TLS connection failed for explicitly authorized domain %s", domain)
		}
		warnings = append(warnings, "Fresh system DNS and certificate-verified TLS passed for "+domain+" (DNS names, TLS SNI and native TLS/network metadata are observable; no HTTP request, account, cookie, token or collected browser fingerprint is sent)")
	}
	return warnings, nil
}

// Each attempt owns an entire DNS exchange, not just a successful UDP dial.
// Dial is an in-process test seam; production always uses the fixed port 53
// and the already inspected system resolver list, with no implicit providers.
func lookupDNSServers(ctx context.Context, servers []string, domain string, dial func(context.Context, string, string) (net.Conn, error)) ([]netip.Addr, error) {
	var last error
	for _, server := range servers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		resolver := &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dial(ctx, network, net.JoinHostPort(server, "53"))
		}}
		addresses, err := resolver.LookupNetIP(attempt, "ip", domain+".")
		cancel()
		if err == nil && len(addresses) != 0 {
			return addresses, nil
		}
		if err == nil {
			err = errors.New("DNS response contained no IP addresses")
		}
		last = err
	}
	if last == nil {
		last = errors.New("new resolver has no nameservers")
	}
	return nil, last
}

var blockedProbePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"),
}

func publicProbeAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedProbePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func runFixed(parent context.Context, path string, input []byte, args ...string) ([]byte, error) {
	switch path {
	case "/usr/bin/systemctl", "/usr/bin/resolvectl", "/usr/bin/locale", "/usr/bin/dpkg-query", "/usr/sbin/resolvconf":
	default:
		return nil, errors.New("unapproved region helper executable")
	}
	resolved, err := trustedPath(path)
	if err != nil {
		return nil, err
	}
	if path == "/usr/sbin/resolvconf" {
		if err := trustedResolverResources(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	command := exec.Command(resolved, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "HOME=/root"}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	output := &limitedBuffer{maximum: 65536}
	stderr := &limitedBuffer{maximum: 4096}
	command.Stdout, command.Stderr = output, stderr
	err = runOwnedCommand(ctx, command)
	if err != nil {
		return output.Bytes(), fmt.Errorf("fixed region dependency %s failed: %w", filepath.Base(path), err)
	}
	return output.Bytes(), nil
}

func (SystemBackend) BootIdentity(context.Context) (string, error) {
	data, err := readTrustedResolved("/proc/sys/kernel/random/boot_id", 128)
	if err != nil {
		return "", err
	}
	boot := strings.TrimSpace(string(data))
	if len(boot) != 36 {
		return "", errors.New("kernel boot identity is unavailable")
	}
	return boot, nil
}

// resolvconf executes owner-native subscribers and sources configuration. A
// root-owned launcher alone is not enough if a hook/config can be replaced by
// the daemon UID, so check the complete fixed owner resource trees as well.
func trustedResolverResources() error {
	seen := make(map[string]bool)
	count := 0
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 16 || count >= 4096 {
			return fail("failed_precondition", "resolvconf trust tree exceeds its bounded supported size")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if seen[resolved] {
			return nil
		}
		seen[resolved] = true
		count++
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := trustedDirectory(resolved); err != nil {
				return err
			}
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := visit(filepath.Join(resolved, entry.Name()), depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		_, err = trustedPath(resolved)
		return err
	}
	for _, path := range []string{"/etc/resolvconf", "/etc/resolvconf.conf", "/etc/default/resolvconf", "/lib/resolvconf"} {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := visit(path, 0); err != nil {
			return fail("failed_precondition", "resolvconf owner configuration/subscribers are not entirely root trusted: "+err.Error())
		}
	}
	return nil
}

// CLOCK_BOOTTIME bounds an unconfirmed lease independently of wall-clock/NTP
// adjustments and survives guard restarts within the same kernel boot.
func (SystemBackend) BootElapsed(context.Context) (int64, error) {
	var clock unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &clock); err != nil {
		return 0, err
	}
	return clock.Nano(), nil
}

func validatedZoneData(zone string) ([]byte, error) {
	if !zoneName.MatchString(zone) || zone == "Local" || strings.Contains(zone, "..") {
		return nil, errors.New("invalid fixed IANA timezone")
	}
	data, err := readTrustedResolved("/usr/share/zoneinfo/"+zone, 1<<20)
	if err != nil {
		return nil, err
	}
	if _, err := time.LoadLocationFromTZData(zone, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Ubuntu's packaged legacy locale path points to /etc/locale.conf. Only this
// fixed alias is accepted; the alias itself remains an ownership checkpoint.
func selectLocaleState(legacy, canonical FileState) (string, FileState, error) {
	if legacy.Kind == "absent" && canonical.Kind == "file" {
		return "locale-canonical", canonical, nil
	}
	if legacy.Kind != "symlink" {
		return "locale", legacy, nil
	}
	target := legacy.Target
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc/default", target)
	}
	if filepath.Clean(target) != "/etc/locale.conf" || canonical.Kind != "file" {
		return "", FileState{}, fail("failed_precondition", "locale alias is not the supported root-owned /etc/locale.conf regular file")
	}
	return "locale-canonical", canonical, nil
}
