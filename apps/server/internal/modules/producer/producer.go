package producer

import (
	"context"
	"fmt"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/worker"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Redis keys for scheduler
const (
	SchedDueKey   = "peekaping:sched:due"   // ZSET: score=next_due_ms, member=monitor_id
	SchedLeaseKey = "peekaping:sched:lease" // ZSET: score=lease_expire_ms, member=monitor_id

	BatchClaim   = 1000                  // max items to claim per tick
	LeaseTTL     = 10 * time.Second      // how long an item can sit in "lease" while enqueuing
	ReclaimEvery = 2 * time.Second       // how often to sweep expired leases
	ClaimTick    = 25 * time.Millisecond // how often to check for due monitors
)

// Lua scripts for atomic operations
const (
	// CLAIM: move due items (score <= now_ms) from due → lease with lease expiry.
	claimLua = `
local due   = KEYS[1]
local lease = KEYS[2]
local now   = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local lms   = tonumber(ARGV[3])

local ids = redis.call('ZRANGEBYSCORE', due, '-inf', now, 'LIMIT', 0, limit)
if #ids == 0 then return ids end
for i=1,#ids do
  redis.call('ZREM', due, ids[i])
  redis.call('ZADD', lease, now + lms, ids[i])
end
return ids
`

	// RESCHEDULE: move a claimed item lease → due at next_ts_ms
	reschedLua = `
local lease = KEYS[1]
local due   = KEYS[2]
local id    = ARGV[1]
local next  = tonumber(ARGV[2])
redis.call('ZREM', lease, id)
redis.call('ZADD', due, next, id)
return 1
`

	// RECLAIM: move expired leases (score <= now_ms) back to due at now_ms
	reclaimLua = `
local lease = KEYS[1]
local due   = KEYS[2]
local now   = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])

local ids = redis.call('ZRANGEBYSCORE', lease, '-inf', now, 'LIMIT', 0, limit)
for i=1,#ids do
  redis.call('ZREM', lease, ids[i])
  redis.call('ZADD', due, now, ids[i])
end
return ids
`
)

var (
	claimScript   *redis.Script
	reclaimScript *redis.Script
)

func init() {
	claimScript = redis.NewScript(claimLua)
	reclaimScript = redis.NewScript(reclaimLua)
}

// Producer is responsible for scheduling monitor health checks
type Producer struct {
	rdb                     *redis.Client
	queueService            queue.Service
	monitorService          monitor.Service
	proxyService            proxy.Service
	maintenanceService      maintenance.Service
	monitorNotificationSvc  monitor_notification.Service
	settingService          shared.SettingService
	logger                  *zap.SugaredLogger
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	mu                      sync.RWMutex
	monitorIntervals        map[string]int // monitor_id -> interval in seconds
	scheduleRefreshInterval time.Duration
	leaderElection          *LeaderElection
}

// NewProducer creates a new producer instance
func NewProducer(
	rdb *redis.Client,
	queueService queue.Service,
	monitorService monitor.Service,
	proxyService proxy.Service,
	maintenanceService maintenance.Service,
	monitorNotificationSvc monitor_notification.Service,
	settingService shared.SettingService,
	leaderElection *LeaderElection,
	logger *zap.SugaredLogger,
) *Producer {
	ctx, cancel := context.WithCancel(context.Background())

	return &Producer{
		rdb:                     rdb,
		queueService:            queueService,
		monitorService:          monitorService,
		proxyService:            proxyService,
		maintenanceService:      maintenanceService,
		monitorNotificationSvc:  monitorNotificationSvc,
		settingService:          settingService,
		logger:                  logger.With("component", "producer"),
		ctx:                     ctx,
		cancel:                  cancel,
		monitorIntervals:        make(map[string]int),
		scheduleRefreshInterval: 30 * time.Second, // Refresh schedule every 30 seconds
		leaderElection:          leaderElection,
	}
}

// Start starts the producer with leader election
func (p *Producer) Start() error {
	p.logger.Info("Starting producer with leader election")

	// Start leader election
	p.leaderElection.Start(p.ctx)

	// Start a goroutine to monitor leadership changes
	p.wg.Add(1)
	go p.runLeadershipMonitor()

	p.logger.Info("Producer started successfully")
	return nil
}

// runLeadershipMonitor monitors leadership status and starts/stops scheduling accordingly
func (p *Producer) runLeadershipMonitor() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var isScheduling bool

	for {
		select {
		case <-p.ctx.Done():
			if isScheduling {
				p.logger.Info("Context cancelled, stopping scheduling")
			}
			return
		case <-ticker.C:
			isLeader := p.leaderElection.IsLeader()

			if isLeader && !isScheduling {
				p.logger.Info("Became leader, starting scheduling")
				if err := p.startScheduling(); err != nil {
					p.logger.Errorw("Failed to start scheduling", "error", err)
				} else {
					isScheduling = true
				}
			} else if !isLeader && isScheduling {
				p.logger.Info("Lost leadership, stopping scheduling")
				p.stopScheduling()
				isScheduling = false
			}
		}
	}
}

// startScheduling initializes and starts the scheduling components
func (p *Producer) startScheduling() error {
	// Initialize schedule with active monitors
	if err := p.initializeSchedule(); err != nil {
		return fmt.Errorf("failed to initialize schedule: %w", err)
	}

	// Start background reclaimer (handles crashed or slow producers)
	p.wg.Add(1)
	go p.runReclaimer()

	// Start schedule refresher
	p.wg.Add(1)
	go p.runScheduleRefresher()

	// Start main producer loop
	p.wg.Add(1)
	go p.runProducer()

	return nil
}

// stopScheduling is called when this node loses leadership
// The goroutines will naturally exit when they check the context
func (p *Producer) stopScheduling() {
	// The goroutines will stop when they check ctx.Done()
	// No need to do anything special here as we use the same context
	p.logger.Info("Scheduling stopped due to leadership loss")
}

// Stop stops the producer gracefully
func (p *Producer) Stop() {
	p.logger.Info("Stopping producer")
	p.cancel()
	p.wg.Wait()
	p.logger.Info("Producer stopped")
}

// initializeSchedule loads all active monitors and schedules them
func (p *Producer) initializeSchedule() error {
	p.logger.Info("Initializing schedule with active monitors")

	monitors, err := p.monitorService.FindActive(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to find active monitors: %w", err)
	}

	if len(monitors) == 0 {
		p.logger.Info("No active monitors found to schedule")
		return nil
	}

	now := time.Now().UTC()
	pipe := p.rdb.Pipeline()

	for _, mon := range monitors {
		if mon.Interval <= 0 {
			p.logger.Warnw("Skipping monitor with invalid interval", "monitor_id", mon.ID, "interval", mon.Interval)
			continue
		}

		// Store monitor interval for future reference
		p.mu.Lock()
		p.monitorIntervals[mon.ID] = mon.Interval
		p.mu.Unlock()

		// Schedule monitor at next aligned time
		next := nextAligned(now, time.Duration(mon.Interval)*time.Second)
		pipe.ZAdd(p.ctx, SchedDueKey, redis.Z{
			Score:  float64(next.UnixMilli()),
			Member: mon.ID,
		})
	}

	if _, err := pipe.Exec(p.ctx); err != nil {
		return fmt.Errorf("failed to schedule monitors: %w", err)
	}

	p.logger.Infow("Initialized schedule", "monitor_count", len(monitors))
	return nil
}

// runReclaimer periodically reclaims expired leases
func (p *Producer) runReclaimer() {
	defer p.wg.Done()
	ticker := time.NewTicker(ReclaimEvery)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			nowMs := p.redisNowMs()
			result, err := reclaimScript.Run(p.ctx, p.rdb,
				[]string{SchedLeaseKey, SchedDueKey},
				nowMs, 5000).Result()
			if err != nil {
				p.logger.Errorw("Reclaim error", "error", err)
			} else if ids, ok := result.([]interface{}); ok && len(ids) > 0 {
				p.logger.Infow("Reclaimed expired leases", "count", len(ids))
			}
		}
	}
}

// runScheduleRefresher periodically refreshes the schedule with new/updated monitors
func (p *Producer) runScheduleRefresher() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.scheduleRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshSchedule(); err != nil {
				p.logger.Errorw("Failed to refresh schedule", "error", err)
			}
		}
	}
}

// refreshSchedule updates the schedule with any new or updated monitors
func (p *Producer) refreshSchedule() error {
	monitors, err := p.monitorService.FindActive(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to find active monitors: %w", err)
	}

	currentMonitorIDs := make(map[string]bool)
	now := time.Now().UTC()
	pipe := p.rdb.Pipeline()

	for _, mon := range monitors {
		if mon.Interval <= 0 {
			continue
		}

		currentMonitorIDs[mon.ID] = true

		p.mu.RLock()
		oldInterval, exists := p.monitorIntervals[mon.ID]
		p.mu.RUnlock()

		// If monitor is new or interval changed, reschedule it
		if !exists || oldInterval != mon.Interval {
			p.mu.Lock()
			p.monitorIntervals[mon.ID] = mon.Interval
			p.mu.Unlock()

			// Remove from both due and lease sets
			pipe.ZRem(p.ctx, SchedDueKey, mon.ID)
			pipe.ZRem(p.ctx, SchedLeaseKey, mon.ID)

			// Schedule at next aligned time
			next := nextAligned(now, time.Duration(mon.Interval)*time.Second)
			pipe.ZAdd(p.ctx, SchedDueKey, redis.Z{
				Score:  float64(next.UnixMilli()),
				Member: mon.ID,
			})

			if !exists {
				p.logger.Infow("Scheduling new monitor", "monitor_id", mon.ID, "interval", mon.Interval)
			} else {
				p.logger.Infow("Rescheduling monitor with updated interval",
					"monitor_id", mon.ID,
					"old_interval", oldInterval,
					"new_interval", mon.Interval)
			}
		}
	}

	// Remove monitors that are no longer active
	p.mu.Lock()
	for monitorID := range p.monitorIntervals {
		if !currentMonitorIDs[monitorID] {
			delete(p.monitorIntervals, monitorID)
			pipe.ZRem(p.ctx, SchedDueKey, monitorID)
			pipe.ZRem(p.ctx, SchedLeaseKey, monitorID)
			p.logger.Infow("Removed inactive monitor from schedule", "monitor_id", monitorID)
		}
	}
	p.mu.Unlock()

	if _, err := pipe.Exec(p.ctx); err != nil {
		return fmt.Errorf("failed to refresh schedule: %w", err)
	}

	return nil
}

// runProducer is the main producer loop
func (p *Producer) runProducer() error {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		nowMs := p.redisNowMs()

		// Atomically claim a batch of due items
		idsAny, err := claimScript.Run(p.ctx, p.rdb,
			[]string{SchedDueKey, SchedLeaseKey},
			nowMs, BatchClaim, int64(LeaseTTL/time.Millisecond),
		).Result()
		if err != nil {
			p.logger.Errorw("Claim error", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		ids := toStringSlice(idsAny)
		if len(ids) == 0 {
			// Sleep until next check
			time.Sleep(ClaimTick)
			continue
		}

		p.logger.Debugw("Claimed monitors for scheduling", "count", len(ids))

		// Process each claimed monitor
		pipe := p.rdb.Pipeline()
		for _, monitorID := range ids {
			if err := p.processMonitor(monitorID, nowMs); err != nil {
				p.logger.Errorw("Failed to process monitor",
					"monitor_id", monitorID,
					"error", err)
				// Don't reschedule on error - let lease expire and be reclaimed
				continue
			}

			// Get monitor interval for rescheduling
			p.mu.RLock()
			interval, exists := p.monitorIntervals[monitorID]
			p.mu.RUnlock()

			if !exists {
				p.logger.Warnw("Monitor interval not found, will be refreshed soon", "monitor_id", monitorID)
				continue
			}

			// Calculate next execution time
			next := nextAligned(time.UnixMilli(nowMs).UTC(), time.Duration(interval)*time.Second)
			pipe.Eval(p.ctx, reschedLua, []string{SchedLeaseKey, SchedDueKey}, monitorID, next.UnixMilli())
		}

		if _, err := pipe.Exec(p.ctx); err != nil {
			p.logger.Errorw("Resched pipeline error", "error", err)
		}
	}
}

// processMonitor loads monitor config and enqueues a health check task
func (p *Producer) processMonitor(monitorID string, nowMs int64) error {
	start := time.Now()
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(p.ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !mon.Active {
		p.logger.Infow("Skipping inactive monitor", "monitor_id", monitorID)
		return nil
	}

	// Check if monitor is under maintenance
	isUnderMaintenance := false
	maintenances, err := p.maintenanceService.GetMaintenancesByMonitorID(p.ctx, monitorID)
	if err != nil {
		p.logger.Errorw("Failed to get maintenances", "monitor_id", monitorID, "error", err)
	} else {
		for _, maint := range maintenances {
			underMaintenance, err := p.maintenanceService.IsUnderMaintenance(p.ctx, maint)
			if err != nil {
				p.logger.Warnw("Failed to check maintenance status",
					"monitor_id", monitorID,
					"maintenance_id", maint.ID,
					"error", err)
				continue
			}
			if underMaintenance {
				isUnderMaintenance = true
				break
			}
		}
	}

	// Fetch proxy if configured
	var proxyData *worker.ProxyData
	if mon.ProxyId != "" {
		proxyModel, err := p.proxyService.FindByID(p.ctx, mon.ProxyId)
		if err != nil {
			p.logger.Warnw("Failed to fetch proxy, continuing without it",
				"monitor_id", monitorID,
				"proxy_id", mon.ProxyId,
				"error", err)
		} else {
			proxyData = &worker.ProxyData{
				ID:       proxyModel.ID,
				Protocol: proxyModel.Protocol,
				Host:     proxyModel.Host,
				Port:     proxyModel.Port,
				Auth:     proxyModel.Auth,
				Username: proxyModel.Username,
				Password: proxyModel.Password,
			}
		}
	}

	// Check if we should check certificate expiry
	// This is typically enabled by default for http and tcp monitors
	checkCertExpiry := false
	if mon.Type == "http" || mon.Type == "tcp" {
		// Certificate checking is typically always enabled for these monitor types
		// The certificate service handles notification settings
		checkCertExpiry = true
	}

	// Create health check task payload
	payload := worker.HealthCheckTaskPayload{
		MonitorID:          mon.ID,
		MonitorName:        mon.Name,
		MonitorType:        mon.Type,
		Interval:           mon.Interval,
		Timeout:            mon.Timeout,
		MaxRetries:         mon.MaxRetries,
		RetryInterval:      mon.RetryInterval,
		ResendInterval:     mon.ResendInterval,
		Config:             mon.Config,
		Proxy:              proxyData,
		ScheduledAt:        time.UnixMilli(nowMs).UTC(),
		IsUnderMaintenance: isUnderMaintenance,
		CheckCertExpiry:    checkCertExpiry,
	}

	// Enqueue task to worker queue
	opts := &queue.EnqueueOptions{
		Queue:     "healthcheck",
		MaxRetry:  0,
		Timeout:   time.Duration(mon.Timeout) * time.Second,
		Retention: 0 * time.Minute,
	}

	_, err = p.queueService.Enqueue(p.ctx, worker.TaskTypeHealthCheck, payload, opts)
	if err != nil {
		return fmt.Errorf("failed to enqueue health check: %w", err)
	}

	// p.logger.Debugw("Enqueued health check",
	// 	"monitor_id", mon.ID,
	// 	"monitor_name", mon.Name,
	// 	"monitor_type", mon.Type)
	p.logger.Infow("Enqueued health check",
		"monitor_id", mon.ID,
		"duration", time.Since(start))

	return nil
}

// Helper functions

// nextAligned calculates the next aligned time based on interval
func nextAligned(after time.Time, period time.Duration) time.Time {
	ms := after.UnixMilli()
	p := period.Milliseconds()
	return time.UnixMilli(((ms / p) + 1) * p).UTC()
}

// redisNowMs returns the current time in milliseconds from Redis
func (p *Producer) redisNowMs() int64 {
	// Prefer Redis TIME to keep a single clock for all producers
	t, err := p.rdb.Time(p.ctx).Result()
	if err != nil {
		p.logger.Warnw("Failed to get Redis time, using local time", "error", err)
		return time.Now().UTC().UnixMilli()
	}
	return t.UnixMilli()
}

// toStringSlice converts Redis result to string slice
func toStringSlice(v any) []string {
	as, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(as))
	for _, x := range as {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case []byte:
			out = append(out, string(t))
		default:
			out = append(out, fmt.Sprint(t))
		}
	}
	return out
}

// ScheduleMonitor adds or updates a monitor in the schedule
func (p *Producer) ScheduleMonitor(ctx context.Context, monitorID string, intervalSeconds int) error {
	if intervalSeconds <= 0 {
		return fmt.Errorf("invalid interval: %d", intervalSeconds)
	}

	p.mu.Lock()
	p.monitorIntervals[monitorID] = intervalSeconds
	p.mu.Unlock()

	now := time.Now().UTC()
	next := nextAligned(now, time.Duration(intervalSeconds)*time.Second)

	// Remove from lease in case it's there, then add to due
	pipe := p.rdb.Pipeline()
	pipe.ZRem(ctx, SchedLeaseKey, monitorID)
	pipe.ZAdd(ctx, SchedDueKey, redis.Z{
		Score:  float64(next.UnixMilli()),
		Member: monitorID,
	})

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to schedule monitor: %w", err)
	}

	p.logger.Infow("Scheduled monitor", "monitor_id", monitorID, "interval", intervalSeconds, "next_run", next)
	return nil
}

// UnscheduleMonitor removes a monitor from the schedule
func (p *Producer) UnscheduleMonitor(ctx context.Context, monitorID string) error {
	p.mu.Lock()
	delete(p.monitorIntervals, monitorID)
	p.mu.Unlock()

	pipe := p.rdb.Pipeline()
	pipe.ZRem(ctx, SchedDueKey, monitorID)
	pipe.ZRem(ctx, SchedLeaseKey, monitorID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to unschedule monitor: %w", err)
	}

	p.logger.Infow("Unscheduled monitor", "monitor_id", monitorID)
	return nil
}

// AddMonitor adds a new monitor to the schedule
func (p *Producer) AddMonitor(ctx context.Context, monitorID string) error {
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !mon.Active || mon.Interval <= 0 {
		p.logger.Infow("Skipping inactive or invalid monitor", "monitor_id", monitorID, "active", mon.Active, "interval", mon.Interval)
		return nil
	}

	// Schedule the monitor
	return p.ScheduleMonitor(ctx, monitorID, mon.Interval)
}

// UpdateMonitor updates an existing monitor in the schedule
func (p *Producer) UpdateMonitor(ctx context.Context, monitorID string) error {
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !mon.Active {
		// If monitor is no longer active, unschedule it
		p.logger.Infow("Monitor became inactive, unscheduling", "monitor_id", monitorID)
		return p.UnscheduleMonitor(ctx, monitorID)
	}

	if mon.Interval <= 0 {
		p.logger.Warnw("Monitor has invalid interval, unscheduling", "monitor_id", monitorID, "interval", mon.Interval)
		return p.UnscheduleMonitor(ctx, monitorID)
	}

	// Reschedule the monitor with updated interval
	return p.ScheduleMonitor(ctx, monitorID, mon.Interval)
}

// RemoveMonitor removes a monitor from the schedule
func (p *Producer) RemoveMonitor(ctx context.Context, monitorID string) error {
	return p.UnscheduleMonitor(ctx, monitorID)
}
