package producer

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/worker"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// t0 is the Redis time most engine tests start at. It's 30s into a minute, so
// a 60s monitor's next aligned slot is t0+30s.
var t0 = time.Date(2026, 1, 1, 12, 0, 30, 0, time.UTC)

func ms(t time.Time) float64 { return float64(t.UnixMilli()) }

// monitorStore is an in-memory monitor database. Like both repositories, it
// pages active monitors by offset, highest ID first (ORDER BY id DESC).
type monitorStore struct {
	mu       sync.Mutex
	monitors map[string]*monitor.Model
	findErrs map[string]error
	// afterPage runs after FindActivePaginated or FindActiveByProbe reads a page, to change the
	// database between pages
	afterPage func(page int)
}

func newMonitorStore(monitors ...*monitor.Model) *monitorStore {
	s := &monitorStore{monitors: map[string]*monitor.Model{}, findErrs: map[string]error{}}
	for _, m := range monitors {
		s.put(m)
	}
	return s
}

func (s *monitorStore) put(m *monitor.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := *m
	s.monitors[m.ID] = &copied
}

func (s *monitorStore) setActive(id string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.monitors[id].Active = active
}

// failNextFind makes the next FindByID of the monitor fail with err.
func (s *monitorStore) failNextFind(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findErrs[id] = err
}

func (s *monitorStore) FindByID(_ context.Context, id string) (*monitor.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.findErrs[id]; ok {
		delete(s.findErrs, id)
		return nil, err
	}
	m, ok := s.monitors[id]
	if !ok {
		return nil, nil
	}
	copied := *m
	return &copied, nil
}

// FindActivePaginated pages every active monitor, whichever probe runs it, as the watchdog reads them
func (s *monitorStore) FindActivePaginated(_ context.Context, page int, limit int) ([]*monitor.Model, error) {
	return s.pageActive(page, limit, func(*monitor.Model) bool { return true }), nil
}

// FindActiveByProbe pages the active monitors the probe runs
func (s *monitorStore) FindActiveByProbe(_ context.Context, probeID string, page int, limit int) ([]*monitor.Model, error) {
	return s.pageActive(page, limit, func(m *monitor.Model) bool { return m.RunsOn(probeID) }), nil
}

func (s *monitorStore) pageActive(page int, limit int, keep func(*monitor.Model) bool) []*monitor.Model {
	s.mu.Lock()
	var active []*monitor.Model
	for _, m := range s.monitors {
		if m.Active && keep(m) {
			copied := *m
			active = append(active, &copied)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID > active[j].ID })
	start := min(page*limit, len(active))
	end := min(start+limit, len(active))
	result := active[start:end]
	afterPage := s.afterPage
	s.mu.Unlock()

	if afterPage != nil {
		afterPage(page)
	}
	return result
}

// engine is a producer running against miniredis, with the real asynq queue
// service and an in-memory monitor database.
type engine struct {
	mr        *miniredis.Miniredis
	rdb       *redis.Client
	inspector *asynq.Inspector
	monitors  *monitorStore
	producer  *Producer
}

// newEngine is a producer that has its lifetime, as Start gives it, but runs nothing: a test
// starts the part it covers
func newEngine(t *testing.T, monitors ...*monitor.Model) *engine {
	t.Helper()
	e := newUnstartedEngine(t, monitors...)
	require.NoError(t, e.producer.begin(context.Background()))
	t.Cleanup(func() { e.producer.cancel() })
	return e
}

// newUnstartedEngine is a producer that Start hasn't started
func newUnstartedEngine(t *testing.T, monitors ...*monitor.Model) *engine {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(t0)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	redisOpt := asynq.RedisClientOpt{Addr: mr.Addr()}
	inspector := asynq.NewInspector(redisOpt)
	logger := zap.NewNop().Sugar()
	queueSvc := infra.ProvideQueueService(asynq.NewClient(redisOpt), inspector, logger)
	t.Cleanup(func() { _ = queueSvc.Close() })

	maintenanceSvc := new(MockMaintenanceService)
	maintenanceSvc.On("GetMaintenancesByMonitorID", mock.Anything, mock.Anything).Return([]*maintenance.Model{}, nil)

	store := newMonitorStore(monitors...)
	p := NewProducer(rdb, NewQueueRunner(queueSvc, logger), store, check.NewLoader(store, new(MockProxyService), maintenanceSvc, nil, logger),
		nil, nil, nil, nil, &config.Config{ProducerConcurrency: 1}, logger)
	p.reclaimInterval = 5 * time.Millisecond

	return &engine{mr: mr, rdb: rdb, inspector: inspector, monitors: store, producer: p}
}

// startJobProcessing starts the claim loop and the reclaimer, and stops them
// when the test ends.
func (e *engine) startJobProcessing(t *testing.T) {
	t.Helper()
	require.NoError(t, e.producer.startJobProcessing())
	t.Cleanup(e.producer.Stop)
}

// waitForGoroutines waits until every goroutine the wait group tracks has exited.
func waitForGoroutines(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	stopped := make(chan struct{})
	go func() {
		wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutines kept running")
	}
}

func (e *engine) addDue(t *testing.T, id string, at time.Time) {
	t.Helper()
	require.NoError(t, e.rdb.ZAdd(context.Background(), SchedDueKey, redis.Z{Score: ms(at), Member: id}).Err())
}

func (e *engine) addLease(t *testing.T, id string, expires time.Time) {
	t.Helper()
	require.NoError(t, e.rdb.ZAdd(context.Background(), SchedLeaseKey, redis.Z{Score: ms(expires), Member: id}).Err())
}

// score returns the monitor's score in the set, and whether it's there.
func (e *engine) score(key, id string) (float64, bool) {
	score, err := e.rdb.ZScore(context.Background(), key, id).Result()
	return score, err == nil
}

func (e *engine) members(t *testing.T, key string) map[string]float64 {
	t.Helper()
	zs, err := e.rdb.ZRangeWithScores(context.Background(), key, 0, -1).Result()
	require.NoError(t, err)
	out := make(map[string]float64, len(zs))
	for _, z := range zs {
		out[z.Member.(string)] = z.Score
	}
	return out
}

// eventuallyDueAt waits until the monitor sits in the due set at the given time.
func (e *engine) eventuallyDueAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		score, ok := e.score(SchedDueKey, id)
		return ok && score == ms(at)
	}, 5*time.Second, 2*time.Millisecond, "%s never became due at %s", id, at)
}

// queuedCheck returns the monitor's queued check task and its payload.
func (e *engine) queuedCheck(t *testing.T, monitorID string) (*asynq.TaskInfo, worker.HealthCheckTaskPayload) {
	t.Helper()
	info, err := e.inspector.GetTaskInfo(healthCheckQueue, healthCheckTaskID(monitorID))
	require.NoError(t, err, "no check queued for %s", monitorID)
	var payload worker.HealthCheckTaskPayload
	require.NoError(t, json.Unmarshal(info.Payload, &payload))
	return info, payload
}

// takeCheck removes the monitor's queued check, as a worker picking it up would.
func (e *engine) takeCheck(t *testing.T, monitorID string) {
	t.Helper()
	require.NoError(t, e.inspector.DeleteTask(healthCheckQueue, healthCheckTaskID(monitorID)))
}

func (e *engine) noQueuedCheck(t *testing.T, monitorID string) {
	t.Helper()
	_, err := e.inspector.GetTaskInfo(healthCheckQueue, healthCheckTaskID(monitorID))
	// A queue that was never written to doesn't exist yet
	require.Truef(t, errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound),
		"%s has a queued check (err: %v)", monitorID, err)
}
