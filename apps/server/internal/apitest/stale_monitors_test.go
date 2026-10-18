package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/stale"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staleMonitorResponse struct {
	ID         string     `json:"id"`
	StaleSince *time.Time `json:"stale_since"`
}

func TestMonitorsAPI_MarksStaleMonitors(t *testing.T) {
	a := newAppWithAdmin(t)
	staleID := a.createMonitor(t, "Checkout", "checkout-push-token")
	freshID := a.createMonitor(t, "Billing", "billing-push-token")
	since := time.Date(2026, 10, 18, 11, 56, 0, 0, time.UTC)
	require.NoError(t, a.container.Invoke(func(rdb *redis.Client) error {
		return stale.NewStore(rdb).Put(context.Background(), stale.Entry{MonitorID: staleID, Since: since, DetectedAt: since.Add(4 * time.Minute)})
	}))

	got := decode[staleMonitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+staleID, nil, a.asAdmin), http.StatusOK)
	require.NotNil(t, got.StaleSince)
	assert.Equal(t, since, got.StaleSince.UTC())

	got = decode[staleMonitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+freshID, nil, a.asAdmin), http.StatusOK)
	assert.Nil(t, got.StaleSince)

	list := decode[[]staleMonitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
	byID := map[string]*time.Time{}
	for _, m := range list {
		byID[m.ID] = m.StaleSince
	}
	require.Contains(t, byID, staleID)
	require.NotNil(t, byID[staleID])
	assert.Equal(t, since, byID[staleID].UTC())
	assert.Nil(t, byID[freshID])
}
