// Package scheduler is single mode's check scheduler: it keeps a timer for each active
// monitor and runs the checks that fall due in a bounded pool, in the same process, then
// ingests their results directly. Cluster mode has no scheduler: its probes run the checks.
// See ADR 0002, section 4.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting"
)

// Executors finds the executor for a monitor type. executor.ExecutorRegistry is one.
type Executors interface {
	GetExecutor(monitorType string) (executor.Executor, bool)
}

// Config is what a Scheduler reads and calls
type Config struct {
	// Source is where the monitors, their checks and the signals of their changes come from
	Source     Source
	Executors  Executors
	Supervisor *supervisor.HealthCheckSupervisor
	// Ingester takes the results of the checks. The scheduler ingests a monitor's results one
	// at a time, in order. Pushes to a push monitor must go through the same
	// ingest.NewInOrderService to be ordered with its checks.
	Ingester ingest.Service
	// Concurrency is how many checks run at the same time; DefaultConcurrency when it's zero
	Concurrency int
	// ReconcileInterval is how often the timers are compared with the active monitors of the
	// source; DefaultReconcileInterval when it's zero
	ReconcileInterval time.Duration
	Logger            *zap.SugaredLogger
}

const (
	// DefaultConcurrency is the size of the check pool when none is configured
	DefaultConcurrency = 128
	// DefaultReconcileInterval is how often the scheduler reconciles its timers with its source
	DefaultReconcileInterval = 30 * time.Second

	// pageSize is how many active monitors are read at a time
	pageSize = 100
	// ingestTimeout bounds one result's ingest, which a cancelled check doesn't interrupt
	ingestTimeout = 2 * time.Minute
	// ingestGrace is what CheckTimeout allows for a result's ingest. The watchdog of the cluster
	// api uses CheckTimeout too, so it waits as long in both modes.
	ingestGrace = 8 * time.Second
)

// CheckTimeout is how long a check of a monitor with the given timeout (in seconds) takes to
// finish and be ingested once it has a slot in the pool. The watchdog gives a stale
// monitor's new check this long before it notifies.
func CheckTimeout(timeoutSeconds int) time.Duration {
	return time.Duration(timeoutSeconds)*time.Second + supervisor.ExecutorGrace + ingestGrace
}

// Scheduler checks the active monitors when their check intervals pass. It implements
// stale.Rechecker, which package app checks where it hands the scheduler to the watchdog.
type Scheduler struct {
	source            Source
	executors         Executors
	supervisor        *supervisor.HealthCheckSupervisor
	ingester          ingest.Service
	reconcileInterval time.Duration
	logger            *zap.SugaredLogger

	// slots is the check pool: a check holds one of its slots while it runs
	slots chan struct{}

	// syncMu serializes reading a monitor from the source and changing its schedule, so
	// that signals handled at the same time leave the schedule as the last read found the monitor
	syncMu sync.Mutex

	mu        sync.Mutex
	schedules map[string]*schedule
	checks    map[string]*checkRun // the monitors with a check running
	ctx       context.Context      // the scheduler's lifetime; nil until it starts
	cancel    context.CancelFunc
	stopped   bool
	stopping  chan struct{} // closed once the scheduler is stopped
	wg        sync.WaitGroup
}

// schedule is the timer of one monitor
type schedule struct {
	interval time.Duration
	timer    *time.Timer
}

// checkRun is a monitor's running check
type checkRun struct {
	cancel    context.CancelFunc
	cancelled bool
	// rerun is set when a check was asked for while this one, already cancelled, was
	// winding down. The monitor is checked again as soon as it has ended.
	rerun bool
}

// New creates a scheduler
func New(cfg Config) *Scheduler {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	reconcileInterval := cfg.ReconcileInterval
	if reconcileInterval <= 0 {
		reconcileInterval = DefaultReconcileInterval
	}
	return &Scheduler{
		source:            cfg.Source,
		executors:         cfg.Executors,
		supervisor:        cfg.Supervisor,
		ingester:          ingest.NewInOrderService(cfg.Ingester),
		reconcileInterval: reconcileInterval,
		logger:            cfg.Logger.With("component", "scheduler"),
		slots:             make(chan struct{}, concurrency),
		stopping:          make(chan struct{}),
		schedules:         make(map[string]*schedule),
		checks:            make(map[string]*checkRun),
	}
}

// nextAligned is the next multiple of the period after the time, counted from the Unix epoch
func nextAligned(after time.Time, period time.Duration) time.Time {
	ms := after.UnixMilli()
	p := period.Milliseconds()
	return time.UnixMilli(((ms / p) + 1) * p).UTC()
}

// Start checks every active monitor of its source right away, then on each next multiple of
// its check interval. It listens to the source for monitors that are created, changed or gone
// from then on, and reads the monitors itself. It reconciles its timers with the source every
// reconcile interval. ctx ends the scheduler like Stop does.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return errors.New("scheduler already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	runCtx := s.ctx
	s.mu.Unlock()

	s.source.Watch(s.onCreated, s.onUpdated, s.onDeleted)
	if err := s.reconcile(runCtx); err != nil {
		s.Stop()
		return err
	}

	s.mu.Lock()
	if s.running() {
		s.wg.Add(1)
		reporting.Go("scheduler", func() { s.reconcileEvery(runCtx) })
	}
	s.mu.Unlock()

	s.logger.Info("Scheduler started")
	return nil
}

// Stop stops the timers, cancels the running checks and waits for them to end. Their
// results are dropped.
func (s *Scheduler) Stop() {
	if !s.halt() {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.logger.Info("Scheduler stopped")
}

// Drain stops the timers and lets the running checks finish and their results be ingested,
// for as long as ctx lasts. When ctx is done first, it cancels the checks that are left and
// drops their results, as Stop does. A check waiting for a slot in the pool never starts.
// Drain returns once every check has ended, and the scheduler is stopped: Stop does nothing
// after it.
func (s *Scheduler) Drain(ctx context.Context) {
	if !s.halt() {
		return
	}
	ended := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-ctx.Done():
		s.logger.Warn("Drain ran out of time, cancelling the checks that are still running")
	}
	s.cancel()
	<-ended
	s.logger.Info("Scheduler stopped")
}

// halt stops the timers and keeps any new check from starting. It reports false when the
// scheduler has not started, or is stopped already.
func (s *Scheduler) halt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.stopped {
		return false
	}
	s.stopped = true
	close(s.stopping)
	for id, sc := range s.schedules {
		sc.timer.Stop()
		delete(s.schedules, id)
	}
	return true
}

// reconcileEvery reconciles the timers with the source every reconcile interval until ctx is
// done or the scheduler stops
func (s *Scheduler) reconcileEvery(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopping:
			return
		case <-ticker.C:
			if err := s.reconcile(ctx); err != nil && ctx.Err() == nil {
				s.logger.Errorw("Failed to reconcile the schedule", "error", err)
			}
		}
	}
}

// reconciled counts the timers a reconcile changed
type reconciled struct {
	added, rescheduled, removed int
}

// reconcile compares the timers with the active monitors of the source, so that a missed
// signal can't leave a monitor unscheduled or a deleted one checked. A monitor without a timer is checked right away. One whose check
// interval changed is checked next on a multiple of the new interval. One that is no longer
// active, or has moved to a remote probe, loses its timer, and its running check is cancelled.
func (s *Scheduler) reconcile(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	var counts reconciled
	listed := make(map[string]bool)
	for page := 0; ; page++ {
		monitors, err := s.source.ListActive(ctx, page, pageSize)
		if err != nil {
			return fmt.Errorf("failed to find active monitors (page %d): %w", page, err)
		}
		for _, mon := range monitors {
			if !schedulable(mon) {
				continue
			}
			listed[mon.ID] = true
			s.reconcileMonitor(mon, &counts)
		}
		if len(monitors) < pageSize {
			break
		}
	}

	// A monitor paused or deleted during the scan shifts the pages, so check each scheduled
	// monitor the scan missed before dropping it
	s.mu.Lock()
	scheduled := slices.Collect(maps.Keys(s.schedules))
	s.mu.Unlock()
	for _, id := range scheduled {
		if listed[id] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		mon, err := s.source.Find(ctx, id)
		if err != nil {
			s.logger.Errorw("Failed to find monitor", "monitor_id", id, "error", err)
			continue
		}
		if schedulable(mon) {
			s.reconcileMonitor(mon, &counts)
			continue
		}
		s.unschedule(id)
		counts.removed++
	}

	if counts != (reconciled{}) {
		s.logger.Infow("Reconciled the schedule with the source",
			"added", counts.added,
			"rescheduled", counts.rescheduled,
			"removed", counts.removed)
	}
	return nil
}

// reconcileMonitor gives an active monitor a timer and checks it right away if it has none,
// or a new timer for the next multiple of its check interval if the interval changed. The
// caller holds syncMu.
func (s *Scheduler) reconcileMonitor(mon *shared.Monitor, counts *reconciled) {
	s.mu.Lock()
	sc := s.schedules[mon.ID]
	s.mu.Unlock()

	switch {
	case sc == nil:
		if s.set(mon, true) {
			counts.added++
		}
	case sc.interval != checkInterval(mon):
		if s.set(mon, false) {
			counts.rescheduled++
		}
	}
}

// checkInterval is the monitor's check interval as a duration
func checkInterval(mon *shared.Monitor) time.Duration {
	return time.Duration(mon.Interval) * time.Second
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
	sc := &schedule{interval: checkInterval(mon)}
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

// startCheck runs a check of the monitor in the pool, unless one is already running: a
// monitor runs at most one check at a time, and the check that fell due is skipped. A check
// waiting for a slot in the pool counts as running. A running check that was cancelled
// doesn't count: the monitor is checked again once it has ended. The caller holds mu.
func (s *Scheduler) startCheck(monitorID string) {
	if run := s.checks[monitorID]; run != nil {
		if run.cancelled {
			run.rerun = true
		} else {
			s.logger.Debugw("Previous check still running, skipping", "monitor_id", monitorID)
		}
		return
	}

	ctx, cancel := context.WithCancel(s.ctx)
	run := &checkRun{cancel: cancel}
	s.checks[monitorID] = run
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.endCheck(monitorID, run)
		if !s.takeSlot(ctx) {
			return // cancelled or stopped while it waited
		}
		defer s.freeSlot()
		defer s.recoverCheck(monitorID)
		s.runCheck(ctx, monitorID)
	}()
}

// recoverCheck logs a panic in the monitor's check and ends that check alone: an unrecovered
// panic would stop every monitor.
// The check frees its slot and ends as any other, so the monitor's next check runs.
func (s *Scheduler) recoverCheck(monitorID string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		s.logger.Errorw("Check panicked",
			"monitor_id", monitorID,
			"panic", r,
			"stack", string(stack))
		reporting.CapturePanic("scheduler", r, stack)
	}
}

// takeSlot waits for a free slot in the pool. It gives up and reports false when the check
// is cancelled or the scheduler stops first.
func (s *Scheduler) takeSlot(ctx context.Context) bool {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return false
	case <-s.stopping:
		return false
	}
	// The slot can win over a cancel that was ready too, or come free while a stop is
	// still cancelling the checks one by one and hasn't reached this one
	s.mu.Lock()
	cancelled := ctx.Err() != nil || !s.running()
	s.mu.Unlock()
	if cancelled {
		s.freeSlot()
		return false
	}
	return true
}

func (s *Scheduler) freeSlot() {
	<-s.slots
}

// endCheck forgets the monitor's finished check, and starts the check that was waiting for it
func (s *Scheduler) endCheck(monitorID string, run *checkRun) {
	run.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checks[monitorID] == run {
		delete(s.checks, monitorID)
	}
	if run.rerun && s.running() {
		s.startCheck(monitorID)
	}
}

// cancelCheck cancels the monitor's running check, whose result is dropped. The caller holds mu.
func (s *Scheduler) cancelCheck(monitorID string) {
	if run := s.checks[monitorID]; run != nil {
		run.cancelled = true
		run.rerun = false
		run.cancel()
	}
}

// runCheck loads the monitor's check, runs it and ingests its result
func (s *Scheduler) runCheck(ctx context.Context, monitorID string) {
	loaded, err := s.source.Load(ctx, monitorID)
	switch {
	case errors.Is(err, job.ErrMonitorNotFound):
		s.logger.Warnw("Monitor not found, skipping its check", "monitor_id", monitorID)
		return
	case errors.Is(err, job.ErrMonitorPaused):
		s.logger.Infow("Monitor is paused, skipping its check", "monitor_id", monitorID)
		return
	case err != nil && ctx.Err() != nil:
		// An update, pause, delete or stop cancelled the check while it loaded
		s.logger.Debugw("Check was cancelled while it loaded", "monitor_id", monitorID)
		return
	case err != nil:
		s.logger.Errorw("Failed to load the monitor's check", "monitor_id", monitorID, "error", err)
		return
	}
	mon := loaded.Monitor
	// A push monitor's check judges its latest push
	mon.LastHeartbeat = loaded.LastHeartbeat

	exec, ok := s.executors.GetExecutor(mon.Type)
	if !ok {
		s.logger.Errorw("Executor not found for monitor type", "monitor_id", monitorID, "monitor_type", mon.Type)
		return
	}

	tick := s.supervisor.HandleMonitorTick(ctx, mon, exec, loaded.Proxy, loaded.IsUnderMaintenance)

	// An executor that has nothing to report returns no result
	if tick == nil {
		s.logger.Debugw("Executor returned nil - no heartbeat needed", "monitor_id", monitorID, "monitor_type", mon.Type)
		return
	}
	result := tick.IngestResult(loaded.CheckCertExpiry)

	// An update, pause, delete or stop drops the result until here. The ingest isn't
	// interrupted once it starts: it reads the previous heartbeat to write the next one. A
	// cancel during it is too late, as it is in cluster mode once the result is enqueued.
	if ctx.Err() != nil {
		s.logger.Debugw("Check was cancelled, dropping its result", "monitor_id", monitorID)
		return
	}
	ingestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ingestTimeout)
	defer cancel()
	if err := s.ingester.Ingest(ingestCtx, result); err != nil {
		s.logger.Errorw("Failed to ingest the result of a check", "monitor_id", monitorID, "error", err)
	}
}

// The source's signals reschedule monitors as they are created, changed (pausing is a change)
// and gone. A signal is only a notice: the scheduler reads the monitor from the source, so it
// acts on the monitor as it is now, whatever the signal says.

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

// onUpdated reschedules a monitor, or stops scheduling it if it was paused or the source no
// longer has it for this process (it moved to another probe)
func (s *Scheduler) onUpdated(monitorID string) {
	// A running check carries the config from before the update
	s.cancelBeforeSync(monitorID)

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
	s.cancelBeforeSync(monitorID)

	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.unschedule(monitorID)
}

// cancelBeforeSync cancels the monitor's running check as soon as an update, pause or delete
// arrives, before the handler waits for syncMu and reads the monitor. A check that ended
// while the handler waited would otherwise have its result stored.
func (s *Scheduler) cancelBeforeSync(monitorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCheck(monitorID)
}

// RunNow checks the monitor right away, unless a check of it is already running, and puts it
// back on its schedule if it had lost its timer. It implements stale.Rechecker: the watchdog
// calls it for a monitor that hasn't been checked in too long.
func (s *Scheduler) RunNow(ctx context.Context, monitorID string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	s.mu.Lock()
	running := s.running()
	s.mu.Unlock()
	if !running {
		return errors.New("scheduler is not running")
	}

	mon, err := s.source.Find(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}
	if mon == nil {
		return fmt.Errorf("monitor %s not found or runs on another probe", monitorID)
	}
	if !schedulable(mon) {
		return fmt.Errorf("monitor %s is paused or has no check interval", monitorID)
	}
	if !s.set(mon, true) {
		return errors.New("scheduler is not running")
	}
	return nil
}

// find reads the monitor from the source. It reports false when the scheduler isn't running
// or the read failed; the monitor is nil when it is gone or runs on another probe.
func (s *Scheduler) find(monitorID string) (*shared.Monitor, bool) {
	s.mu.Lock()
	ctx := s.ctx
	running := s.running()
	s.mu.Unlock()
	if !running {
		return nil, false
	}

	mon, err := s.source.Find(ctx, monitorID)
	if err != nil {
		s.logger.Errorw("Failed to find monitor", "monitor_id", monitorID, "error", err)
		return nil, false
	}
	return mon, true
}

// unschedule stops the monitor's timer and cancels its running check
func (s *Scheduler) unschedule(monitorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCheck(monitorID)
	if sc := s.schedules[monitorID]; sc != nil {
		sc.timer.Stop()
		delete(s.schedules, monitorID)
	}
}
