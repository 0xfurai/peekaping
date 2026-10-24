package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessTask_SendsTheLocationRuleToTheIngester(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer target.Close()
	task, err := json.Marshal(HealthCheckTaskPayload{
		MonitorID: "mon-1", MonitorName: "Test", MonitorType: "http", Interval: 60, Timeout: 5,
		LocationRule: shared.LocationRuleAtLeast, LocationThreshold: 2,
		Config:      fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none", "accepted_statuscodes": ["2XX"]}`, target.URL),
		ScheduledAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	results := &fakeIngester{}

	require.NoError(t, newTestHandler(results).ProcessTask(context.Background(), asynq.NewTask(TaskTypeHealthCheck, task)))

	require.Len(t, results.ingested, 1)
	assert.Equal(t, shared.LocationRuleAtLeast, results.ingested[0].MonitorLocationRule)
	assert.Equal(t, 2, results.ingested[0].MonitorLocationThreshold)
}

func TestHealthCheckTaskPayload_ATaskQueuedBeforeRulesExistedIsAny(t *testing.T) {
	var payload HealthCheckTaskPayload
	require.NoError(t, json.Unmarshal([]byte(`{"monitor_id":"mon-1","monitor_type":"http","interval":60}`), &payload))

	assert.Empty(t, payload.LocationRule)
	assert.Zero(t, payload.LocationThreshold)

	encoded, err := json.Marshal(HealthCheckTaskPayload{MonitorID: "mon-1"})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "location", "a monitor on the default rule adds nothing to the task")
}
