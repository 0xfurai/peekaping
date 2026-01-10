package queue

import (
	"vigi/internal/config"

	"go.uber.org/dig"
)

// RegisterDependencies registers queue module dependencies
// The actual providers are in infra/queue.go to avoid asynq references in this module
func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	// No dependencies to register here
	// The queue service provider is in infra/queue.go
}
