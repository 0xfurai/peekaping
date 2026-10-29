// Package supervisor runs one check of a monitor: it bounds the monitor's executor by its timeout,
// and turns what the executor reports into the TickResult that the scheduler ingests. It has no storage or queue behind it, so the remote probe runs its checks with it too.
package supervisor

import (
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"

	"go.uber.org/zap"
)

type HealthCheckSupervisor struct {
	execRegistry *executor.ExecutorRegistry
	eventBus     events.EventBus
	logger       *zap.SugaredLogger
	// spacer spaces out the checks to one target host (CHECKS_PER_HOST_PER_MINUTE); off until set
	spacer *HostSpacer
}

func NewHealthCheck(
	eventBus events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		execRegistry: execRegistry,
		eventBus:     eventBus,
		logger:       logger.With("service", "[healthcheck]"),
		spacer:       NewHostSpacer(0),
	}
}

// SetChecksPerHostPerMinute caps how many checks to one target host the supervisor lets run per
// minute, all monitors together; 0 is off. It applies from the next check, which is how a remote
// probe follows the server's value at each hello.
func (s *HealthCheckSupervisor) SetChecksPerHostPerMinute(perMinute int) {
	s.spacer.SetPerMinute(perMinute)
}

// SetHostSpacer replaces the supervisor's spacer, for tests that need its clock
func (s *HealthCheckSupervisor) SetHostSpacer(spacer *HostSpacer) { s.spacer = spacer }

// ChecksPerHostPerMinute is the cap now; 0 is off
func (s *HealthCheckSupervisor) ChecksPerHostPerMinute() int { return s.spacer.PerMinute() }
