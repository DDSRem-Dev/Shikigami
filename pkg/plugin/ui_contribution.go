// Package plugin — UIContribution and related types describing which UI
// surfaces a plugin contributes to. Declared in manifest.json under "ui".
package plugin

// UIContribution describes all UI surfaces a plugin contributes to.
// All fields are optional; a plugin with no UI contributions is valid.
type UIContribution struct {
	// StatusSlots are compact inline status indicators in the status bar /
	// connection view. Plugins should use at most one "primary" slot.
	StatusSlots []UIStatusSlot `json:"status_slots,omitempty"`
	// DashboardPanels are full panels shown in the dashboard view.
	DashboardPanels []UIDashboardPanel `json:"dashboard_panels,omitempty"`
	// SettingsSection declares the plugin's settings page section.
	// The host renders this section from ConfigSchema.
	SettingsSection *UISettingsSection `json:"settings_section,omitempty"`
	// CLICommands are subcommands injected under `shiki plugins <plugin-id>`.
	CLICommands []UICLICommand `json:"cli_commands,omitempty"`
}

// UIStatusSlot describes an inline status slot in the status bar.
type UIStatusSlot struct {
	// SlotID identifies the slot: "primary", "secondary", or "notifications".
	SlotID string `json:"slot_id"`
	// EventType is the event type whose payload is rendered in this slot.
	// Convention: "plugin.status.<plugin-id>.v1"
	EventType string `json:"event_type"`
	// Label is a short prefix shown before the event text (e.g. "Sync").
	Label string `json:"label"`
}

// UIDashboardPanel describes a panel in the dashboard view.
type UIDashboardPanel struct {
	// PanelID is a unique identifier for the panel (e.g. "sync-overview").
	PanelID string `json:"panel_id"`
	// Title is the panel heading.
	Title string `json:"title"`
	// EventType is the event type whose payload is rendered in this panel.
	// Convention: "plugin.panel.<plugin-id>.v1"
	EventType string `json:"event_type"`
	// Priority determines ordering when multiple plugins contribute panels.
	// Lower values appear first. 0 means unspecified.
	Priority int32 `json:"priority,omitempty"`
	// Layout hints how the panel should be sized: "compact", "full", "wide".
	Layout string `json:"layout,omitempty"`
}

// UISettingsSection declares the plugin's section in the global settings page.
// The host renders this section using ConfigSchema from the manifest.
type UISettingsSection struct {
	// SectionID is a unique identifier used as an anchor in the settings UI.
	SectionID string `json:"section_id"`
	// Title is the section heading.
	Title string `json:"title"`
}

// UICLICommand declares a CLI subcommand injected under
// `shiki plugins <plugin-id> <name>`.
type UICLICommand struct {
	// Name is the subcommand name (e.g. "conflicts", "resolve").
	Name string `json:"name"`
	// Description is the short help text shown in --help output.
	Description string `json:"description"`
	// ActionID identifies the action handler in the plugin implementation.
	ActionID string `json:"action_id"`
	// Flags declares the flags accepted by this command.
	Flags []UICLIFlag `json:"flags,omitempty"`
}

// UICLIFlag describes a single flag on a CLI command.
type UICLIFlag struct {
	// Name is the long flag name without the leading "--".
	Name string `json:"name"`
	// Shorthand is the optional single-character shorthand (without "-").
	Shorthand string `json:"shorthand,omitempty"`
	// Type is one of: string, bool, int.
	Type string `json:"type"`
	// Default is the string representation of the default value.
	Default string `json:"default,omitempty"`
	// Description is the flag help text.
	Description string `json:"description"`
}

// StatusPayload is the JSON shape of "plugin.status.v1" events published
// via CoreAPI.PublishEvent. Plugins SHOULD use this shape for status_payload
// fields so the host can render the slot uniformly.
type StatusPayload struct {
	// Text is the display text shown in the status slot (e.g. "23 files synced").
	Text string `json:"text"`
	// State is one of: ok, warning, error, idle, syncing, loading.
	State string `json:"state"`
	// Icon is an optional symbol prefix (uses terminal-safe single characters).
	Icon string `json:"icon,omitempty"`
	// Detail is optional expanded text shown on hover or in expanded view.
	Detail string `json:"detail,omitempty"`
}

// NotifyPayload is the JSON shape of "plugin.notify.v1" events. The host
// renders these as in-app notifications in TUI/GUI frontends.
type NotifyPayload struct {
	// Level is one of: info, success, warning, error.
	Level string `json:"level"`
	// Title is the notification heading.
	Title string `json:"title"`
	// Message is the notification body.
	Message string `json:"message"`
	// ActionID is an optional action to invoke when the notification is clicked.
	ActionID string `json:"action_id,omitempty"`
}
