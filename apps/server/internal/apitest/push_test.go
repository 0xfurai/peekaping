package apitest

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queuedHeartbeats returns the heartbeats waiting for the ingester.
func (a *app) queuedHeartbeats(t *testing.T) []*asynq.TaskInfo {
	t.Helper()
	var tasks []*asynq.TaskInfo
	require.NoError(t, a.container.Invoke(func(inspector *asynq.Inspector) error {
		var err error
		tasks, err = inspector.ListPendingTasks("ingester")
		if errors.Is(err, asynq.ErrQueueNotFound) {
			return nil
		}
		return err
	}))
	return tasks
}

func TestPush_QueuesHeartbeatForPushMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=down&msg=disk%20full&ping=12", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok":"true"}`, w.Body.String())
	tasks := a.queuedHeartbeats(t)
	require.Len(t, tasks, 1)
	assert.Equal(t, "monitor:ingest", tasks[0].Type)
	var heartbeat struct {
		MonitorID string `json:"monitor_id"`
		Status    int    `json:"status"`
		Message   string `json:"message"`
		PingMs    int    `json:"ping_ms"`
	}
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &heartbeat))
	assert.Equal(t, monitorID, heartbeat.MonitorID)
	assert.Equal(t, 0, heartbeat.Status)
	assert.Equal(t, "disk full", heartbeat.Message)
	assert.Equal(t, 12, heartbeat.PingMs)
}

func TestPush_IgnoresMalformedResponseTime(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?ping=fast", nil)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tasks := a.queuedHeartbeats(t)
	require.Len(t, tasks, 1)
	var heartbeat struct {
		Status int `json:"status"`
		PingMs int `json:"ping_ms"`
	}
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &heartbeat))
	assert.Equal(t, 1, heartbeat.Status)
	assert.Equal(t, 0, heartbeat.PingMs)
}

func TestPush_RefusesUnknownAndPausedTokens(t *testing.T) {
	a := newAppWithAdmin(t)
	paused := a.createMonitor(t, "Paused backup", "paused-push-token")
	a.pauseMonitor(t, paused)

	for path, status := range map[string]int{
		"/api/v1/push/not-a-push-token":  http.StatusNotFound,
		"/api/v1/push/paused-push-token": http.StatusBadRequest,
	} {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			assert.Equal(t, status, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), "Paused backup")
			assert.NotContains(t, w.Body.String(), paused)
		})
	}
	assert.Empty(t, a.queuedHeartbeats(t))
}
