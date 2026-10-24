package protocol

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies provides the protocol's middleware, controller and route, the Waker that
// ends waiting polls, the MonitorSource that says which monitors are a probe's, and the
// stale.ProbeOfflineFunc the stale-monitor watchdog asks. They need the probe service, which
// probe.RegisterDependencies provides, on a MongoDB install too: there it answers every call 501.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewAuth, NewRateLimiter, NewWaker, NewMonitorSource, MonitorProbeOffline, DefaultTiming, NewController, NewRoute)
}
