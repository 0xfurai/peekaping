package producer

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeHealthCheck is the task type for health check tasks
	TaskTypeHealthCheck = "monitor:healthcheck"
)

// ProxyData contains proxy configuration for health checks
type ProxyData struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Auth     bool   `json:"auth"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// HealthCheckTaskPayload is the payload for health check tasks
type HealthCheckTaskPayload struct {
	MonitorID          string     `json:"monitor_id"`
	MonitorName        string     `json:"monitor_name"`
	MonitorType        string     `json:"monitor_type"`
	Interval           int        `json:"interval"`
	Timeout            int        `json:"timeout"`
	MaxRetries         int        `json:"max_retries"`
	RetryInterval      int        `json:"retry_interval"`
	ResendInterval     int        `json:"resend_interval"`
	Config             string     `json:"config"`
	Proxy              *ProxyData `json:"proxy,omitempty"`
	ScheduledAt        time.Time  `json:"scheduled_at"`
	IsUnderMaintenance bool       `json:"is_under_maintenance"`
	CheckCertExpiry    bool       `json:"check_cert_expiry"`
}

// MonitorScheduler manages periodic tasks for monitors using asynq scheduler
type MonitorScheduler struct {
	mu                 sync.RWMutex
	scheduler          *asynq.Scheduler
	jobs               map[string]string // monitor ID -> entry ID (for tracking)
	monitorService     monitor.Service
	proxyService       proxy.Service
	maintenanceService maintenance.Service
	logger             *zap.SugaredLogger
}

// NewMonitorScheduler creates a new monitor scheduler
func NewMonitorScheduler(
	scheduler *asynq.Scheduler,
	monitorService monitor.Service,
	proxyService proxy.Service,
	maintenanceService maintenance.Service,
	logger *zap.SugaredLogger,
) *MonitorScheduler {
	return &MonitorScheduler{
		scheduler:          scheduler,
		jobs:               make(map[string]string),
		monitorService:     monitorService,
		proxyService:       proxyService,
		maintenanceService: maintenanceService,
		logger:             logger.With("component", "monitor_scheduler"),
	}
}

// Start starts the asynq scheduler
func (ms *MonitorScheduler) Start() error {
	ms.logger.Info("Starting monitor scheduler")
	if err := ms.scheduler.Run(); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}
	return nil
}

// Stop stops the asynq scheduler
func (ms *MonitorScheduler) Stop() {
	ms.logger.Info("Stopping monitor scheduler")
	ms.scheduler.Shutdown()
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
	for monitorID := range ms.jobs {
		if !currentMonitorIDs[monitorID] {
			entryID := ms.getEntryID(monitorID)
			if err := ms.scheduler.Unregister(entryID); err != nil {
				ms.logger.Warnw("Failed to unregister job",
					"monitor_id", monitorID,
					"entry_id", entryID,
					"error", err,
				)
			}
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

	if _, exists := ms.jobs[monitorID]; exists {
		entryID := ms.getEntryID(monitorID)
		if err := ms.scheduler.Unregister(entryID); err != nil {
			ms.logger.Warnw("Failed to unregister monitor",
				"monitor_id", monitorID,
				"entry_id", entryID,
				"error", err,
			)
		}
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
	if _, exists := ms.jobs[monitorID]; exists {
		entryID := ms.getEntryID(monitorID)
		if err := ms.scheduler.Unregister(entryID); err != nil {
			ms.logger.Warnw("Failed to unregister existing job",
				"monitor_id", monitorID,
				"entry_id", entryID,
				"error", err,
			)
		}
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
	if _, exists := ms.jobs[m.ID]; exists {
		entryID := ms.getEntryID(m.ID)
		if err := ms.scheduler.Unregister(entryID); err != nil {
			ms.logger.Warnw("Failed to unregister existing job",
				"monitor_id", m.ID,
				"entry_id", entryID,
				"error", err,
			)
		}
		delete(ms.jobs, m.ID)
	}

	// Prepare the task payload
	payload, err := ms.buildHealthCheckPayload(ctx, m)
	if err != nil {
		return fmt.Errorf("failed to build payload: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the task
	task := asynq.NewTask(TaskTypeHealthCheck, payloadJSON)

	// Convert interval (seconds) to cron expression
	cronExpr := fmt.Sprintf("@every %ds", m.Interval)

	// Create unique entry ID for this monitor
	entryID := ms.getEntryID(m.ID)

	// Register the periodic task
	_, err = ms.scheduler.Register(
		cronExpr,
		task,
		asynq.Queue("healthcheck"),
		asynq.MaxRetry(3),
		asynq.Timeout(time.Duration(m.Interval)*time.Second),
		asynq.Retention(1*time.Minute),
		asynq.TaskID(entryID),
	)
	if err != nil {
		return fmt.Errorf("failed to register periodic task: %w", err)
	}

	ms.jobs[m.ID] = entryID
	ms.logger.Infow("Added/updated monitor job",
		"monitor_id", m.ID,
		"monitor_name", m.Name,
		"interval", m.Interval,
		"cron_expr", cronExpr,
		"entry_id", entryID,
	)

	return nil
}

// getEntryID generates a unique entry ID for a monitor
func (ms *MonitorScheduler) getEntryID(monitorID string) string {
	return fmt.Sprintf("monitor:%s:healthcheck", monitorID)
}

// buildHealthCheckPayload builds the health check payload for a monitor
func (ms *MonitorScheduler) buildHealthCheckPayload(ctx context.Context, m *monitor.Model) (*HealthCheckTaskPayload, error) {

	// Check if monitor is under maintenance
	// isUnderMaintenance := false
	// maintenances, err := ms.maintenanceService.GetMaintenancesByMonitorID(ctx, m.ID)
	// if err != nil {
	// 	ms.logger.Warnw("Failed to get maintenances for monitor, assuming not under maintenance",
	// 		"monitor_id", m.ID,
	// 		"error", err,
	// 	)
	// } else {
	// 	for _, maint := range maintenances {
	// 		underMaintenance, err := ms.maintenanceService.IsUnderMaintenance(ctx, maint)
	// 		if err != nil {
	// 			ms.logger.Warnw("Failed to check maintenance status, skipping",
	// 				"maintenance_id", maint.ID,
	// 				"error", err,
	// 			)
	// 			continue
	// 		}
	// 		if underMaintenance {
	// 			isUnderMaintenance = true
	// 			break
	// 		}
	// 	}
	// }

	// Check if certificate expiry checking is enabled (for HTTPS monitors)
	checkCertExpiry := false
	if strings.HasPrefix(strings.ToLower(m.Type), "http") && m.Config != "" {
		var httpConfig struct {
			CheckCertExpiry bool `json:"check_cert_expiry"`
		}
		if err := json.Unmarshal([]byte(m.Config), &httpConfig); err != nil {
			ms.logger.Warnw("Failed to parse HTTP config for monitor, assuming cert check disabled",
				"monitor_id", m.ID,
				"error", err,
			)
		} else {
			checkCertExpiry = httpConfig.CheckCertExpiry
		}
	}

	// Prepare the payload with all monitor data
	payload := &HealthCheckTaskPayload{
		MonitorID:          m.ID,
		MonitorName:        m.Name,
		MonitorType:        m.Type,
		Interval:           m.Interval,
		Timeout:            m.Timeout,
		MaxRetries:         m.MaxRetries,
		RetryInterval:      m.RetryInterval,
		ResendInterval:     m.ResendInterval,
		Config:             m.Config,
		ScheduledAt:        time.Now().UTC(),
		IsUnderMaintenance: false,
		CheckCertExpiry:    checkCertExpiry,
	}

	// Fetch proxy data if monitor has a proxy configured
	if m.ProxyId != "" {
		p, err := ms.proxyService.FindByID(ctx, m.ProxyId)
		if err != nil {
			ms.logger.Warnw("Failed to fetch proxy for monitor, proceeding without proxy",
				"monitor_id", m.ID,
				"proxy_id", m.ProxyId,
				"error", err,
			)
		} else if p != nil {
			payload.Proxy = &ProxyData{
				ID:       p.ID,
				Protocol: p.Protocol,
				Host:     p.Host,
				Port:     p.Port,
				Auth:     p.Auth,
				Username: p.Username,
				Password: p.Password,
			}
		}
	}

	return payload, nil
}

// GetStats returns statistics about the scheduler
func (ms *MonitorScheduler) GetStats() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return map[string]interface{}{
		"total_jobs": len(ms.jobs),
	}
}
