package protocol

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies provides the protocol's middleware, controller and route. They need the
// probe service, which probe.RegisterDependencies provides, on a MongoDB install too: there it
// answers every call 501.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewAuth, NewRateLimiter, NewController, NewRoute)
}
