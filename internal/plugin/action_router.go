package plugin

import (
	"context"
	"fmt"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

// ActionRouter routes InvokePluginAction calls to the correct Supervisor instance.
type ActionRouter struct {
	settings    *SettingsRegistry
	supervisors func(pluginID string) *Supervisor
}

// NewActionRouter creates an ActionRouter backed by the given settings registry
// and supervisor-lookup function.
func NewActionRouter(settings *SettingsRegistry, supervisors func(string) *Supervisor) *ActionRouter {
	return &ActionRouter{settings: settings, supervisors: supervisors}
}

// InvokeAction finds the running plugin instance and calls its InvokeAction.
// pluginID identifies the plugin; req carries the action details.
func (r *ActionRouter) InvokeAction(ctx context.Context, pluginID string, req *sdk.ActionRequest) (*sdk.ActionResult, error) {
	if r.settings.Get(pluginID) == nil {
		return nil, fmt.Errorf("plugin %q is not registered", pluginID)
	}
	sup := r.supervisors(pluginID)
	if sup == nil {
		return nil, fmt.Errorf("plugin %q has no supervisor", pluginID)
	}
	var result *sdk.ActionResult
	if err := sup.Call(ctx, pluginID, sdk.UIActionCapability, func(ctx context.Context, lc sdk.Lifecycle) error {
		var err error
		result, err = lc.InvokeAction(ctx, req)
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}
