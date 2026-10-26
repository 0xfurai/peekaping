package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/probe"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type availabilityPeriod struct {
	Status string    `json:"status"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
}

type availabilityConnection struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Open  bool      `json:"open"`
}

type availabilityInstance struct {
	ID          string                   `json:"id"`
	Hostname    string                   `json:"hostname"`
	Version     string                   `json:"version"`
	Connections []availabilityConnection `json:"connections"`
}

type probeAvailability struct {
	ProbeID       string                 `json:"probe_id"`
	Range         string                 `json:"range"`
	From          time.Time              `json:"from"`
	To            time.Time              `json:"to"`
	InProcess     bool                   `json:"in_process"`
	TrackedFrom   *time.Time             `json:"tracked_from"`
	UptimePercent *float64               `json:"uptime_percent"`
	Periods       []availabilityPeriod   `json:"periods"`
	Instances     []availabilityInstance `json:"instances"`
}

const availabilityPath = probesPath + "/availability"

// seedProbeHistory replaces the probe's history with offline three hours ago and online two hours ago
func (a *app) seedProbeHistory(t *testing.T, probeID string) (offlineAt, onlineAt time.Time) {
	t.Helper()
	// Whole seconds: the database keeps microseconds, and Linux clocks give nanoseconds
	now := time.Now().UTC().Truncate(time.Second)
	offlineAt, onlineAt = now.Add(-3*time.Hour), now.Add(-2*time.Hour)
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		ctx := context.Background()
		if _, err := db.ExecContext(ctx, "DELETE FROM probe_status_changes WHERE probe_id = ?", probeID); err != nil {
			return err
		}
		for _, c := range []struct {
			status string
			at     time.Time
		}{{"offline", offlineAt}, {"online", onlineAt}} {
			if _, err := db.ExecContext(ctx, "INSERT INTO probe_status_changes (id, probe_id, status, at, recorded_at) VALUES (?, ?, ?, ?, ?)", uuid.NewString(), probeID, c.status, c.at, c.at); err != nil {
				return err
			}
		}
		return nil
	}))
	return offlineAt, onlineAt
}

func TestProbeAvailabilityAPI_OneProbeWithItsInstances(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	offlineAt, onlineAt := a.seedProbeHistory(t, made.ID)
	require.NoError(t, a.container.Invoke(func(svc probe.Service) error {
		return svc.RegisterInstance(context.Background(), made.ID, &probe.Instance{ID: "instance-1", Hostname: "fra-1", Version: "1.4.0", LastSeenAt: time.Now().UTC()})
	}))

	got := decode[probeAvailability](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID+"/availability", nil, a.withAPIKey), http.StatusOK)

	assert.Equal(t, made.ID, got.ProbeID)
	assert.Equal(t, "24h", got.Range, "24h is the default")
	assert.False(t, got.InProcess)
	assert.WithinDuration(t, time.Now(), got.To, time.Minute)
	assert.Equal(t, 24*time.Hour, got.To.Sub(got.From))
	require.NotNil(t, got.TrackedFrom)
	assert.True(t, offlineAt.Equal(*got.TrackedFrom), "tracked from the first change: %s", got.TrackedFrom)
	require.Len(t, got.Periods, 2)
	assert.Equal(t, "offline", got.Periods[0].Status)
	assert.True(t, offlineAt.Equal(got.Periods[0].Start))
	assert.True(t, onlineAt.Equal(got.Periods[0].End))
	assert.Equal(t, "online", got.Periods[1].Status)
	assert.True(t, got.Periods[1].Start.Equal(got.Periods[0].End), "one period runs into the next")
	assert.True(t, got.Periods[1].End.Equal(got.To), "the last one ends now")
	require.NotNil(t, got.UptimePercent)
	assert.InDelta(t, 200.0/3, *got.UptimePercent, 0.01, "two of the three hours it has been tracked")
	require.Len(t, got.Instances, 1)
	assert.Equal(t, "instance-1", got.Instances[0].ID)
	assert.Equal(t, "fra-1", got.Instances[0].Hostname)
	assert.Equal(t, "1.4.0", got.Instances[0].Version)
	require.Len(t, got.Instances[0].Connections, 1)
	assert.True(t, got.Instances[0].Connections[0].Open, "it was seen just now")

	week := decode[probeAvailability](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID+"/availability?range=7d", nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "7d", week.Range)
	assert.Equal(t, 7*24*time.Hour, week.To.Sub(week.From))
	require.Len(t, week.Instances, 1, "the instances do not depend on the range")
}

func TestProbeAvailabilityAPI_ListsEveryProbeWithoutInstances(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	a.seedProbeHistory(t, made.ID)
	require.NoError(t, a.container.Invoke(func(svc probe.Service) error {
		return svc.RegisterInstance(context.Background(), made.ID, &probe.Instance{ID: "instance-1", LastSeenAt: time.Now().UTC()})
	}))

	w := a.request(t, http.MethodGet, availabilityPath+"?range=30d", nil, a.withAPIKey)
	list := decode[[]probeAvailability](t, w, http.StatusOK)

	require.Len(t, list, 2)
	assert.Equal(t, "local", list[0].ProbeID, "the local probe comes first")
	assert.False(t, list[0].InProcess, "the API server under test is a cluster's")
	assert.Equal(t, made.ID, list[1].ProbeID)
	assert.Equal(t, "30d", list[1].Range)
	assert.Len(t, list[1].Periods, 2)
	assert.Empty(t, list[1].Instances)
	assert.NotContains(t, w.Body.String(), "instance")
	require.NotNil(t, list[1].UptimePercent)
}

func TestProbeAvailabilityAPI_TheListRouteIsNotAProbeCalledAvailability(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createProbe(t, map[string]any{"name": "availability"})

	list := decode[[]probeAvailability](t, a.request(t, http.MethodGet, availabilityPath, nil, a.asAdmin), http.StatusOK)

	assert.Len(t, list, 2)
}

func TestProbeAvailabilityAPI_AnUnknownProbeIsNotFound(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodGet, probesPath+"/"+uuid.NewString()+"/availability", nil, a.asAdmin)

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, "Probe not found", errorMessage(t, w.Body.Bytes()))
}

func TestProbeAvailabilityAPI_NeedsCredentials(t *testing.T) {
	a := newAppWithAdmin(t)
	for _, path := range []string{availabilityPath, probesPath + "/local/availability"} {
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, path, nil).Code, path)
	}
}

func TestProbeAvailabilityAPI_RefusesARangeItDoesNotKnow(t *testing.T) {
	a := newAppWithAdmin(t)
	for _, path := range []string{availabilityPath, probesPath + "/local/availability"} {
		for _, bad := range []string{"1h", "90d", "24H", "week"} {
			w := a.request(t, http.MethodGet, path+"?range="+bad, nil, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, w.Code, path+" "+bad+": "+w.Body.String())
		}
		for _, good := range []string{"24h", "7d", "30d"} {
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path+"?range="+good, nil, a.asAdmin).Code, path+" "+good)
		}
	}
}

func TestProbeAvailabilityAPI_AProbeWithNoHistoryHasNullUptime(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("DELETE FROM probe_status_changes WHERE probe_id = ?", made.ID)
		return err
	}))

	w := a.request(t, http.MethodGet, probesPath+"/"+made.ID+"/availability", nil, a.asAdmin)
	got := decode[probeAvailability](t, w, http.StatusOK)

	assert.Nil(t, got.TrackedFrom)
	assert.Nil(t, got.UptimePercent)
	assert.Contains(t, w.Body.String(), `"tracked_from":null`)
	assert.Contains(t, w.Body.String(), `"uptime_percent":null`)
	assert.Contains(t, w.Body.String(), `"periods":[]`)
	assert.Contains(t, w.Body.String(), `"instances":[]`)
}
