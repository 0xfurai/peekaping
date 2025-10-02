package producer

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/queue"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

const (
	// TaskTypeHealthCheck is the task type for health check tasks
	TaskTypeHealthCheck = "monitor:healthcheck"
)

// HealthCheckTaskPayload is the payload for health check tasks
type HealthCheckTaskPayload struct {
	MonitorID   string    `json:"monitor_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// MonitorScheduler manages cron jobs for monitors
type MonitorScheduler struct {
	mu             sync.RWMutex
	cron           *cron.Cron
	jobs           map[string]cron.EntryID // monitor ID -> cron entry ID
	monitorService monitor.Service
	queueService   queue.Service
	logger         *zap.SugaredLogger
}

// NewMonitorScheduler creates a new monitor scheduler
func NewMonitorScheduler(
	monitorService monitor.Service,
	queueService queue.Service,
	logger *zap.SugaredLogger,
) *MonitorScheduler {
	return &MonitorScheduler{
		cron:           cron.New(cron.WithSeconds()),
		jobs:           make(map[string]cron.EntryID),
		monitorService: monitorService,
		queueService:   queueService,
		logger:         logger.With("component", "monitor_scheduler"),
	}
}

// Start starts the cron scheduler
func (ms *MonitorScheduler) Start() {
	ms.logger.Info("Starting monitor scheduler")
	ms.cron.Start()
}

// Stop stops the cron scheduler
func (ms *MonitorScheduler) Stop() {
	ms.logger.Info("Stopping monitor scheduler")
	ctx := ms.cron.Stop()
	<-ctx.Done()
	ms.logger.Info("Monitor scheduler stopped")
}

// SyncMonitors synchronizes all active monitors with the cron scheduler
func (ms *MonitorScheduler) SyncMonitors(ctx context.Context) error {
	ms.logger.Info("Syncing monitors with scheduler")

	// Get all active monitors
	monitors, err := ms.monitorService.FindActive(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active monitors: %w", err)
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Track which monitors are currently scheduled
	currentMonitorIDs := make(map[string]bool)
	for _, m := range monitors {
		currentMonitorIDs[m.ID] = true
	}

	// Remove jobs for monitors that are no longer active
	for monitorID, entryID := range ms.jobs {
		if !currentMonitorIDs[monitorID] {
			ms.cron.Remove(entryID)
			delete(ms.jobs, monitorID)
			ms.logger.Infow("Removed job for inactive monitor", "monitor_id", monitorID)
		}
	}

	// Add or update jobs for active monitors
	for _, m := range monitors {
		if err := ms.addOrUpdateMonitorJob(ctx, m); err != nil {
			ms.logger.Errorw("Failed to add/update monitor job",
				"monitor_id", m.ID,
				"monitor_name", m.Name,
				"error", err,
			)
		}
	}

	ms.logger.Infof("Synced %d active monitors", len(monitors))
	return nil
}

// AddMonitor adds a monitor to the scheduler
func (ms *MonitorScheduler) AddMonitor(ctx context.Context, monitorID string) error {
	m, err := ms.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !m.Active {
		ms.logger.Infow("Monitor is not active, skipping", "monitor_id", monitorID)
		return nil
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	return ms.addOrUpdateMonitorJob(ctx, m)
}

// RemoveMonitor removes a monitor from the scheduler
func (ms *MonitorScheduler) RemoveMonitor(monitorID string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if entryID, exists := ms.jobs[monitorID]; exists {
		ms.cron.Remove(entryID)
		delete(ms.jobs, monitorID)
		ms.logger.Infow("Removed monitor from scheduler", "monitor_id", monitorID)
	}
}

// UpdateMonitor updates a monitor's schedule
func (ms *MonitorScheduler) UpdateMonitor(ctx context.Context, monitorID string) error {
	m, err := ms.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Remove existing job if it exists
	if entryID, exists := ms.jobs[monitorID]; exists {
		ms.cron.Remove(entryID)
		delete(ms.jobs, monitorID)
	}

	if !m.Active {
		ms.logger.Infow("Monitor is not active, not rescheduling", "monitor_id", monitorID)
		return nil
	}

	return ms.addOrUpdateMonitorJob(ctx, m)
}

// addOrUpdateMonitorJob adds or updates a monitor job (must be called with lock held)
func (ms *MonitorScheduler) addOrUpdateMonitorJob(ctx context.Context, m *monitor.Model) error {
	// Remove existing job if it exists
	if entryID, exists := ms.jobs[m.ID]; exists {
		ms.cron.Remove(entryID)
		delete(ms.jobs, m.ID)
	}

	// Convert interval (seconds) to cron expression
	cronExpr := fmt.Sprintf("@every %ds", m.Interval)

	// Create the job function
	jobFunc := func() {
		if err := ms.enqueueHealthCheckTask(context.Background(), m.ID, m.Name); err != nil {
			ms.logger.Errorw("Failed to enqueue health check task",
				"monitor_id", m.ID,
				"monitor_name", m.Name,
				"error", err,
			)
		}
	}

	// Add the job to cron
	entryID, err := ms.cron.AddFunc(cronExpr, jobFunc)
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	ms.jobs[m.ID] = entryID
	ms.logger.Infow("Added/updated monitor job",
		"monitor_id", m.ID,
		"monitor_name", m.Name,
		"interval", m.Interval,
		"cron_expr", cronExpr,
	)

	return nil
}

// enqueueHealthCheckTask enqueues a health check task to the queue
func (ms *MonitorScheduler) enqueueHealthCheckTask(ctx context.Context, monitorID, monitorName string) error {
	payload := HealthCheckTaskPayload{
		MonitorID:   monitorID,
		ScheduledAt: time.Now().UTC(),
	}

	opts := &queue.EnqueueOptions{
		Queue:     "default",
		MaxRetry:  3,
		Timeout:   5 * time.Minute,
		Retention: 1 * time.Hour,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	ms.logger.Debugw("Enqueuing health check task",
		"monitor_id", monitorID,
		"monitor_name", monitorName,
		"payload", string(payloadJSON),
	)

	_, err = ms.queueService.Enqueue(ctx, TaskTypeHealthCheck, payload, opts)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	ms.logger.Debugw("Successfully enqueued health check task",
		"monitor_id", monitorID,
		"monitor_name", monitorName,
	)

	return nil
}

// GetStats returns statistics about the scheduler
func (ms *MonitorScheduler) GetStats() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return map[string]interface{}{
		"total_jobs":   len(ms.jobs),
		"cron_entries": len(ms.cron.Entries()),
	}
}
