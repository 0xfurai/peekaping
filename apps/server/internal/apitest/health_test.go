package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/stale"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type healthResponse struct {
	Status        string     `json:"status"`
	StaleMonitors *int64     `json:"stale_monitors"`
	WatchdogRanAt *time.Time `json:"watchdog_ran_at"`
}

func getHealth(t *testing.T, a *app, path string) healthResponse {
	t.Helper()
	w := a.request(t, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body healthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body
}

func TestHealth_BeforeTheWatchdogRan(t *testing.T) {
	a := newApp(t)

	for _, path := range []string{"/health", "/api/v1/health"} {
		body := getHealth(t, a, path)
		assert.Equal(t, "success", body.Status, path)
		require.NotNil(t, body.StaleMonitors, path)
		assert.Zero(t, *body.StaleMonitors, path)
		assert.Nil(t, body.WatchdogRanAt, path)
	}
}

func TestHealth_ReportsStaleMonitors(t *testing.T) {
	a := newApp(t)
	ranAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	require.NoError(t, a.container.Invoke(func(rdb *redis.Client) error {
		ctx := context.Background()
		store := stale.NewRedisStore(rdb)
		for _, id := range []string{"mon-1", "mon-2"} {
			if err := store.Put(ctx, stale.Entry{MonitorID: id, Since: ranAt.Add(-time.Hour), DetectedAt: ranAt}); err != nil {
				return err
			}
		}
		return store.SetRanAt(ctx, ranAt)
	}))

	// An external watcher reads it without credentials, and it stays 200 for the docker healthcheck
	body := getHealth(t, a, "/api/v1/health")

	assert.Equal(t, "success", body.Status)
	require.NotNil(t, body.StaleMonitors)
	assert.Equal(t, int64(2), *body.StaleMonitors)
	require.NotNil(t, body.WatchdogRanAt)
	assert.Equal(t, ranAt, body.WatchdogRanAt.UTC())
}
