package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

// PP-161: a workspace's plan can cap its data retention period (limit retention_days)

func (a *app) saveRetention(t *testing.T, days string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPut, retentionPath, map[string]any{"value": days, "type": "int"}, a.asAdmin)
}

func (a *app) retention(t *testing.T) string {
	t.Helper()
	return decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK).Value
}

// AC1: a period longer than the cap is refused with LIMIT_REACHED, and one at the cap is saved,
// from the install's defaults and from the workspace's own limits
func TestLimits_RetentionDays_APeriodLongerThanTheCapIsRefused(t *testing.T) {
	a := newAppWithAdmin(t, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitRetentionDays = 30 }))

	assertLimitReached(t, a.saveRetention(t, "31"), "retention_days", 30)
	assert.Equal(t, "180", a.retention(t), "the refused save changed nothing")

	decode[settingResponse](t, a.saveRetention(t, "30"), http.StatusOK)
	assert.Equal(t, "30", a.retention(t))
	decode[settingResponse](t, a.saveRetention(t, "7"), http.StatusOK)
	assert.Equal(t, "7", a.retention(t))

	// The workspace's own limits replace the install's
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{RetentionDays: limitOf(10)})
	assertLimitReached(t, a.saveRetention(t, "11"), "retention_days", 10)
	decode[settingResponse](t, a.saveRetention(t, "10"), http.StatusOK)
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{RetentionDays: limitOf(90)})
	decode[settingResponse](t, a.saveRetention(t, "90"), http.StatusOK)
	assert.Equal(t, "90", a.retention(t))
}

// AC4: without a cap the settings API takes any period it took before, up to 100 years, and a
// workspace given no limits of its own has none, whatever the install's defaults say
func TestLimits_RetentionDays_WithoutACapNothingChanges(t *testing.T) {
	a := newAppWithAdmin(t)
	decode[settingResponse](t, a.saveRetention(t, "36500"), http.StatusOK)
	assert.Equal(t, http.StatusBadRequest, a.saveRetention(t, "36501").Code, "the 100 years bound stays a 400")

	capped := newAppWithAdmin(t, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitRetentionDays = 30 }))
	capped.setOwnLimits(t, capped.firstWorkspace(t), &limits.Limits{})
	decode[settingResponse](t, capped.saveRetention(t, "36500"), http.StatusOK)
	assert.Equal(t, "36500", capped.retention(t))
}

// A member is refused for their role before the cap counts
func TestLimits_RetentionDays_AMembersSaveIsRefusedForTheRoleFirst(t *testing.T) {
	a := newAppWithAdmin(t, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitRetentionDays = 30 }))
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)

	w := a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "31", "type": "int"}, member)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "LIMIT_REACHED")
}
