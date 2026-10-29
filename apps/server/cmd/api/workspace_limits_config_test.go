package main

import (
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-160: the install's default limits of a workspace, one variable each, unset by default
var workspaceLimitVariables = map[string]func(*config.Config) int{
	"WORKSPACE_LIMIT_MONITORS":           func(c *config.Config) int { return c.WorkspaceLimitMonitors },
	"WORKSPACE_LIMIT_MIN_CHECK_INTERVAL": func(c *config.Config) int { return c.WorkspaceLimitMinCheckInterval },
	"WORKSPACE_LIMIT_STATUS_PAGES":       func(c *config.Config) int { return c.WorkspaceLimitStatusPages },
	"WORKSPACE_LIMIT_CUSTOM_DOMAINS":     func(c *config.Config) int { return c.WorkspaceLimitCustomDomains },
	"WORKSPACE_LIMIT_MEMBERS":            func(c *config.Config) int { return c.WorkspaceLimitMembers },
	"WORKSPACE_LIMIT_OWNED_WORKSPACES":   func(c *config.Config) int { return c.WorkspaceLimitOwnedWorkspaces },
	"WORKSPACE_LIMIT_PROBES_PER_MONITOR": func(c *config.Config) int { return c.WorkspaceLimitProbesPerMonitor },
	"WORKSPACE_LIMIT_RETENTION_DAYS":     func(c *config.Config) int { return c.WorkspaceLimitRetentionDays },
}

func TestWorkspaceLimitsAreUnsetByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	for variable := range workspaceLimitVariables {
		t.Setenv(variable, "")
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	for variable, read := range workspaceLimitVariables {
		assert.Zero(t, read(cfg.ToInternalConfig()), variable)
	}

	for variable, read := range workspaceLimitVariables {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "7")
			cfg, err := LoadAndValidate(t.TempDir())
			require.NoError(t, err)
			assert.Equal(t, 7, read(cfg.ToInternalConfig()))

			t.Setenv(variable, "-1")
			_, err = LoadAndValidate(t.TempDir())
			assert.Error(t, err, "a negative limit stops startup")
		})
	}
}
