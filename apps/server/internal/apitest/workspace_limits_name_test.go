package apitest

import (
	"net/http"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

// PP-163: the install's name for its default limits (WORKSPACE_LIMITS_NAME, "Free tier" on the
// cloud) is an install setting. The environment wins and locks it, else the value an instance admin
// saved in Admin → Settings, else none. GET /workspace/limits gives it to the workspace's Usage page.

// nameOf is a name as GET /workspace/limits gives it
func nameOf(name string) *string { return &name }

// limitsName is the name of the default limits as GET /admin/settings shows it
func (a *app) limitsName(t *testing.T) settingValue[string] {
	t.Helper()
	return decode[struct {
		WorkspaceLimitsName settingValue[string] `json:"workspaceLimitsName"`
	}](t, a.request(t, http.MethodGet, adminSettingsPath, nil, a.asAdmin), http.StatusOK).WorkspaceLimitsName
}

// reportedLimitsName is installLimitsName of GET /workspace/limits, for the admin
func (a *app) reportedLimitsName(t *testing.T) *string {
	t.Helper()
	return decode[struct {
		InstallLimitsName *string `json:"installLimitsName"`
	}](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK).InstallLimitsName
}

func TestLimitsName_AnInstallThatSetsNothingHasNone(t *testing.T) {
	a := newAppWithAdmin(t)

	assert.Equal(t, settingValue[string]{Value: "", Source: "default", EnvVar: "WORKSPACE_LIMITS_NAME"}, a.limitsName(t))
	assert.Nil(t, a.reportedLimitsName(t), "no name is null")
}

func TestLimitsName_IsSavedAndClearedBackToNone(t *testing.T) {
	a := newAppWithAdmin(t)

	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimitsName": "  Free tier "}), http.StatusOK)

	assert.Equal(t, settingValue[string]{Value: "Free tier", Source: "saved", EnvVar: "WORKSPACE_LIMITS_NAME"}, a.limitsName(t))
	assert.Equal(t, nameOf("Free tier"), a.reportedLimitsName(t))

	// Saving something else leaves it, and an empty string clears it
	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimits": map[string]any{"monitors": 5}}), http.StatusOK)
	assert.Equal(t, "Free tier", a.limitsName(t).Value)
	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimitsName": ""}), http.StatusOK)
	assert.Equal(t, settingValue[string]{Value: "", Source: "default", EnvVar: "WORKSPACE_LIMITS_NAME"}, a.limitsName(t))
	assert.Nil(t, a.reportedLimitsName(t))
}

func TestLimitsName_TheEnvironmentWinsAndLocksIt(t *testing.T) {
	a := newAppWithAdmin(t, func(cfg *config.Config) { cfg.WorkspaceLimitsName = "Free tier" }, withEnvironment("WORKSPACE_LIMITS_NAME"))

	assert.Equal(t, settingValue[string]{Value: "Free tier", Source: "environment", Locked: true, EnvVar: "WORKSPACE_LIMITS_NAME"}, a.limitsName(t))
	assert.Equal(t, nameOf("Free tier"), a.reportedLimitsName(t))

	refused := settingRefusal(t, a.putSettings(t, map[string]any{"workspaceLimitsName": "Hobby"}), http.StatusConflict)
	assert.Equal(t, "SETTING_LOCKED", refused.Error.Code)
	assert.Equal(t, "workspaceLimitsName", refused.Error.Field)
	assert.Equal(t, "Free tier", a.limitsName(t).Value)
}

func TestLimitsName_AnInvalidNameIsRefused(t *testing.T) {
	a := newAppWithAdmin(t)

	for name, value := range map[string]string{
		"a line break": "Free\ntier",
		"too long":     strings.Repeat("a", 101),
	} {
		t.Run(name, func(t *testing.T) {
			refused := settingRefusal(t, a.putSettings(t, map[string]any{"workspaceLimitsName": value}), http.StatusBadRequest)
			assert.Equal(t, "INVALID_SETTING", refused.Error.Code)
			assert.Equal(t, "workspaceLimitsName", refused.Error.Field)
		})
	}
	assert.Equal(t, "default", a.limitsName(t).Source, "nothing was saved")
}

// A plain member reads the name with the workspace's limits
func TestLimitsName_EveryMemberReadsIt(t *testing.T) {
	a := newAppWithAdmin(t)
	decode[installSettings](t, a.putSettings(t, map[string]any{"workspaceLimitsName": "Free tier"}), http.StatusOK)
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)

	got := decode[struct {
		InstallLimitsName *string `json:"installLimitsName"`
	}](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, member), http.StatusOK)

	assert.Equal(t, nameOf("Free tier"), got.InstallLimitsName)
}
