package healthcheck

import (
	"vigi/internal/modules/healthcheck/executor"
)

// TickResult is what HandleMonitorTick returns
type TickResult struct {
	// Raw execution result
	ExecutionResult *executor.Result

	// Monitor that was checked
	Monitor *Monitor

	// Calculated ping time in milliseconds
	PingMs int

	// Whether the monitor is under maintenance
	IsUnderMaintenance bool
}
