package apitest

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// storedHeartbeats are the heartbeats stored for the monitor, oldest first. A cluster api on
// SQLite or PostgreSQL stores a push or a probe's result itself, on the replica that took the
// request.
func (a *app) storedHeartbeats(t *testing.T, monitorID string) []*heartbeat.Model {
	t.Helper()
	var beats []*heartbeat.Model
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service) error {
		var err error
		beats, err = heartbeats.FindByMonitorIDPaginated(context.Background(), monitorID, 1000, 0, nil, false)
		return err
	}))
	slices.SortStableFunc(beats, func(x, y *heartbeat.Model) int { return x.Time.Compare(y.Time) })
	return beats
}

// hasHeartbeats reports whether any heartbeat is stored, of any monitor
func (a *app) hasHeartbeats(t *testing.T) bool {
	t.Helper()
	var any bool
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service) error {
		var err error
		any, err = heartbeats.HasAny(context.Background())
		return err
	}))
	return any
}

// withQueuedIngest has the api ingest as it did when the producer, worker and ingester served
// MongoDB: a push or a result is enqueued for the ingester, and stored by it. The api stores them
// itself. Only the soak test of the ingester queue (ingest_rate_soak_test.go) uses it.
func withQueuedIngest(container *dig.Container) error {
	return container.Decorate(func(q queue.Service) ingester.Service { return ingester.NewPushQueueService(q) })
}

// queuedHeartbeats returns the heartbeats waiting for the ingester. Only an api that ingests through
// the queue (withQueuedIngest) queues any.
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

func TestPush_StoresAHeartbeatForAPushMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=down&msg=disk%20full&ping=12", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok":"true"}`, w.Body.String())
	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 1, "stored by the replica that took the push, before it answered")
	assert.Equal(t, shared.MonitorStatusDown, beats[0].Status)
	assert.Equal(t, "disk full", beats[0].Msg)
	assert.Equal(t, 12, beats[0].Ping)
	assert.Equal(t, shared.LocalProbeID, beats[0].ProbeID)
	assert.Empty(t, a.queuedHeartbeats(t), "nothing is queued for an ingester")
}

func TestPush_IgnoresMalformedResponseTime(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?ping=fast", nil)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 1)
	assert.Equal(t, shared.MonitorStatusUp, beats[0].Status)
	assert.Equal(t, 0, beats[0].Ping)
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
	assert.Empty(t, a.storedHeartbeats(t, paused))
}

func TestPush_RecordsMaintenanceDuringMaintenanceWindow(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")
	body := maintenanceBody("Database upgrade")
	body["monitor_ids"] = []string{monitorID}
	maintenanceID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.asAdmin), http.StatusCreated).ID

	// push sends a push and returns the heartbeat it stored
	push := func(t *testing.T, query string) *heartbeat.Model {
		t.Helper()
		before := len(a.storedHeartbeats(t, monitorID))
		w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?"+query, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		beats := a.storedHeartbeats(t, monitorID)
		require.Len(t, beats, before+1)
		return beats[len(beats)-1]
	}

	hb := push(t, "status=down&msg=disk%20full&ping=12")
	assert.Equal(t, shared.MonitorStatusMaintenance, hb.Status)
	assert.Equal(t, "Monitor under maintenance", hb.Msg)
	assert.Equal(t, 0, hb.Ping)

	// A window that is no longer active stops overriding the pushed status.
	decode[any](t, a.request(t, http.MethodPatch, "/api/v1/maintenances/"+maintenanceID+"/pause", nil, a.asAdmin), http.StatusOK)
	hb = push(t, "status=down&msg=disk%20full&ping=12")
	assert.Equal(t, shared.MonitorStatusDown, hb.Status)
	assert.Equal(t, "disk full", hb.Msg)
	assert.Equal(t, 12, hb.Ping)
}
