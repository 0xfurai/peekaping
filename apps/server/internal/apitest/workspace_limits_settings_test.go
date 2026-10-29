package apitest

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/limits"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-160: the install's default limits are install settings (docs/adr/0005 section 5). Each comes
// from its WORKSPACE_LIMIT_* variable when the environment sets it, which locks it, then from the
// value an instance admin saved in Admin → Settings, then none.

// limitSetting is a default limit as GET /admin/settings shows it
type limitSetting struct {
	Value  *int   `json:"value"`
	Source string `json:"source"`
	Locked bool   `json:"locked"`
	EnvVar string `json:"envVar"`
}

// defaultLimitSettings are the default limits of GET /admin/settings, by name
func (a *app) defaultLimitSettings(t *testing.T) map[string]limitSetting {
	t.Helper()
	return decode[struct {
		WorkspaceLimits map[string]limitSetting `json:"workspaceLimits"`
	}](t, a.request(t, http.MethodGet, adminSettingsPath, nil, a.asAdmin), http.StatusOK).WorkspaceLimits
}

// limitVariables are the variables of the default limits, by name
var limitVariables = map[string]string{
	"monitors":           "WORKSPACE_LIMIT_MONITORS",
	"min_check_interval": "WORKSPACE_LIMIT_MIN_CHECK_INTERVAL",
	"status_pages":       "WORKSPACE_LIMIT_STATUS_PAGES",
	"custom_domains":     "WORKSPACE_LIMIT_CUSTOM_DOMAINS",
	"members":            "WORKSPACE_LIMIT_MEMBERS",
	"owned_workspaces":   "WORKSPACE_LIMIT_OWNED_WORKSPACES",
	"probes_per_monitor": "WORKSPACE_LIMIT_PROBES_PER_MONITOR",
	"retention_days":     "WORKSPACE_LIMIT_RETENTION_DAYS",
}

func TestLimits_Settings_AnInstallThatSetsNothingShowsEveryDefaultLimitAsNone(t *testing.T) {
	a := newAppWithAdmin(t)

	got := a.defaultLimitSettings(t)

	require.Len(t, got, len(limitVariables))
	for name, envVar := range limitVariables {
		assert.Equal(t, limitSetting{Value: nil, Source: "default", EnvVar: envVar}, got[name], name)
	}
}

func TestLimits_Settings_EachDefaultLimitIsSavedAndClearedBackToNone(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(30))
	saved := map[string]int{
		"monitors": 10, "min_check_interval": 60, "status_pages": 2, "custom_domains": 3,
		"members": 4, "owned_workspaces": 5, "probes_per_monitor": 6, "retention_days": 36500,
	}
	body := map[string]any{}
	for name, value := range saved {
		body[name] = value
	}

	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": body}), http.StatusOK)

	got := a.defaultLimitSettings(t)
	for name, value := range saved {
		assert.Equal(t, limitSetting{Value: limitOf(value), Source: "saved", EnvVar: limitVariables[name]}, got[name], name)
	}

	// A limit left out stays, and null clears one
	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"monitors": nil, "members": 7}}), http.StatusOK)
	got = a.defaultLimitSettings(t)
	assert.Equal(t, limitSetting{Value: nil, Source: "default", EnvVar: "WORKSPACE_LIMIT_MONITORS"}, got["monitors"])
	assert.Equal(t, limitOf(7), got["members"].Value)
	assert.Equal(t, limitOf(2), got["status_pages"].Value, "a limit left out stays as it is")
	assert.Equal(t, "default", a.installSettings(t).PublicURL.Source, "and nothing else changed")
}

func TestLimits_Settings_TheEnvironmentWinsAndLocksADefaultLimit(t *testing.T) {
	a := newAppWithAdmin(t, withLimits(func(cfg *config.Config) {
		cfg.WorkspaceLimitMonitors = 10
		cfg.WorkspaceLimitMembers = 0
	}), withEnvironment("WORKSPACE_LIMIT_MONITORS", "WORKSPACE_LIMIT_MEMBERS"))

	got := a.defaultLimitSettings(t)
	assert.Equal(t, limitSetting{Value: limitOf(10), Source: "environment", Locked: true, EnvVar: "WORKSPACE_LIMIT_MONITORS"}, got["monitors"])
	assert.Equal(t, limitSetting{Value: nil, Source: "environment", Locked: true, EnvVar: "WORKSPACE_LIMIT_MEMBERS"}, got["members"], "0 in the environment is no limit, and locked")
	assert.False(t, got["status_pages"].Locked)

	for name, body := range map[string]map[string]any{
		"set":   {"monitors": 20},
		"clear": {"members": nil},
	} {
		refused := settingRefusal(t, a.putSettings(t, map[string]any{"workspaceLimits": body}), http.StatusConflict)
		assert.Equal(t, "SETTING_LOCKED", refused.Error.Code, name)
		assert.Contains(t, refused.Message, "set by the server's environment", name)
	}
	refused := settingRefusal(t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"status_pages": 3, "monitors": 20}}), http.StatusConflict)
	assert.Equal(t, "workspaceLimits.monitors", refused.Error.Field)
	assert.Nil(t, a.defaultLimitSettings(t)["status_pages"].Value, "a refused change stores nothing")
	assert.Equal(t, limitOf(10), a.defaultLimitSettings(t)["monitors"].Value)
}

func TestLimits_Settings_AnInvalidDefaultLimitIsRefusedNamingIt(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"zero":                         {map[string]any{"monitors": 0}, "workspaceLimits.monitors"},
		"negative":                     {map[string]any{"members": -1}, "workspaceLimits.members"},
		"below MIN_CHECK_INTERVAL":     {map[string]any{"min_check_interval": 59}, "workspaceLimits.min_check_interval"},
		"no retention":                 {map[string]any{"retention_days": 0}, "workspaceLimits.retention_days"},
		"retention of over a century":  {map[string]any{"retention_days": 36501}, "workspaceLimits.retention_days"},
		"probes of zero":               {map[string]any{"probes_per_monitor": 0}, "workspaceLimits.probes_per_monitor"},
		"a valid one beside a bad one": {map[string]any{"status_pages": 2, "custom_domains": -5}, "workspaceLimits.custom_domains"},
	} {
		t.Run(name, func(t *testing.T) {
			refused := settingRefusal(t, a.putSettings(t, map[string]any{"workspaceLimits": tc.body}), http.StatusBadRequest)
			assert.Equal(t, "INVALID_SETTING", refused.Error.Code)
			assert.Equal(t, tc.field, refused.Error.Field)
			assert.NotEmpty(t, refused.Message)
		})
	}
	assert.Nil(t, a.defaultLimitSettings(t)["status_pages"].Value, "nothing was saved")

	// MIN_CHECK_INTERVAL itself is allowed
	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"min_check_interval": 60, "retention_days": 1}}), http.StatusOK)
	assert.Equal(t, limitOf(60), a.defaultLimitSettings(t)["min_check_interval"].Value)
}

// A saved default applies at once to every workspace without limits of its own, and a workspace's
// own limits still replace the defaults whole
func TestLimits_Settings_ASavedDefaultAppliesToAWorkspaceWithoutItsOwnLimits(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		decode[created](t, a.postPage(t, "one"), http.StatusCreated)

		decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"status_pages": 1, "members": 3}}), http.StatusOK)

		assertLimitReached(t, a.postPage(t, "two"), "status_pages", 1)
		report := decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK)
		assert.Equal(t, "install", report.Source)
		assert.Equal(t, limitOf(1), report.Limits["status_pages"].Max)
		assert.Equal(t, limitOf(3), report.Limits["members"].Max)
		listed := a.adminLimitsOf(t, a.firstWorkspace(t))
		assert.Equal(t, "install", listed.Source)
		assert.Equal(t, limitOf(1), listed.Values["status_pages"])

		// The workspace's own limits win
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{StatusPages: limitOf(2)})
		decode[created](t, a.postPage(t, "two"), http.StatusCreated)
		report = decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK)
		assert.Equal(t, "workspace", report.Source)
		assert.Nil(t, report.Limits["members"].Max, "its own limits replace the defaults whole")

		// Cleared, the default is none again
		a.setOwnLimits(t, a.firstWorkspace(t), nil)
		decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"status_pages": nil, "members": nil}}), http.StatusOK)
		decode[created](t, a.postPage(t, "three"), http.StatusCreated)
		assert.Equal(t, "none", a.adminLimitsOf(t, a.firstWorkspace(t)).Source)
	})
}

// A saved default reaches another api replica within the install settings' cache lifetime, with
// no restart
func TestLimits_Settings_AnotherReplicaGoesByASavedDefaultWithinTheCacheLifetime(t *testing.T) {
	first := newAppWithAdmin(t)
	replica := newReplica(t, first, withSettingsCacheLifetime(200*time.Millisecond))
	decode[created](t, replica.postPage(t, "one"), http.StatusCreated)

	decode[installSettings](t, first.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"status_pages": 1}}), http.StatusOK)

	tries := 0
	assert.Eventually(t, func() bool {
		tries++
		return replica.postPage(t, fmt.Sprintf("try-%d", tries)).Code == http.StatusForbidden
	}, 5*time.Second, 50*time.Millisecond)
}
