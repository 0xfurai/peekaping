// Package scheduler is single mode's replacement for the producer, the queue and the worker:
// it keeps a timer for each active monitor and runs the checks that fall due in a bounded
// pool, in the same process, then ingests their results directly. Cluster mode keeps its
// producer and worker. See ADR 0002, section 4.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/infra"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"
)

// Loader loads what a check of a monitor needs. check.Loader is one.
type Loader interface {
	Load(ctx context.Context, monitorID string) (*check.Job, error)
}

// MonitorFinder is the part of the monitor service the scheduler reads
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error)
}

// Executors finds the executor for a monitor type. executor.ExecutorRegistry is one.
type Executors interface {
	GetExecutor(monitorType string) (executor.Executor, bool)
}

// Config is what a Scheduler reads and calls
type Config struct {
	Loader     Loader
	Monitors   MonitorFinder
	Executors  Executors
	Supervisor *healthcheck.HealthCheckSupervisor
	// Ingester takes the results of the checks. The scheduler ingests a monitor's results one
	// at a time, in order. Pushes to a push monitor must go through the same
	// ingester.NewInOrderService to be ordered with its checks.
	Ingester ingester.Service
	// Concurrency is how many checks run at the same time; DefaultConcurrency when it's zero
	Concurrency int
	// ReconcileInterval is how often the timers are compared with the active monitors in
	// the database; DefaultReconcileInterval when it's zero
	ReconcileInterval time.Duration
	Logger            *zap.SugaredLogger
}

const (
	// DefaultConcurrency is the size of the check pool when none is configured
	DefaultConcurrency = 128
	// DefaultReconcileInterval is how often the scheduler reconciles its timers with the database
	DefaultReconcileInterval = 30 * time.Second

	// pageSize is how many active monitors are read at a time
	pageSize = 100
	// ingestTimeout bounds one result's ingest, which a cancelled check doesn't interrupt
	ingestTimeout = 2 * time.Minute
)

// Scheduler checks the active monitors when their check intervals pass. It implements
// stale.Rechecker.
type Scheduler struct {
	loader            Loader
	monitors          MonitorFinder
	executors         Executors
	supervisor        *healthcheck.HealthCheckSupervisor
	ingester          ingester.Service
	reconcileInterval time.Duration
	logger            *zap.SugaredLogger

	// syncMu serializes reading a monitor from the database and changing its schedule, so
	// that events handled at the same time leave the schedule as the last read found the monitor
	syncMu sync.Mutex

	mu        sync.Mutex
	schedules map[string]*schedule
	ctx       context.Context // the scheduler's lifetime; nil until it starts
	cancel    context.CancelFunc
	stopped   bool
	wg        sync.WaitGroup
}

// schedule is the timer of one monitor
type schedule struct {
	interval time.Duration
	timer    *time.Timer
}

// New creates a scheduler
func New(cfg Config) *Scheduler {
	reconcileInterval := cfg.ReconcileInterval
	if reconcileInterval <= 0 {
		reconcileInterval = DefaultReconcileInterval
	}
	return &Scheduler{
		loader:            cfg.Loader,
		monitors:          cfg.Monitors,
		executors:         cfg.Executors,
		supervisor:        cfg.Supervisor,
		ingester:          ingester.NewInOrderService(cfg.Ingester),
		reconcileInterval: reconcileInterval,
		logger:            cfg.Logger.With("component", "scheduler"),
		schedules:         make(map[string]*schedule),
	}
}

// nextAligned is the next multiple of the period after the time, counted from the Unix
// epoch: the same rule as the producer's
func nextAligned(after time.Time, period time.Duration) time.Time {
	ms := after.UnixMilli()
	p := period.Milliseconds()
	return time.UnixMilli(((ms / p) + 1) * p).UTC()
}

// Start checks every active monitor right away, then on each next multiple of its check
// interval. Events that arrive before it starts are ignored: it reads the monitors itself.
// ctx ends the scheduler like Stop does.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return errors.New("scheduler already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	runCtx := s.ctx
	s.mu.Unlock()

	s.syncMu.Lock()
	err := s.scheduleActiveMonitors(runCtx)
	s.syncMu.Unlock()
	if err != nil {
		s.Stop()
		return err
	}

	s.logger.Info("Scheduler started")
	return nil
}

// Stop stops the timers, cancels the running checks and waits for them to end. Their
// results are dropped.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.ctx == nil || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	for id, sc := range s.schedules {
		sc.timer.Stop()
		delete(s.schedules, id)
	}
	s.cancel()
	s.mu.Unlock()

	s.wg.Wait()
	s.logger.Info("Scheduler stopped")
}

// scheduleActiveMonitors reads the active monitors and checks each one that has no
// schedule yet right away
func (s *Scheduler) scheduleActiveMonitors(ctx context.Context) error {
	scheduled := 0
	for page := 0; ; page++ {
		monitors, err := s.monitors.FindActivePaginated(ctx, page, pageSize)
		if err != nil {
			return fmt.Errorf("failed to find active monitors (page %d): %w", page, err)
		}
		for _, mon := range monitors {
			if !schedulable(mon) {
				continue
			}
			if s.set(mon, true) {
				scheduled++
			}
		}
		if len(monitors) < pageSize {
			break
		}
	}
	s.logger.Infow("Scheduled the active monitors", "count", scheduled)
	return nil
}

// schedulable reports whether a monitor gets checks: it is active and has a check interval
func schedulable(mon *shared.Monitor) bool {
	return mon != nil && mon.Active && mon.Interval > 0
}

// set gives the monitor a timer for its next multiple of its check interval, replacing any
// it had, and runs a check now if asked. It reports whether the scheduler is running.
func (s *Scheduler) set(mon *shared.Monitor, checkNow bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() {
		return false
	}

	if old := s.schedules[mon.ID]; old != nil {
		old.timer.Stop()
	}
	interval := time.Duration(mon.Interval) * time.Second
	sc := &schedule{interval: interval}
	s.schedules[mon.ID] = sc
	s.arm(mon.ID, sc, time.Now())

	if checkNow {
		s.startCheck(mon.ID)
	}
	return true
}

// arm sets the schedule's timer for the first multiple of its interval after the time.
// The caller holds mu.
func (s *Scheduler) arm(monitorID string, sc *schedule, after time.Time) {
	due := nextAligned(after, sc.interval)
	sc.timer = time.AfterFunc(due.Sub(after), func() { s.fire(monitorID, sc) })
}

// fire is a monitor's timer going off: it sets the next timer, then starts a check
func (s *Scheduler) fire(monitorID string, sc *schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() || s.schedules[monitorID] != sc {
		return // stopped, or the monitor was rescheduled or removed meanwhile
	}
	s.arm(monitorID, sc, time.Now())
	s.startCheck(monitorID)
}

// running reports whether the scheduler has started and not stopped. The caller holds mu.
func (s *Scheduler) running() bool {
	return s.ctx != nil && !s.stopped && s.ctx.Err() == nil
}

// startCheck runs a check of the monitor in the background. The caller holds mu.
func (s *Scheduler) startCheck(monitorID string) {
	ctx := s.ctx
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runCheck(ctx, monitorID)
	}()
}

// runCheck loads the monitor's check, runs it and ingests its result
func (s *Scheduler) runCheck(ctx context.Context, monitorID string) {
	job, err := s.loader.Load(ctx, monitorID)
	switch {
	case errors.Is(err, check.ErrMonitorNotFound):
		s.logger.Warnw("Monitor not found, skipping its check", "monitor_id", monitorID)
		return
	case errors.Is(err, check.ErrMonitorInactive):
		s.logger.Infow("Monitor is inactive, skipping its check", "monitor_id", monitorID)
		return
	case err != nil:
		s.logger.Errorw("Failed to load the monitor's check", "monitor_id", monitorID, "error", err)
		return
	}
	mon := job.Monitor

	exec, ok := s.executors.GetExecutor(mon.Type)
	if !ok {
		s.logger.Errorw("Executor not found for monitor type", "monitor_id", monitorID, "monitor_type", mon.Type)
		return
	}

	tick := s.supervisor.HandleMonitorTick(ctx, mon, exec, job.Proxy, job.IsUnderMaintenance)

	// A check cut short by a stop says nothing about the target
	if ctx.Err() != nil {
		s.logger.Debugw("Check was cancelled, dropping its result", "monitor_id", monitorID)
		return
	}
	// An executor that has nothing to report returns no result
	if tick == nil {
		s.logger.Debugw("Executor returned nil - no heartbeat needed", "monitor_id", monitorID, "monitor_type", mon.Type)
		return
	}

	// A stop doesn't interrupt the ingest of a result that is already being stored
	ingestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ingestTimeout)
	defer cancel()
	if err := s.ingester.Ingest(ingestCtx, tick.IngestResult(job.CheckCertExpiry)); err != nil {
		s.logger.Errorw("Failed to ingest the result of a check", "monitor_id", monitorID, "error", err)
	}
}

// Subscribe reschedules monitors as they are created, updated and deleted. Pausing a
// monitor is an update.
func (s *Scheduler) Subscribe(eventBus events.EventBus) {
	eventBus.Subscribe(events.MonitorCreated, func(event events.Event) {
		if id, ok := monitorEventID(event); ok {
			s.onCreated(id)
		} else {
			s.logger.Errorw("Failed to read the monitor created event")
		}
	})
	eventBus.Subscribe(events.MonitorUpdated, func(event events.Event) {
		if id, ok := monitorEventID(event); ok {
			s.onUpdated(id)
		} else {
			s.logger.Errorw("Failed to read the monitor updated event")
		}
	})
	eventBus.Subscribe(events.MonitorDeleted, func(event events.Event) {
		if id, ok := infra.UnmarshalEventPayloadValue[string](event); ok {
			s.onDeleted(id)
		} else {
			s.logger.Errorw("Failed to read the monitor deleted event")
		}
	})
}

// monitorEventID is the ID of the monitor in a created or updated event. The event is only
// a notice: the scheduler reads the monitor from the database.
func monitorEventID(event events.Event) (string, bool) {
	mon, ok := infra.UnmarshalEventPayloadValue[shared.Monitor](event)
	return mon.ID, ok && mon.ID != ""
}

// onCreated checks a new monitor right away
func (s *Scheduler) onCreated(monitorID string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	mon, ok := s.find(monitorID)
	if !ok || !schedulable(mon) {
		return
	}
	s.mu.Lock()
	_, scheduled := s.schedules[monitorID]
	s.mu.Unlock()
	s.set(mon, !scheduled)
}

// onUpdated reschedules a monitor, or stops scheduling it if it was paused
func (s *Scheduler) onUpdated(monitorID string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	mon, ok := s.find(monitorID)
	if !ok {
		return
	}
	if !schedulable(mon) {
		s.unschedule(monitorID)
		return
	}
	// Check right away so the change shows up without waiting a full interval. Not a push
	// monitor: it waits for pushes, and right after a data reset an immediate check would
	// find none and report DOWN. The next timer is at the next multiple of the (new) interval.
	s.set(mon, mon.Type != "push")
}

// onDeleted stops scheduling a deleted monitor
func (s *Scheduler) onDeleted(monitorID string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.unschedule(monitorID)
}

// find reads the monitor from the database. It reports false when the scheduler isn't running
// or the read failed; the monitor is nil when it doesn't exist.
func (s *Scheduler) find(monitorID string) (*shared.Monitor, bool) {
	s.mu.Lock()
	ctx := s.ctx
	running := s.running()
	s.mu.Unlock()
	if !running {
		return nil, false
	}

	mon, err := s.monitors.FindByID(ctx, monitorID)
	if err != nil {
		s.logger.Errorw("Failed to find monitor", "monitor_id", monitorID, "error", err)
		return nil, false
	}
	return mon, true
}

// unschedule stops the monitor's timer
func (s *Scheduler) unschedule(monitorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc := s.schedules[monitorID]; sc != nil {
		sc.timer.Stop()
		delete(s.schedules, monitorID)
	}
}
