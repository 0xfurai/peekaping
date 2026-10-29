package limits

import (
	"context"

	"peekaping/internal/config"
)

// InstallDefaults are the limits of a workspace that has none of its own. The environment
// variables stand behind it today; Admin → Settings can later (PP-373).
type InstallDefaults interface {
	Defaults(ctx context.Context) (Limits, error)
}

// EnvDefaults are the install's defaults as the WORKSPACE_LIMIT_* environment variables set them
type EnvDefaults Limits

// Defaults returns the limits the variables set. A variable left unset, or set to 0, is no limit.
func (d EnvDefaults) Defaults(context.Context) (Limits, error) {
	return Limits(d).normalized(), nil
}

// FromConfig reads the install's defaults from its settings
func FromConfig(cfg *config.Config) EnvDefaults {
	if cfg == nil {
		return EnvDefaults{}
	}
	return EnvDefaults{
		Monitors:         Of(cfg.WorkspaceLimitMonitors),
		MinCheckInterval: Of(cfg.WorkspaceLimitMinCheckInterval),
		StatusPages:      Of(cfg.WorkspaceLimitStatusPages),
		CustomDomains:    Of(cfg.WorkspaceLimitCustomDomains),
		Members:          Of(cfg.WorkspaceLimitMembers),
		OwnedWorkspaces:  Of(cfg.WorkspaceLimitOwnedWorkspaces),
		ProbesPerMonitor: Of(cfg.WorkspaceLimitProbesPerMonitor),
		RetentionDays:    Of(cfg.WorkspaceLimitRetentionDays),
	}
}
