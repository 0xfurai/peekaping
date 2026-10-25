package supervisor

import (
	"context"
	"fmt"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting"
	"runtime/debug"
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
	m *shared.Monitor,
	exec executor.Executor,
	proxyModel *shared.Proxy,
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
	result := s.executeWithDeadline(callCtx, m, exec, proxyModel)
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

// ExecutorGrace is how long an executor may keep running after the check
// timeout before the check is reported DOWN without waiting for it.
const ExecutorGrace = 2 * time.Second

// executeWithDeadline bounds a check by ctx even when the executor ignores it,
// so the caller always gets a result shortly after the check timeout.
func (s *HealthCheckSupervisor) executeWithDeadline(
	ctx context.Context,
	m *shared.Monitor,
	exec executor.Executor,
	proxyModel *shared.Proxy,
) *executor.Result {
	startTime := time.Now().UTC()
	done := make(chan *executor.Result, 1)
	go func() {
		// A panic in this goroutine would stop the process, so it is recovered here
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				s.logger.Errorw("Health check executor panicked",
					"monitor_id", m.ID,
					"monitor_type", m.Type,
					"panic", r,
					"stack", string(stack))
				reporting.CapturePanic("executor", r, stack)
				done <- executor.DownResult(fmt.Errorf("check failed with an internal error: %v", r), startTime, time.Now().UTC())
			}
		}()
		done <- exec.Execute(ctx, m, proxyModel)
	}()

	select {
	case result := <-done:
		return result
	case <-ctx.Done():
	}

	// Executors that honor ctx return right after it expires, with their own message.
	select {
	case result := <-done:
		return result
	case <-time.After(ExecutorGrace):
		return executor.DownResult(
			fmt.Errorf("check did not finish within the %ds timeout", m.Timeout),
			startTime,
			time.Now().UTC(),
		)
	}
}
