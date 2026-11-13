// Package plugin — ConfigField and related types for plugin configuration
// schema declaration. These types are defined in manifest.json under
// config_schema and confirmed at runtime via PluginInfo.config_schema.
package plugin

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

// ConfigField describes one typed configuration value in a plugin's schema.
type ConfigField struct {
	// Key is the dot-separated config key (e.g. "max_auto_sync_size").
	Key string `json:"key"`
	// Type is one of: string, int, bool, float, array, object.
	Type string `json:"type"`
	// Label is the human-readable field label for the settings UI.
	Label string `json:"label"`
	// Description is an optional help text shown below the field.
	Description string `json:"description,omitempty"`
	// DefaultJSON is the JSON-encoded default value (e.g. "10485760", "true", "[]").
	DefaultJSON string `json:"default_json,omitempty"`
	// Required marks the field as mandatory in SetConfig calls.
	Required bool `json:"required,omitempty"`
	// UIHint guides the host's settings renderer. One of:
	// text, number, checkbox, select, password, textarea.
	// Defaults to a type-appropriate widget if empty.
	UIHint string `json:"ui_hint,omitempty"`
	// EnumValues limits valid values for string fields rendered as a select.
	EnumValues []string `json:"enum_values,omitempty"`
	// Constraints carries optional validation rules.
	Constraints *FieldConstraints `json:"constraints,omitempty"`
	// Children describes the nested schema for object or array-element fields.
	Children []ConfigField `json:"children,omitempty"`
	// Group is an optional grouping label within the settings section.
	Group string `json:"group,omitempty"`
}

// FieldConstraints holds validation rules for a ConfigField.
type FieldConstraints struct {
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	MinLen *int     `json:"min_len,omitempty"`
	MaxLen *int     `json:"max_len,omitempty"`
	// Pattern is a Go regex applied to string values.
	Pattern string `json:"pattern,omitempty"`
}

// ValidateConfigValue validates value against the declared constraints of field.
// value must be JSON-decoded (bool, float64, string, []any, map[string]any).
// Returns nil on success, a descriptive error on failure.
func ValidateConfigValue(field *ConfigField, value any) error {
	if value == nil {
		if field.Required {
			return fmt.Errorf("field %q is required", field.Key)
		}
		return nil
	}
	switch field.Type {
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field %q expects bool, got %T", field.Key, value)
		}
	case "int":
		f, ok := value.(float64)
		if !ok {
			return fmt.Errorf("field %q expects int, got %T", field.Key, value)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
			return fmt.Errorf("field %q: %v is not an integer", field.Key, f)
		}
		if err := checkNumericConstraints(field, f); err != nil {
			return err
		}
	case "float":
		f, ok := value.(float64)
		if !ok {
			return fmt.Errorf("field %q expects float, got %T", field.Key, value)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("field %q expects a finite number", field.Key)
		}
		if err := checkNumericConstraints(field, f); err != nil {
			return err
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("field %q expects string, got %T", field.Key, value)
		}
		if err := checkStringConstraints(field, s); err != nil {
			return err
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("field %q expects array, got %T", field.Key, value)
		}
		if err := checkLengthConstraints(field, len(arr)); err != nil {
			return err
		}
		if len(field.Children) > 0 {
			for i, item := range arr {
				elem := field.Children[0]
				elem.Key = fmt.Sprintf("%s[%d]", field.Key, i)
				if err := ValidateConfigValue(&elem, item); err != nil {
					return err
				}
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("field %q expects object, got %T", field.Key, value)
		}
		if len(field.Children) > 0 {
			if err := ValidateConfig(field.Children, object); err != nil {
				return fmt.Errorf("field %q: %w", field.Key, err)
			}
		}
	default:
		return fmt.Errorf("field %q has unsupported type %q", field.Key, field.Type)
	}
	return nil
}

// ValidateConfig validates all declared fields in schema against the provided
// JSON-decoded values map (map[string]any from json.Unmarshal).
func ValidateConfig(schema []ConfigField, values map[string]any) error {
	for key := range values {
		known := false
		for _, field := range schema {
			if field.Key == key {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown configuration field %q", key)
		}
	}
	for _, field := range schema {
		f := field
		v, ok := values[f.Key]
		if !ok {
			v = nil
		}
		if err := ValidateConfigValue(&f, v); err != nil {
			return err
		}
	}
	return nil
}

// ParseDefaultJSON decodes a field's DefaultJSON into a Go value.
// Returns nil if DefaultJSON is empty.
func ParseDefaultJSON(field *ConfigField) (any, error) {
	if field.DefaultJSON == "" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal([]byte(field.DefaultJSON), &v); err != nil {
		return nil, fmt.Errorf("field %q: invalid default_json: %w", field.Key, err)
	}
	return v, nil
}

func checkNumericConstraints(field *ConfigField, v float64) error {
	if field.Constraints == nil {
		return nil
	}
	if field.Constraints.Min != nil && v < *field.Constraints.Min {
		return fmt.Errorf("field %q: %v is below minimum %v", field.Key, v, *field.Constraints.Min)
	}
	if field.Constraints.Max != nil && v > *field.Constraints.Max {
		return fmt.Errorf("field %q: %v exceeds maximum %v", field.Key, v, *field.Constraints.Max)
	}
	return nil
}

func checkStringConstraints(field *ConfigField, v string) error {
	if err := checkLengthConstraints(field, len(v)); err != nil {
		return err
	}
	if field.Constraints != nil && field.Constraints.Pattern != "" {
		matched, err := regexp.MatchString(field.Constraints.Pattern, v)
		if err != nil {
			return fmt.Errorf("field %q: invalid constraint pattern: %w", field.Key, err)
		}
		if !matched {
			return fmt.Errorf("field %q: %q does not match pattern %q", field.Key, v, field.Constraints.Pattern)
		}
	}
	if len(field.EnumValues) > 0 {
		for _, ev := range field.EnumValues {
			if v == ev {
				return nil
			}
		}
		return fmt.Errorf("field %q: %q is not in allowed values %v", field.Key, v, field.EnumValues)
	}
	return nil
}

func checkLengthConstraints(field *ConfigField, length int) error {
	if field.Constraints == nil {
		return nil
	}
	if field.Constraints.MinLen != nil && length < *field.Constraints.MinLen {
		return fmt.Errorf("field %q: length %d is below minimum %d", field.Key, length, *field.Constraints.MinLen)
	}
	if field.Constraints.MaxLen != nil && length > *field.Constraints.MaxLen {
		return fmt.Errorf("field %q: length %d exceeds maximum %d", field.Key, length, *field.Constraints.MaxLen)
	}
	return nil
}
