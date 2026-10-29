package scheduler

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// recordingPushExecutor is the push executor, recording what each of its checks returned
type recordingPushExecutor struct {
	*executor.PushExecutor
	mu       sync.Mutex
	returned map[string][]*executor.Result
}

func (e *recordingPushExecutor) Execute(ctx context.Context, m *executor.Monitor, proxy *executor.Proxy) *executor.Result {
	result := e.PushExecutor.Execute(ctx, m, proxy)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.returned[m.ID] = append(e.returned[m.ID], result)
	return result
}

// returnedFor is what the monitor's checks returned, oldest first
func (e *recordingPushExecutor) returnedFor(monitorID string) []*executor.Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.returned[monitorID])
}

func TestScheduler_APushMonitorsCheckSeesItsLatestPush(t *testing.T) {
	t.Parallel()
	push := &recordingPushExecutor{PushExecutor: executor.NewPushExecutor(zap.NewNop().Sugar()), returned: map[string][]*executor.Result{}}
	h := newHarness(t, func(cfg *Config) { cfg.Executors = executorsByType{"push": push} })
	h.db.put(monitorWith("pushed", withInterval(60), ofType("push")))
	h.db.put(monitorWith("silent", withInterval(60), ofType("push")))
	h.db.setLastHeartbeat(shared.HeartBeatModel{MonitorID: "pushed", Status: shared.MonitorStatusUp, Time: time.Now().Add(-10 * time.Second)})

	h.start()

	// Both are checked at the start, and again on the next whole minute: a start just before
	// one checks them twice
	eventually(t, 2*time.Second, func() bool { return len(push.returnedFor("pushed")) >= 1 }, "the pushed monitor is checked")
	eventually(t, 2*time.Second, func() bool { return h.ingestedFor("silent") >= 1 }, "the silent monitor is reported")
	for _, result := range push.returnedFor("pushed") {
		assert.Nil(t, result, "a push that came in time needs no heartbeat")
	}
	assert.Zero(t, h.ingestedFor("pushed"), "a push that came in time needs no heartbeat")
	for _, result := range h.ingest.ingested() {
		if result.MonitorID == "silent" {
			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, "No push received yet", result.Message)
		}
	}
}

func TestScheduler_IngestsACheckWithTheMonitorsSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.certExpiry = true
	h.db.put(shared.Monitor{
		ID:             "mon",
		Name:           "Example",
		Type:           "http",
		Interval:       day,
		Timeout:        16,
		MaxRetries:     3,
		RetryInterval:  30,
		ResendInterval: 5,
		Config:         `{"url":"https://example.com","check_cert_expiry":true}`,
		Active:         true,
	})
	h.exec.run = func(context.Context, checkCall) *executor.Result { return down("503 - Service Unavailable") }

	h.start()

	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "the check is ingested")
	got := h.ingest.ingested()[0]
	assert.Equal(t, "mon", got.MonitorID)
	assert.Equal(t, "Example", got.MonitorName)
	assert.Equal(t, "http", got.MonitorType)
	assert.Equal(t, day, got.MonitorInterval)
	assert.Equal(t, 16, got.MonitorTimeout)
	assert.Equal(t, 3, got.MonitorMaxRetries)
	assert.Equal(t, 30, got.MonitorRetryInt)
	assert.Equal(t, 5, got.MonitorResendInt)
	assert.Equal(t, `{"url":"https://example.com","check_cert_expiry":true}`, got.MonitorConfig)
	assert.Equal(t, shared.MonitorStatusDown, got.Status)
	assert.Equal(t, "503 - Service Unavailable", got.Message)
	assert.False(t, got.IsUnderMaintenance)
	assert.True(t, got.CheckCertExpiry, "the certificate's expiry is checked when the monitor asks for it")
}

// PP-331: in single mode the peekaping process runs the check, so no probe instance made it
func TestScheduler_IngestsACheckWithNoProbeInstance(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))

	h.start()

	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "the check is ingested")
	got := h.ingest.ingested()[0]
	assert.Empty(t, got.InstanceID)
}

func TestScheduler_IngestsAMonitorUnderMaintenanceAsMaintenanceWithoutCheckingIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.maintained = true
	h.db.put(monitorWith("mon", withInterval(day)))

	h.start()

	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "the result is ingested")
	got := h.ingest.ingested()[0]
	assert.Equal(t, shared.MonitorStatusMaintenance, got.Status)
	assert.True(t, got.IsUnderMaintenance)
	assert.Zero(t, h.exec.count(), "the target isn't checked")
}

// ingestFunc is an ingester.Service made of a function
type ingestFunc func(ctx context.Context, r *ingester.Result) error

func (f ingestFunc) Ingest(ctx context.Context, r *ingester.Result) error { return f(ctx, r) }

func TestScheduler_IngestsAMonitorsChecksInOrderWithItsPushes(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var ingested []string
	pushing := make(chan struct{})
	release := make(chan struct{})
	// Pushes and the scheduler's checks share one in-order ingester, as single mode wires them
	ordered := ingest.NewInOrderService(ingestFunc(func(_ context.Context, r *ingester.Result) error {
		mu.Lock()
		ingested = append(ingested, r.Message)
		mu.Unlock()
		if r.Message == "push" {
			close(pushing)
			<-release
		}
		return nil
	}))
	messages := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(ingested)
	}
	h := newHarness(t, func(cfg *Config) { cfg.Ingester = ordered })
	h.db.put(monitorWith("mon", withInterval(day)))
	go func() {
		assert.NoError(t, ordered.Ingest(context.Background(), &ingester.Result{MonitorID: "mon", Message: "push"}))
	}()
	<-pushing

	h.start()

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the monitor is checked")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, []string{"push"}, messages(), "the check's result waits while the push is ingested")

	close(release)
	eventually(t, 2*time.Second, func() bool { return len(messages()) == 2 }, "the check's result is ingested")
	assert.Equal(t, []string{"push", "check 1"}, messages())
}
