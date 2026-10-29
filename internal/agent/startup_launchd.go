package agent

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func (p *startupObserver) launchd(ctx context.Context, result *apb.StartupPolicy) {
	result.Manager, result.BootStart, result.ReasonCode = "launchd", "not_applicable", "STARTUP_MANAGER_UNAVAILABLE"
	domain := "gui/" + strconv.FormatUint(uint64(p.uid), 10)
	data, err := p.run(ctx, "/bin/launchctl", "print-disabled", domain)
	if err != nil {
		return
	}
	overrides, ok := startupLaunchOverrides(data)
	if !ok {
		result.ReasonCode = "STARTUP_MANAGER_OUTPUT_UNKNOWN"
		return
	}
	path := filepath.Join(p.home, "Library/LaunchAgents/dev.shikigami.agent.plist")
	plist, err := p.read(path)
	if os.IsNotExist(err) {
		// Only the standard installer-managed plist is absent. This says nothing
		// about an arbitrarily named plist, Login Item or other user automation.
		result.ManagedService, result.LoginStart, result.ReasonCode = "absent", "disabled", "STARTUP_MANAGED_SERVICE_ABSENT"
		return
	}
	result.ReasonCode = "STARTUP_SERVICE_CONFIGURATION_UNKNOWN"
	if err != nil {
		return
	}
	values, ok := startupPlist(plist)
	if !ok || values["Label"].kind != "string" || values["Label"].text != "dev.shikigami.agent" {
		return
	}
	args := values["ProgramArguments"]
	if args.kind != "array" || len(args.items) != 3 || args.items[0].kind != "string" || !filepath.IsAbs(args.items[0].text) || filepath.Base(args.items[0].text) != "shiki-agent" || args.items[1].kind != "string" || args.items[1].text != "--config" || args.items[2].kind != "string" || args.items[2].text != p.config {
		return
	}
	// Limit policy interpretation to the installer's known keys. Other launch
	// triggers or executable/config overrides must not become a positive claim.
	allowed := map[string]bool{"Label": true, "ProgramArguments": true, "EnvironmentVariables": true, "WorkingDirectory": true, "RunAtLoad": true, "KeepAlive": true, "Umask": true, "Disabled": true}
	for key := range values {
		if !allowed[key] {
			return
		}
	}
	if env, exists := values["EnvironmentVariables"]; exists {
		entries, ok := env.dictionary()
		if !ok {
			return
		}
		for key, value := range entries {
			if (key != "HOME" && key != "PATH") || value.kind != "string" {
				return
			}
		}
	}
	if directory, exists := values["WorkingDirectory"]; exists {
		if directory.kind != "string" || !filepath.IsAbs(directory.text) || strings.ContainsAny(directory.text, "\x00\r\n") {
			return
		}
	}
	if mask, exists := values["Umask"]; exists {
		number, err := strconv.ParseUint(mask.text, 10, 16)
		if mask.kind != "integer" || err != nil || number > 0777 {
			return
		}
	}
	disabled, ok := plistBoolean(values, "Disabled", false)
	if !ok {
		return
	}
	if override, exists := overrides["dev.shikigami.agent"]; exists {
		disabled = override
	}
	atLoad, ok := plistBoolean(values, "RunAtLoad", false)
	if !ok {
		return
	}
	if keep, exists := values["KeepAlive"]; exists {
		entries, ok := keep.dictionary()
		// SuccessfulExit=false is the exact generated restart-on-failure policy.
		// Other KeepAlive forms have different eager-launch semantics: unknown.
		if !ok || len(entries) != 1 || entries["SuccessfulExit"].kind != "false" || !atLoad {
			return
		}
	}
	again, err := p.run(ctx, "/bin/launchctl", "print-disabled", domain)
	if err != nil || string(again) != string(data) {
		return
	}
	checked, err := p.read(path)
	if err != nil || string(checked) != string(plist) {
		return
	}
	result.ManagedService, result.LoginStart, result.ReasonCode = "present", "disabled", "STARTUP_MANAGED_SERVICE_DISABLED"
	if !disabled && atLoad {
		result.LoginStart, result.ReasonCode = "enabled", "STARTUP_CONFIGURED_POLICY_OBSERVED"
	}
}

var startupOverride = regexp.MustCompile(`^"([^"\r\n]+)" => (enabled|disabled)$`)

func startupLaunchOverrides(data []byte) (map[string]bool, bool) {
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "disabled services = {") || !strings.HasSuffix(text, "}") {
		return nil, false
	}
	result := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "disabled services = {"), "}")), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := startupOverride.FindStringSubmatch(line)
		if match == nil {
			return nil, false
		}
		if _, exists := result[match[1]]; exists {
			return nil, false
		}
		result[match[1]] = match[2] == "disabled"
	}
	return result, true
}

type startupPlistValue struct {
	kind, text string
	items      []startupPlistValue
}

func (v startupPlistValue) dictionary() (map[string]startupPlistValue, bool) {
	if v.kind != "dict" || len(v.items)%2 != 0 {
		return nil, false
	}
	values := map[string]startupPlistValue{}
	for i := 0; i < len(v.items); i += 2 {
		key := v.items[i]
		if key.kind != "key" {
			return nil, false
		}
		if _, exists := values[key.text]; exists {
			return nil, false
		}
		values[key.text] = v.items[i+1]
	}
	return values, true
}
func plistBoolean(values map[string]startupPlistValue, key string, fallback bool) (bool, bool) {
	value, exists := values[key]
	if !exists {
		return fallback, true
	}
	if value.kind != "true" && value.kind != "false" {
		return false, false
	}
	return value.kind == "true", true
}
func startupPlist(data []byte) (map[string]startupPlistValue, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var roots []startupPlistValue
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		if start, ok := token.(xml.StartElement); ok {
			value, ok := readStartupPlistValue(decoder, start, 0)
			if !ok {
				return nil, false
			}
			roots = append(roots, value)
		} else if text, ok := token.(xml.CharData); ok && strings.TrimSpace(string(text)) != "" {
			return nil, false
		}
	}
	if len(roots) != 1 || roots[0].kind != "plist" || len(roots[0].items) != 1 {
		return nil, false
	}
	return roots[0].items[0].dictionary()
}
func readStartupPlistValue(decoder *xml.Decoder, start xml.StartElement, depth int) (startupPlistValue, bool) {
	value := startupPlistValue{kind: start.Name.Local}
	if depth > 8 || start.Name.Space != "" {
		return value, false
	}
	if value.kind == "plist" {
		if len(start.Attr) != 1 || start.Attr[0].Name.Local != "version" || start.Attr[0].Name.Space != "" || start.Attr[0].Value != "1.0" {
			return value, false
		}
	} else if len(start.Attr) != 0 {
		return value, false
	}
	switch value.kind {
	case "plist", "dict", "array", "key", "string", "integer", "true", "false":
	default:
		return value, false
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return value, false
		}
		switch token := token.(type) {
		case xml.StartElement:
			child, ok := readStartupPlistValue(decoder, token, depth+1)
			if !ok {
				return value, false
			}
			value.items = append(value.items, child)
		case xml.CharData:
			value.text += string(token)
		case xml.EndElement:
			if (value.kind == "plist" || value.kind == "dict" || value.kind == "array") && strings.TrimSpace(value.text) != "" {
				return value, false
			}
			if (value.kind == "true" || value.kind == "false") && (strings.TrimSpace(value.text) != "" || len(value.items) > 0) {
				return value, false
			}
			if (value.kind == "string" || value.kind == "key" || value.kind == "integer") && len(value.items) > 0 {
				return value, false
			}
			return value, true
		}
	}
}
