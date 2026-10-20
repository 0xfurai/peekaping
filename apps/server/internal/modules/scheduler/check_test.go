package scheduler

import (
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestScheduler_APushMonitorsCheckSeesItsLatestPush(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) {
		cfg.Executors = executorsByType{"push": executor.NewPushExecutor(zap.NewNop().Sugar())}
	})
	h.db.put(monitorWith("pushed", withInterval(60), ofType("push")))
	h.db.put(monitorWith("silent", withInterval(60), ofType("push")))
	h.db.setLastHeartbeat(shared.HeartBeatModel{MonitorID: "pushed", Status: shared.MonitorStatusUp, Time: time.Now().Add(-10 * time.Second)})

	h.start()

	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) >= 1 }, "the silent monitor is reported")
	time.Sleep(200 * time.Millisecond)
	results := h.ingest.ingested()
	require.Len(t, results, 1, "a push that came in time needs no heartbeat")
	assert.Equal(t, "silent", results[0].MonitorID)
	assert.Equal(t, shared.MonitorStatusDown, results[0].Status)
	assert.Equal(t, "No push received yet", results[0].Message)
}
