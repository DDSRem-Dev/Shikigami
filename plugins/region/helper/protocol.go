// Package helper implements the fixed-purpose region transaction protocol. Its
// production entry point never accepts filesystem roots or executable paths.
package helper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

const (
	ExecutablePath   = "/usr/lib/shikigami/shiki-region-helper"
	PolicyPath       = "/etc/shikigami-region-helper.json"
	StatePath        = "/var/lib/shikigami-region"
	GuardUnit        = "shikigami-region-guard.service"
	MaxRequestBytes  = 32768
	MaxResponseBytes = 262144
)

var Actions = []string{"inspect", "plan", "apply", "confirm", "rollback", "operation"}

// Request has no path, executable, UID, source URL, shell, or arbitrary action.
// The action is a separate, exact, sudoers-authorized command argument.
type Request struct {
	Config           *epb.RegionConfig `json:"config,omitempty"`
	ExpectedRevision string            `json:"expected_revision,omitempty"`
	RequestID        string            `json:"request_id,omitempty"`
	PlanID           string            `json:"plan_id,omitempty"`
	OperationID      string            `json:"operation_id,omitempty"`
	Confirm          bool              `json:"confirm,omitempty"`
	DNSAuthorized    bool              `json:"dns_authorized,omitempty"`
}

type Response struct {
	Status    *epb.RegionStatus    `json:"status,omitempty"`
	Plan      *epb.RegionPlan      `json:"plan,omitempty"`
	Operation *epb.RegionOperation `json:"operation,omitempty"`
	Code      string               `json:"code,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type Error struct{ Code, Message string }

func (e *Error) Error() string        { return e.Message }
func fail(code, message string) error { return &Error{code, message} }

func DecodeRequest(reader io.Reader) (Request, error) {
	var request Request
	data, err := io.ReadAll(io.LimitReader(reader, MaxRequestBytes+1))
	if err != nil {
		return request, err
	}
	if len(data) > MaxRequestBytes {
		return request, fail("resource_exhausted", "region helper request exceeds limit")
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return request, fail("invalid_argument", "region helper input must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fail("invalid_argument", "invalid region helper JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, fail("invalid_argument", "exactly one JSON object is required")
	}
	return request, nil
}

func EncodeResponse(writer io.Writer, response Response) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if len(data) > MaxResponseBytes {
		return errors.New("region helper response exceeds limit")
	}
	_, err = writer.Write(append(data, '\n'))
	return err
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var localeName = regexp.MustCompile(`^[a-z]{2,3}_[A-Z]{2}\.(?i:UTF-?8)$`)
var languageName = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z]{2,8})?(@[A-Za-z0-9_-]{1,24})?$`)
var zoneName = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)
var domainName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

var LocaleCategories = []string{
	"LC_ADDRESS", "LC_COLLATE", "LC_CTYPE", "LC_IDENTIFICATION", "LC_MEASUREMENT",
	"LC_MESSAGES", "LC_MONETARY", "LC_NAME", "LC_NUMERIC", "LC_PAPER", "LC_TELEPHONE", "LC_TIME",
}

func NormalizeConfig(input *epb.RegionConfig) (*epb.RegionConfig, error) {
	if input == nil {
		return nil, fail("invalid_argument", "region configuration is required")
	}
	config := proto.Clone(input).(*epb.RegionConfig)
	config.Country = strings.ToUpper(config.Country)
	if config.Country != "JP" && config.Country != "US" {
		return nil, fail("invalid_argument", "only JP and US region presets are supported")
	}
	if config.Country == "JP" {
		if config.Timezone == "" {
			config.Timezone = "Asia/Tokyo"
		}
		if config.Locale == "" {
			config.Locale = "ja_JP.UTF-8"
		}
		if len(config.Languages) == 0 {
			config.Languages = []string{"ja_JP", "ja"}
		}
	}
	if !zoneName.MatchString(config.Timezone) || config.Timezone == "Local" || strings.Contains(config.Timezone, "..") || len(config.Timezone) > 128 {
		return nil, fail("invalid_argument", "an explicit IANA timezone is required (US has no implicit timezone)")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return nil, fail("invalid_argument", "timezone is not an installed IANA timezone")
	}
	if !localeName.MatchString(config.Locale) {
		return nil, fail("invalid_argument", "locale must be an explicit UTF-8 POSIX locale, e.g. en_US.UTF-8")
	}
	config.Locale = canonicalLocale(config.Locale)
	if len(config.Languages) == 0 || len(config.Languages) > 8 {
		return nil, fail("invalid_argument", "one to eight explicit language preferences are required")
	}
	seen := make(map[string]bool)
	for _, language := range config.Languages {
		if !languageName.MatchString(language) || seen[language] {
			return nil, fail("invalid_argument", "languages must be unique gettext language identifiers")
		}
		seen[language] = true
	}
	allowed := make(map[string]bool, len(LocaleCategories))
	for _, name := range LocaleCategories {
		allowed[name] = true
	}
	for name, value := range config.Formats {
		if !allowed[name] || !localeName.MatchString(value) {
			return nil, fail("invalid_argument", "only individual UTF-8 LC_* categories are allowed; LC_ALL is forbidden")
		}
		config.Formats[name] = canonicalLocale(value)
	}
	if config.Dns == nil {
		config.Dns = &epb.DNSConfig{ChromeMode: "unchanged"}
	}
	dns := config.Dns
	if dns.ChromeMode == "" {
		dns.ChromeMode = "unchanged"
	}
	if len(dns.Servers) > 3 {
		return nil, fail("invalid_argument", "at most three explicit system DNS servers are supported")
	}
	seen = make(map[string]bool)
	for index, server := range dns.Servers {
		address, err := netip.ParseAddr(server)
		if err != nil || address.Zone() != "" {
			return nil, fail("invalid_argument", "system DNS servers must be explicit unscoped IP addresses")
		}
		address = address.Unmap()
		if address.IsUnspecified() || address.IsMulticast() || (address.Is6() && address.IsLinkLocalUnicast()) || !(address.IsGlobalUnicast() || address.IsLoopback() || (address.Is4() && address.IsLinkLocalUnicast())) {
			return nil, fail("invalid_argument", "system DNS servers must be unicast IP addresses; scoped IPv6 link-local resolvers require an unsupported interface policy")
		}
		server = address.String()
		if seen[server] {
			return nil, fail("invalid_argument", "system DNS servers must be unique")
		}
		seen[server], dns.Servers[index] = true, server
	}
	if dns.ManageSystem && (len(dns.Servers) == 0 || len(dns.CheckDomains) == 0) {
		return nil, fail("invalid_argument", "system DNS requires explicit servers and HTTPS connectivity check domains")
	}
	if !dns.ManageSystem && (len(dns.Servers) != 0 || len(dns.CheckDomains) != 0) {
		return nil, fail("invalid_argument", "system DNS servers/check domains require explicit manage_system authorization")
	}
	if len(dns.CheckDomains) > 4 {
		return nil, fail("invalid_argument", "at most four DNS connectivity check domains are allowed")
	}
	seen = make(map[string]bool)
	for index, domain := range dns.CheckDomains {
		domain = strings.ToLower(strings.TrimSuffix(domain, "."))
		if !domainName.MatchString(domain) || !strings.Contains(domain, ".") || strings.Contains(domain, "..") || net.ParseIP(domain) != nil || strings.HasSuffix(domain, ".local") || strings.HasSuffix(domain, ".localhost") || seen[domain] {
			return nil, fail("invalid_argument", "DNS check domains must be unique fully qualified public domain names")
		}
		for _, label := range strings.Split(domain, ".") {
			if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil, fail("invalid_argument", "invalid DNS check domain label")
			}
		}
		seen[domain], dns.CheckDomains[index] = true, domain
	}
	switch dns.ChromeMode {
	case "unchanged", "system":
		if len(dns.ChromeTemplates) != 0 {
			return nil, fail("invalid_argument", "Chrome DNS templates require secure or automatic mode")
		}
	case "secure", "automatic":
		if len(dns.ChromeTemplates) == 0 || len(dns.ChromeTemplates) > 4 {
			return nil, fail("invalid_argument", "Chrome DNS requires one to four explicit HTTPS templates; no provider is selected implicitly")
		}
		for _, template := range dns.ChromeTemplates {
			parsed, err := url.Parse(strings.ReplaceAll(template, "{?dns}", ""))
			if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || len(template) > 1024 || strings.ContainsAny(template, "\r\n\t ") || strings.Contains(strings.ReplaceAll(template, "{?dns}", ""), "{") {
				return nil, fail("invalid_argument", "invalid explicit Chrome HTTPS DNS template")
			}
		}
	default:
		return nil, fail("invalid_argument", "Chrome DNS mode must be unchanged, system, secure, or automatic")
	}
	return config, nil
}

func canonicalLocale(value string) string {
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, ".utf8") {
		return value[:len(value)-5] + ".UTF-8"
	}
	if strings.HasSuffix(lower, ".utf-8") {
		return value[:len(value)-6] + ".UTF-8"
	}
	return value
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func cloneConfig(value *epb.RegionConfig) *epb.RegionConfig {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*epb.RegionConfig)
}
func settingChanges(before, after *epb.RegionConfig) []*epb.SettingChange {
	if before == nil {
		before = &epb.RegionConfig{}
	}
	changes := make([]*epb.SettingChange, 0, 20)
	add := func(name, old, next string, privileged bool) {
		if old != next {
			changes = append(changes, &epb.SettingChange{Setting: name, Before: old, After: next, Privileged: privileged})
		}
	}
	add("timezone", before.Timezone, after.Timezone, true)
	add("LANG", before.Locale, after.Locale, true)
	add("LANGUAGE", strings.Join(before.Languages, ":"), strings.Join(after.Languages, ":"), true)
	keys := append([]string(nil), LocaleCategories...)
	sort.Strings(keys)
	for _, key := range keys {
		add(key, before.Formats[key], after.Formats[key], true)
	}
	if after.Dns.ManageSystem {
		add("system_dns", strings.Join(before.GetDns().GetServers(), " "), strings.Join(after.Dns.Servers, " "), true)
	}
	add("chrome_dns_mode_on_next_launch", before.GetDns().GetChromeMode(), after.Dns.ChromeMode, false)
	add("chrome_dns_templates_on_next_launch", strings.Join(before.GetDns().GetChromeTemplates(), " "), strings.Join(after.Dns.ChromeTemplates, " "), false)
	return changes
}

func requestDigest(action string, request Request) string {
	return digest(struct {
		Action  string
		Request Request
	}{action, request})
}
func validID(value, label string) error {
	if !identifier.MatchString(value) {
		return fail("invalid_argument", fmt.Sprintf("%s must be 8..128 safe identifier characters", label))
	}
	return nil
}

// Store serializes the complete read/check/write transaction across helper and
// guard processes. save must be called before the first privileged mutation.
type Store interface {
	WithLock(context.Context, func(*Journal, func() error) error) error
}

type FileState struct {
	Kind   string `json:"kind"` // absent, file, symlink, or fixed resolver record
	Data   []byte `json:"data,omitempty"`
	Target string `json:"target,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
}

type Mutation struct {
	Key    string    `json:"key"`
	Before FileState `json:"before"`
	After  FileState `json:"after"`
}
type Transition struct {
	Manager   string               `json:"manager"`
	DNS       bool                 `json:"dns"`
	Entries   []Mutation           `json:"entries"`
	Ownership map[string]FileState `json:"ownership,omitempty"`
}
type Observation struct {
	Actual               *epb.RegionConfig
	Manager, Fingerprint string
	Warnings, Missing    []string
}

// Backend is injected only in process, never through root CLI JSON/flags.
type Backend interface {
	Inspect(context.Context) (Observation, error)
	BootIdentity(context.Context) (string, error)
	BootElapsed(context.Context) (int64, error)
	Prepare(context.Context, *epb.RegionConfig) (Transition, error)
	Write(context.Context, string, FileState) error
	Read(context.Context, string) (FileState, error)
	Reload(context.Context, Transition) error
	CheckDNS(context.Context, *epb.RegionConfig) ([]string, error)
}
