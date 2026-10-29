package config

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// SessionTemplate contains only launch configuration. WorkingDir is relative
// to the selected remote project, and AutoStart defaults to true when absent.
type SessionTemplate struct {
	Description string            `yaml:"description"`
	Sessions    []TemplateSession `yaml:"sessions"`
}
type TemplateSession struct {
	Name        string            `yaml:"name"`
	WorkingDir  string            `yaml:"working_dir"`
	Description string            `yaml:"description"`
	Tags        []string          `yaml:"tags"`
	Metadata    map[string]string `yaml:"metadata"`
	AutoStart   *bool             `yaml:"auto_start"`
}

func (c *Client) validateTemplates() error {
	if len(c.Templates) > 100 {
		return errors.New("at most 100 templates are allowed")
	}
	for name, template := range c.Templates {
		if !identifier.MatchString(name) {
			return errors.New("template names must use letters, digits, dash, underscore or dot")
		}
		if len(template.Description) > 4096 || len(template.Sessions) == 0 || len(template.Sessions) > 100 {
			return fmt.Errorf("template %s requires 1–100 sessions and a description no longer than 4096 bytes", name)
		}
		names := map[string]bool{}
		for _, session := range template.Sessions {
			if !identifier.MatchString(session.Name) || names[session.Name] {
				return fmt.Errorf("template %s contains an invalid or duplicate session name", name)
			}
			names[session.Name] = true
			if !ValidTemplateDirectory(session.WorkingDir) {
				return fmt.Errorf("template %s working_dir must be a clean remote-relative path", name)
			}
			if len(session.Description) > 4096 || len(session.Tags) > 64 || len(session.Metadata) > 4 {
				return fmt.Errorf("template %s session metadata exceeds limits", name)
			}
			for _, tag := range session.Tags {
				if len(tag) > 128 {
					return fmt.Errorf("template %s tag exceeds 128 bytes", name)
				}
			}
			if len(SnapshotMetadata(session.Metadata)) != len(session.Metadata) {
				return fmt.Errorf("template %s metadata permits only safe color, icon, language, and role values", name)
			}
		}
	}
	return nil
}
func ValidTemplateDirectory(value string) bool {
	return value == "" || value == "." || !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00")
}

var presentationValue = regexp.MustCompile(`^[A-Za-z0-9#][A-Za-z0-9_.#-]{0,63}$`)

// SnapshotMetadata explicitly selects presentation metadata, never copying an
// arbitrary map that could contain credentials, environment, or conversation IDs.
func SnapshotMetadata(values map[string]string) map[string]string {
	result := map[string]string{}
	for _, key := range []string{"color", "icon", "language", "role"} {
		value, ok := values[key]
		if !ok || !presentationValue.MatchString(value) {
			continue
		}
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "shiki_") || strings.HasPrefix(lower, "tskey-") || strings.HasPrefix(lower, "sk-") {
			continue
		}
		result[key] = value
	}
	return result
}
