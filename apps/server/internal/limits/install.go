package limits

import (
	"context"

	"peekaping/internal/installsettings"
)

// InstallDefaults are the limits of a workspace that has none of its own
type InstallDefaults interface {
	Defaults(ctx context.Context) (Limits, error)
}

// FixedDefaults are defaults that never change, for a test that has no install settings
type FixedDefaults Limits

// Defaults returns the limits. A value of 0 or less is no limit.
func (d FixedDefaults) Defaults(context.Context) (Limits, error) {
	return Limits(d).normalized(), nil
}

// SettingsDefaults are the install's defaults as install settings (installsettings, docs/adr/0005
// section 5): each limit is its WORKSPACE_LIMIT_* variable when the environment sets it, else the
// value an instance admin saved in Admin → Settings, else none. A saved change takes effect within
// the install settings' cache lifetime, with no restart.
type SettingsDefaults struct {
	Settings *installsettings.Resolver
}

func (d SettingsDefaults) Defaults(ctx context.Context) (Limits, error) {
	resolved, err := d.Settings.DefaultLimits(ctx)
	if err != nil {
		return Limits{}, err
	}
	return Limits{
		Monitors:         resolved.Monitors.Value,
		MinCheckInterval: resolved.MinCheckInterval.Value,
		StatusPages:      resolved.StatusPages.Value,
		CustomDomains:    resolved.CustomDomains.Value,
		Members:          resolved.Members.Value,
		OwnedWorkspaces:  resolved.OwnedWorkspaces.Value,
		ProbesPerMonitor: resolved.ProbesPerMonitor.Value,
		RetentionDays:    resolved.RetentionDays.Value,
	}.normalized(), nil
}
