package producer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/infra"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/worker"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Regression test for monitors that stopped checking after a timeout (PEE-15):
// the asynq task deadline equalled the check timeout, so a hanging target made
// asynq archive the task, and the archived task's ID blocked every later check.
func TestHealthCheckAgainstHangingTarget(t *testing.T) {
	var hanging atomic.Bool
	hanging.Store(true)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hanging.Load() {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	mr := miniredis.RunT(t)
	redisOpt := asynq.RedisClientOpt{Addr: mr.Addr()}
	inspector := asynq.NewInspector(redisOpt)
	logger := zap.NewNop().Sugar()
	queueSvc := infra.ProvideQueueService(asynq.NewClient(redisOpt), inspector, logger)
	defer queueSvc.Close()

	// An install upgraded from an affected version starts with an archived task
	// holding the monitor's task ID; it must be replaced, not block the monitor
	seedClient := asynq.NewClient(redisOpt)
	_, err := seedClient.Enqueue(asynq.NewTask(worker.TaskTypeHealthCheck, []byte(`{}`)),
		asynq.Queue(healthCheckQueue), asynq.TaskID(healthCheckTaskID("mon-hanging")), asynq.MaxRetry(0))
	require.NoError(t, err)
	require.NoError(t, seedClient.Close())
	require.NoError(t, inspector.ArchiveTask(healthCheckQueue, healthCheckTaskID("mon-hanging")))

	execRegistry := executor.NewExecutorRegistry(logger)
	handler := worker.NewHealthCheckTaskHandler(
		execRegistry,
		supervisor.NewHealthCheck(nil, execRegistry, logger),
		ingester.NewQueueService(queueSvc),
		logger,
	)
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TaskTypeHealthCheck, handler.ProcessTask)
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 2,
		Queues:      map[string]int{healthCheckQueue: 1},
		LogLevel:    asynq.FatalLevel,
	})
	require.NoError(t, srv.Start(mux))
	defer srv.Shutdown()

	mon := &monitor.Model{
		ID:       "mon-hanging",
		Name:     "Hanging target",
		Type:     "http",
		Active:   true,
		Interval: 20,
		Timeout:  1,
		Config: fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none",
			"max_redirects": 10, "accepted_statuscodes": ["2XX"]}`, target.URL),
	}
	mockMonitorSvc := new(MockMonitorService)
	mockMonitorSvc.On("FindByID", mock.Anything, mon.ID).Return(mon, nil)
	mockMaintenanceSvc := new(MockMaintenanceService)
	mockMaintenanceSvc.On("GetMaintenancesByMonitorID", mock.Anything, mon.ID).Return([]*maintenance.Model{}, nil)

	producer := &Producer{
		logger: logger,
		loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
		runner: NewQueueRunner(queueSvc, logger),
	}

	// A queue that was never written to doesn't exist yet and reports as empty
	queueCounts := func(qname string) (pending, active, archived int) {
		info, err := inspector.GetQueueInfo(qname)
		if err != nil {
			return 0, 0, 0
		}
		return info.Pending, info.Active, info.Archived
	}

	// Each round is one scheduler tick: the producer enqueues, the worker runs the check.
	expected := []shared.MonitorStatus{
		shared.MonitorStatusDown,
		shared.MonitorStatusDown,
		shared.MonitorStatusDown,
		shared.MonitorStatusUp,
	}
	for round, status := range expected {
		hanging.Store(status == shared.MonitorStatusDown)

		interval, err := producer.processMonitor(context.Background(), mon.ID, time.Now().UnixMilli())
		require.NoError(t, err)
		require.Equal(t, mon.Interval, interval)

		require.Eventuallyf(t, func() bool {
			ingested, _, _ := queueCounts("ingester")
			pending, active, _ := queueCounts(healthCheckQueue)
			return ingested == round+1 && pending == 0 && active == 0
		}, 10*time.Second, 50*time.Millisecond, "round %d: check result never reached the ingester", round)

		_, _, archived := queueCounts(healthCheckQueue)
		assert.Zerof(t, archived, "round %d: health check task was archived", round)
	}

	tasks, err := inspector.ListPendingTasks("ingester")
	require.NoError(t, err)
	got := make([]shared.MonitorStatus, 0, len(tasks))
	for _, task := range tasks {
		var payload ingester.Result
		require.NoError(t, json.Unmarshal(task.Payload, &payload))
		got = append(got, payload.Status)
	}
	assert.ElementsMatch(t, expected, got)
}
