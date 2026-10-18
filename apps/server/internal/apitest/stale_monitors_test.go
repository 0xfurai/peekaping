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

	// A monitor that isn't stale leaves the field out rather than sending null
	fresh := a.request(t, http.MethodGet, "/api/v1/monitors/"+freshID, nil, a.asAdmin)
	got = decode[staleMonitorResponse](t, fresh, http.StatusOK)
	assert.Nil(t, got.StaleSince)
	assert.NotContains(t, fresh.Body.String(), `"stale_since"`)

	listResponse := a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin)
	assert.NotContains(t, listResponse.Body.String(), `"stale_since":null`)
	list := decode[[]staleMonitorResponse](t, listResponse, http.StatusOK)
	byID := map[string]*time.Time{}
	for _, m := range list {
		byID[m.ID] = m.StaleSince
	}
	require.Contains(t, byID, staleID)
	require.NotNil(t, byID[staleID])
	assert.Equal(t, since, byID[staleID].UTC())
	assert.Nil(t, byID[freshID])
}

func TestMonitorsAPI_ListsMonitorsWhenStaleRecordsCannotBeRead(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	// A string under the hash's key makes every read of it fail with WRONGTYPE
	require.NoError(t, a.container.Invoke(func(rdb *redis.Client) error {
		return rdb.Set(context.Background(), stale.MonitorsKey, "not a hash", 0).Err()
	}))

	got := decode[staleMonitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, id, got.ID)
	assert.Nil(t, got.StaleSince)

	list := decode[[]staleMonitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Nil(t, list[0].StaleSince)
}
