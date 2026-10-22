package ingester

import (
	"context"
	"encoding/json"
	"testing"

	"peekaping/internal/reporting/reportingtest"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// panickingService panics when it stores a result, as a bug in the ingest would
type panickingService struct{}

func (panickingService) Ingest(context.Context, *Result) error { panic("ingest failed") }

func TestIngester_ReportsAPanicInAnIngestTask(t *testing.T) {
	recorder := reportingtest.Install(t)
	server, client := reportingtest.NewAsynq(t)
	logger := zap.NewNop().Sugar()
	ing := NewIngester(server, NewIngesterTaskHandler(panickingService{}, logger), logger)
	require.NoError(t, ing.Start(context.Background()))
	t.Cleanup(ing.Stop)
	payload, err := json.Marshal(Result{MonitorID: "mon-1"})
	require.NoError(t, err)

	_, err = client.Enqueue(asynq.NewTask(TaskTypeIngester, payload), asynq.MaxRetry(0))
	require.NoError(t, err)

	events := recorder.Wait(t, 1)
	assert.Equal(t, "asynq", events[0].Tags["component"])
	assert.Equal(t, "string", events[0].Exception[0].Type)
}
