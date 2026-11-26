package apitest

import (
	"fmt"
	"net/http"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Metrics, the usage ping, push and badges run without a signed-in user, so no workspace is in
// their context. They act for the whole install: these tests put a monitor in each of two
// workspaces and check that each of them reaches both.

// twoWorkspaces is an app with a monitor of the admin's workspace and one of another user's
type twoWorkspaces struct {
	*app
	bob          credentials
	mine, theirs string // the IDs of the monitors of the admin and of Bob
}

func newTwoWorkspaces(t *testing.T, options ...func(*config.Config)) twoWorkspaces {
	t.Helper()
	a := newAppWithAdmin(t, options...)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	mine := a.createMonitor(t, "Alice's", "alice-token")
	theirs := decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Bob's", "bob-token"), bob), http.StatusCreated).ID
	require.NotEqual(t, a.workspaceOf(t, mine), a.workspaceOf(t, theirs))
	return twoWorkspaces{app: a, bob: bob, mine: mine, theirs: theirs}
}

func TestMonitorWorkspace_MetricsListTheMonitorsOfEveryWorkspace(t *testing.T) {
	a := newTwoWorkspaces(t, withMetrics)

	first := a.createHeartbeat(t, a.mine, shared.MonitorStatusUp, 100)
	second := a.createHeartbeat(t, a.theirs, shared.MonitorStatusDown, 0)

	a.eventuallySeries(t, fmt.Sprintf(`
peekaping_monitor_status{monitor_id=%[1]q,monitor_name="Alice's",monitor_type="push"} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id=%[1]q,monitor_name="Alice's",monitor_type="push"} %[2]d
peekaping_monitor_response_seconds{monitor_id=%[1]q,monitor_name="Alice's",monitor_type="push"} 0.1
peekaping_monitor_status{monitor_id=%[3]q,monitor_name="Bob's",monitor_type="push"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id=%[3]q,monitor_name="Bob's",monitor_type="push"} %[4]d
`, a.mine, first, a.theirs, second))
}

// The ping reports the number of monitors as a bucket, so the test uses the edge of one: six
// monitors in each workspace make twelve, which is not in the bucket either count falls in
func TestMonitorWorkspace_TheUsagePingCountsTheMonitorsOfEveryWorkspace(t *testing.T) {
	a := newTwoWorkspaces(t)
	require.Equal(t, "1-10", a.telemetry(t).Payload.Monitors, "one in each")
	for i := range 5 {
		a.createMonitor(t, fmt.Sprintf("Alice's %d", i), fmt.Sprintf("alice-more-%d", i))
		decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody(fmt.Sprintf("Bob's %d", i), fmt.Sprintf("bob-more-%d", i)), a.bob), http.StatusCreated)
	}
	assert.Equal(t, "11-50", a.telemetry(t).Payload.Monitors, "six and six are twelve in all, which neither workspace holds alone")
}

func TestMonitorWorkspace_APushToAnotherWorkspacesTokenLandsOnItsMonitor(t *testing.T) {
	a := newTwoWorkspaces(t)

	w := a.request(t, http.MethodGet, "/api/v1/push/bob-token?status=up&msg=hello&ping=7", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	beats := a.storedHeartbeats(t, a.theirs)
	require.Len(t, beats, 1)
	assert.Equal(t, "hello", beats[0].Msg)
	assert.Empty(t, a.storedHeartbeats(t, a.mine))
	assert.Len(t, decode[[]map[string]any](t, a.request(t, http.MethodGet, monitorsPath+"/"+a.theirs+"/heartbeats", nil, a.bob), http.StatusOK), 1)
}

func TestMonitorWorkspace_ABadgeServesAMonitorOfAnotherWorkspaceOnItsPublishedPage(t *testing.T) {
	a := newTwoWorkspaces(t)
	decode[created](t, a.request(t, http.MethodPost, "/api/v1/status-pages", map[string]any{
		"slug": "bobs", "title": "Bob's status", "published": true, "monitor_ids": []string{a.theirs},
	}, a.bob), http.StatusCreated)

	for path, title := range badgePaths(a.theirs) {
		t.Run(path, func(t *testing.T) {
			assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
		})
	}
	// the admin's monitor is on no published page, so it has no badge
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/badge/"+a.mine+"/status", nil).Code)
}
