package metrics

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// stubMonitorService embeds monitor.Service (left nil) so it satisfies the
// interface, and only overrides the methods the collector actually calls. Any
// unexpected call will panic on the nil embedded interface, which is what we
// want in a test.
type stubMonitorService struct {
	monitor.Service
	findByID     func(ctx context.Context, id string) (*monitor.Model, error)
	findActive   func(ctx context.Context) ([]*monitor.Model, error)
	findByIDHits int32
}

func (s *stubMonitorService) FindByID(ctx context.Context, id string) (*monitor.Model, error) {
	atomic.AddInt32(&s.findByIDHits, 1)
	return s.findByID(ctx, id)
}

func (s *stubMonitorService) FindActive(ctx context.Context) ([]*monitor.Model, error) {
	if s.findActive == nil {
		return nil, nil
	}
	return s.findActive(ctx)
}

func newTestCollector(t *testing.T, svc monitor.Service) *Collector {
	t.Helper()
	logger := zap.NewNop().Sugar()
	return NewCollector(svc, logger)
}

func TestHandleHeartbeatSetsGauges(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return &monitor.Model{ID: id, Name: "API", Type: "http"}, nil
		},
	}
	c := newTestCollector(t, svc)

	hbTime := time.Unix(1700000000, 0)
	c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
		MonitorID: "m1",
		Status:    shared.MonitorStatusUp,
		Ping:      250, // ms
		Time:      hbTime,
	}})

	labels := monitorMeta{name: "API", monitorType: "http"}.labels("m1")
	assert.Equal(t, float64(shared.MonitorStatusUp), testutil.ToFloat64(c.status.With(labels)))
	assert.Equal(t, 1.0, testutil.ToFloat64(c.up.With(labels)))
	assert.InDelta(t, 0.25, testutil.ToFloat64(c.responseTime.With(labels)), 1e-9)
	assert.Equal(t, float64(hbTime.Unix()), testutil.ToFloat64(c.lastHeartbeat.With(labels)))
}

func TestHandleHeartbeatDownReportsUpZero(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return &monitor.Model{ID: id, Name: "DB", Type: "tcp"}, nil
		},
	}
	c := newTestCollector(t, svc)

	c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
		MonitorID: "m2",
		Status:    shared.MonitorStatusDown,
		Ping:      0,
	}})

	labels := monitorMeta{name: "DB", monitorType: "tcp"}.labels("m2")
	assert.Equal(t, float64(shared.MonitorStatusDown), testutil.ToFloat64(c.status.With(labels)))
	assert.Equal(t, 0.0, testutil.ToFloat64(c.up.With(labels)))
}

func TestMetadataIsCached(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return &monitor.Model{ID: id, Name: "API", Type: "http"}, nil
		},
	}
	c := newTestCollector(t, svc)

	for i := 0; i < 3; i++ {
		c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
			MonitorID: "m1",
			Status:    shared.MonitorStatusUp,
			Ping:      100,
		}})
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.findByIDHits), "monitor metadata should be looked up only once")
}

func TestHandleMonitorDeletedRemovesSeries(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return &monitor.Model{ID: id, Name: "API", Type: "http"}, nil
		},
	}
	c := newTestCollector(t, svc)

	c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
		MonitorID: "m1",
		Status:    shared.MonitorStatusUp,
		Ping:      100,
	}})

	labels := monitorMeta{name: "API", monitorType: "http"}.labels("m1")
	assert.Equal(t, 1, testutil.CollectAndCount(c.up), "series should exist before delete")

	c.handleMonitorDeleted(events.Event{Payload: "m1"})

	assert.Equal(t, 0, testutil.CollectAndCount(c.up), "series should be removed after delete")
	// And the cache entry is gone, so a later lookup hits the service again.
	_ = labels
}

func TestHandleMonitorRenamedDropsStaleSeries(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return &monitor.Model{ID: id, Name: "old-name", Type: "http"}, nil
		},
	}
	c := newTestCollector(t, svc)

	c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
		MonitorID: "m1",
		Status:    shared.MonitorStatusUp,
		Ping:      100,
	}})

	// Rename the monitor.
	c.handleMonitorUpdated(events.Event{Payload: &monitor.Model{ID: "m1", Name: "new-name", Type: "http"}})

	// Old series is gone...
	oldLabels := monitorMeta{name: "old-name", monitorType: "http"}.labels("m1")
	assert.Equal(t, 0, testutil.CollectAndCount(c.status), "stale series under old name should be dropped")

	// ...and a fresh heartbeat lands under the new name.
	c.handleHeartbeat(events.Event{Payload: &shared.HeartBeatModel{
		MonitorID: "m1",
		Status:    shared.MonitorStatusUp,
		Ping:      100,
	}})
	newLabels := monitorMeta{name: "new-name", monitorType: "http"}.labels("m1")
	assert.Equal(t, 1.0, testutil.ToFloat64(c.up.With(newLabels)))
	_ = oldLabels
}

func TestSeedPopulatesFromActiveMonitors(t *testing.T) {
	svc := &stubMonitorService{
		findByID: func(_ context.Context, id string) (*monitor.Model, error) {
			return nil, nil
		},
		findActive: func(_ context.Context) ([]*monitor.Model, error) {
			return []*monitor.Model{
				{ID: "m1", Name: "API", Type: "http", Status: shared.MonitorStatusUp},
				{ID: "m2", Name: "DB", Type: "tcp", Status: shared.MonitorStatusDown},
			}, nil
		},
	}
	c := newTestCollector(t, svc)

	c.Seed(context.Background())

	up := monitorMeta{name: "API", monitorType: "http"}.labels("m1")
	down := monitorMeta{name: "DB", monitorType: "tcp"}.labels("m2")
	assert.Equal(t, 1.0, testutil.ToFloat64(c.up.With(up)))
	assert.Equal(t, 0.0, testutil.ToFloat64(c.up.With(down)))
}
