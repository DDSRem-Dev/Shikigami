package plugin

import (
	"errors"
	"os"
	"slices"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/structpb"
)

// refresh reconciles installed artifacts without changing existing durable
// intent. New packages start disabled; a missing or invalid package keeps its
// revision/config/enabled intent but immediately loses process admission.
func (m *Supervisor) refresh() error {
	if m.registry == nil {
		return nil
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.ctx.Err() != nil {
		return connect.NewError(connect.CodeUnavailable, "plugin supervisor stopped")
	}
	scanErr := m.registry.Reload()
	records := m.registry.snapshot()
	state, err := m.store.View()
	if err != nil {
		return err
	}
	var missing []string
	for id := range records {
		if _, exists := state.Buckets["plugins"][id]; !exists {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		if err := m.store.Update(func(state *storage.Snapshot) error {
			for _, id := range missing {
				if _, exists := state.Buckets["plugins"][id]; !exists {
					if err := state.Put("plugins", id, &desiredPlugin{ID: id, Revision: "rev_" + randomID()}); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		state, err = m.store.View()
		if err != nil {
			return err
		}
	}
	var retired []*Instance
	var changed []string
	m.mu.Lock()
	for id := range state.Buckets["plugins"] {
		if builtinID(id) {
			continue
		}
		slot := m.slots[id]
		if slot == nil {
			var want desiredPlugin
			if err := state.Get("plugins", id, &want); err != nil {
				m.mu.Unlock()
				return err
			}
			slot = &supervised{wake: make(chan struct{}, 1), status: &pb.PluginStatus{PluginId: id, Enabled: want.Enabled, Revision: want.Revision, State: "stopped"}}
			m.slots[id] = slot
			if m.started {
				m.done.Add(1)
				go m.run(id, slot)
			}
		}
		record, exists := records[id]
		if !exists {
			record.err = os.ErrNotExist
			if scanErr != nil {
				record.err = scanErr
			}
		}
		if sameArtifact(slot.artifact, record.manifest) && sameArtifactError(slot.artifactErr, record.err) {
			continue
		}
		slot.artifact, slot.artifactErr = record.manifest, record.err
		slot.generation++
		delete(m.manifests, id)
		if record.manifest != nil {
			m.manifests[id] = record.manifest.PluginManifest
		}
		if slot.instance != nil {
			retired = append(retired, slot.instance)
		}
		changed = append(changed, id)
		select {
		case slot.wake <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
	for _, instance := range retired {
		instance.grant.revoke()
	}
	for _, id := range changed {
		if m.options.PluginStopped != nil {
			m.options.PluginStopped(id)
		}
		m.changed(id)
	}
	return scanErr
}

func sameArtifactError(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Error() == b.Error()
}

func (m *Supervisor) artifact(id string) (*Manifest, error, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slot := m.slots[id]; slot != nil {
		return slot.artifact, slot.artifactErr, slot.generation
	}
	return nil, os.ErrNotExist, 0
}

func artifactFailure(err error) (string, string) {
	if errors.Is(err, ErrIncompatible) {
		return "incompatible", "PLUGIN_INCOMPATIBLE"
	}
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return "failed", "PLUGIN_ARTIFACT_MISSING"
	}
	return "failed", "PLUGIN_ARTIFACT_INVALID"
}

func (m *Supervisor) validate(id string, values *structpb.Struct) error {
	if builtinID(id) {
		return m.options.Validate(id, values)
	}
	artifact, err, _ := m.artifact(id)
	if artifact == nil {
		_, reason := artifactFailure(err)
		return connect.NewError(connect.CodeFailedPrecondition, reason)
	}
	if err := sdk.ValidateConfig(artifact.PluginManifest.ConfigSchema, values.AsMap()); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	return nil
}

func clonePluginManifest(manifest *sdk.PluginManifest) *sdk.PluginManifest {
	if manifest == nil {
		return nil
	}
	result := *manifest
	result.Capabilities = slices.Clone(manifest.Capabilities)
	result.Sides = slices.Clone(manifest.Sides)
	result.BinaryFlags = slices.Clone(manifest.BinaryFlags)
	result.ConfigSchema = cloneConfigSchema(manifest.ConfigSchema)
	result.UI.StatusSlots = slices.Clone(manifest.UI.StatusSlots)
	result.UI.DashboardPanels = slices.Clone(manifest.UI.DashboardPanels)
	result.UI.CLICommands = slices.Clone(manifest.UI.CLICommands)
	for i := range result.UI.CLICommands {
		result.UI.CLICommands[i].Flags = slices.Clone(manifest.UI.CLICommands[i].Flags)
	}
	if manifest.UI.SettingsSection != nil {
		section := *manifest.UI.SettingsSection
		result.UI.SettingsSection = &section
	}
	return &result
}

func cloneConfigSchema(schema []sdk.ConfigField) []sdk.ConfigField {
	result := slices.Clone(schema)
	for i := range result {
		result[i].EnumValues = slices.Clone(schema[i].EnumValues)
		result[i].Children = cloneConfigSchema(schema[i].Children)
		if schema[i].Constraints != nil {
			constraints := *schema[i].Constraints
			if constraints.Min != nil {
				value := *constraints.Min
				constraints.Min = &value
			}
			if constraints.Max != nil {
				value := *constraints.Max
				constraints.Max = &value
			}
			if constraints.MinLen != nil {
				value := *constraints.MinLen
				constraints.MinLen = &value
			}
			if constraints.MaxLen != nil {
				value := *constraints.MaxLen
				constraints.MaxLen = &value
			}
			result[i].Constraints = &constraints
		}
	}
	return result
}

func thirdPartyCallbacks(manifest *sdk.PluginManifest) []string {
	// core.scope.v1 includes only this instance's own configuration and log.
	// Other implemented callback permissions must be declared explicitly;
	// static/live scope checks and side restrictions still apply to every call.
	result := []string{"core.config.read", "core.log"}
	for _, capability := range manifest.Capabilities {
		if capability == "core.config.read" || capability == "core.log" {
			continue
		}
		for _, permission := range callbackPermissions {
			if capability == permission {
				result = append(result, capability)
				break
			}
		}
	}
	return result
}
