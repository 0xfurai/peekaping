package apitest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

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

// clearQueuedHeartbeats drops the heartbeats waiting for the ingester, so a
// test can inspect the next push's task on its own.
func (a *app) clearQueuedHeartbeats(t *testing.T) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(inspector *asynq.Inspector) error {
		_, err := inspector.DeleteAllPendingTasks("ingester")
		if errors.Is(err, asynq.ErrQueueNotFound) {
			return nil
		}
		return err
	}))
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

// Cluster mode's ingester reads a push as a monitor:ingest task. The task, its options and its
// payload are pinned so that the ingester keeps reading them the same way.
func TestPush_QueuesTheIngesterTaskClusterModeReads(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=down&msg=disk%20full&ping=12", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tasks := a.queuedHeartbeats(t)
	require.Len(t, tasks, 1)
	task := tasks[0]
	assert.Equal(t, "monitor:ingest", task.Type)
	assert.Equal(t, "ingester", task.Queue)
	assert.Equal(t, 3, task.MaxRetry)
	assert.Equal(t, 2*time.Minute, task.Timeout)
	assert.Equal(t, time.Hour, task.Retention)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(task.Payload, &payload))
	pushedAt, err := time.Parse(time.RFC3339Nano, payload["start_time"].(string))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ingest:push:%s:local:%d", monitorID, pushedAt.UnixNano()), task.ID)
	assert.Equal(t, payload["start_time"], payload["end_time"])
	payload["start_time"], payload["end_time"] = "pushed at", "pushed at"
	pinned, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{
		"probe_id": "local",
		"monitor_id": %q,
		"monitor_name": "Nightly backup",
		"monitor_type": "push",
		"monitor_interval": 60,
		"monitor_timeout": 16,
		"monitor_max_retries": 0,
		"monitor_retry_interval": 60,
		"monitor_resend_interval": 0,
		"monitor_location_rule": "any",
		"monitor_config": "{\"pushToken\":\"backup-push-token\"}",
		"status": 0,
		"message": "disk full",
		"ping_ms": 12,
		"start_time": "pushed at",
		"end_time": "pushed at",
		"is_under_maintenance": false,
		"check_cert_expiry": false
	}`, monitorID), string(pinned))
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

func TestPush_RecordsMaintenanceDuringMaintenanceWindow(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")
	body := maintenanceBody("Database upgrade")
	body["monitor_ids"] = []string{monitorID}
	maintenanceID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.asAdmin), http.StatusCreated).ID

	type heartbeat struct {
		Status             int    `json:"status"`
		Message            string `json:"message"`
		PingMs             int    `json:"ping_ms"`
		IsUnderMaintenance bool   `json:"is_under_maintenance"`
	}
	push := func(t *testing.T, query string) heartbeat {
		t.Helper()
		w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?"+query, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		tasks := a.queuedHeartbeats(t)
		require.Len(t, tasks, 1)
		var hb heartbeat
		require.NoError(t, json.Unmarshal(tasks[0].Payload, &hb))
		a.clearQueuedHeartbeats(t)
		return hb
	}

	hb := push(t, "status=down&msg=disk%20full&ping=12")
	assert.Equal(t, int(shared.MonitorStatusMaintenance), hb.Status)
	assert.Equal(t, "Monitor under maintenance", hb.Message)
	assert.Equal(t, 0, hb.PingMs)
	assert.True(t, hb.IsUnderMaintenance)

	// A window that is no longer active stops overriding the pushed status.
	decode[any](t, a.request(t, http.MethodPatch, "/api/v1/maintenances/"+maintenanceID+"/pause", nil, a.asAdmin), http.StatusOK)
	hb = push(t, "status=down&msg=disk%20full&ping=12")
	assert.Equal(t, int(shared.MonitorStatusDown), hb.Status)
	assert.Equal(t, "disk full", hb.Message)
	assert.Equal(t, 12, hb.PingMs)
	assert.False(t, hb.IsUnderMaintenance)
}
