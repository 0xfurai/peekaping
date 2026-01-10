package healthcheck

import (
	"context"
	"vigi/internal/modules/healthcheck/executor"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/shared"
	"time"
)

// isImportantForNotification and isImportantBeat have been moved to the ingester
// where all heartbeat processing logic now resides

// postProcessHeartbeat has been removed - all logic moved to the ingester
// This includes:
// - Getting previous heartbeat from database
// - Creating heartbeat with retry/pending logic
// - Determining if beat is important
// - Determining if notification should be sent
// - Managing down count
// - Storing heartbeat in database
// - Publishing events
// - Updating TLS info
// - Checking certificate expiry

// HandleMonitorTick processes a single monitor tick and returns the result.
// It does NOT save to the database - that's the ingester's job.
func (s *HealthCheckSupervisor) HandleMonitorTick(
	ctx context.Context,
	m *Monitor,
	exec executor.Executor,
	proxyModel *proxy.Model,
	isUnderMaintenance bool,
) *TickResult {
	// Use the maintenance status provided via queue payload (no database call)
	s.logger.Debugf("isUnderMaintenance for %s: %t", m.Name, isUnderMaintenance)

	if isUnderMaintenance {
		// If under maintenance, create a maintenance status heartbeat
		result := &executor.Result{
			Status:    shared.MonitorStatusMaintenance,
			Message:   "Monitor under maintenance",
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}
		ping := int(result.EndTime.Sub(result.StartTime).Milliseconds())
		return &TickResult{
			ExecutionResult:    result,
			Monitor:            m,
			PingMs:             ping,
			IsUnderMaintenance: true,
		}
	}

	callCtx, cCancel := context.WithTimeout(
		ctx,
		time.Duration(m.Timeout)*time.Second,
	)
	defer cCancel()

	// Execute the health check
	result := exec.Execute(callCtx, m, proxyModel)
	if result == nil {
		return nil
	}

	ping := int(result.EndTime.Sub(result.StartTime).Milliseconds())

	return &TickResult{
		ExecutionResult:    result,
		Monitor:            m,
		PingMs:             ping,
		IsUnderMaintenance: false,
	}
}
