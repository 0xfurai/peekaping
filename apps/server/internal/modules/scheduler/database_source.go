package scheduler

import (
	"context"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/shared"
)

// Loader loads what a check of a monitor needs. check.Loader is one.
type Loader interface {
	Load(ctx context.Context, monitorID string) (*job.Job, error)
}

// MonitorFinder is the part of the monitor service the database source reads
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
	FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*shared.Monitor, error)
}

// DatabaseSource is the Source of the peekaping process: the monitors of the local probe in
// the database, their checks from the loader, and the monitor events on the event bus.
type DatabaseSource struct {
	monitors MonitorFinder
	loader   Loader
	eventBus events.EventBus
}

var _ Source = (*DatabaseSource)(nil)

// NewDatabaseSource creates the source of monitors that single mode runs the scheduler on
func NewDatabaseSource(monitors MonitorFinder, loader Loader, eventBus events.EventBus) *DatabaseSource {
	return &DatabaseSource{monitors: monitors, loader: loader, eventBus: eventBus}
}

// ListActive pages the active monitors the local probe runs
func (d *DatabaseSource) ListActive(ctx context.Context, page int, limit int) ([]*shared.Monitor, error) {
	return d.monitors.FindActiveByProbe(ctx, shared.LocalProbeID, page, limit)
}

// Find reads the monitor, and returns nil when it is gone or the local probe doesn't run it
func (d *DatabaseSource) Find(ctx context.Context, monitorID string) (*shared.Monitor, error) {
	mon, err := d.monitors.FindByID(ctx, monitorID)
	if err != nil || mon == nil || !mon.RunsOn(shared.LocalProbeID) {
		return nil, err
	}
	return mon, nil
}

// Load loads the check of the monitor
func (d *DatabaseSource) Load(ctx context.Context, monitorID string) (*job.Job, error) {
	return d.loader.Load(ctx, monitorID)
}

// Watch subscribes to the monitor events on the event bus. Pausing a monitor is an update.
func (d *DatabaseSource) Watch(created, updated, deleted func(monitorID string)) {
	subscriber := events.NewSubscriber(d.eventBus, "scheduler")
	events.Subscribe(subscriber, events.MonitorCreated, func(_ context.Context, mon shared.Monitor) { created(mon.ID) })
	events.Subscribe(subscriber, events.MonitorUpdated, func(_ context.Context, mon shared.Monitor) { updated(mon.ID) })
	events.Subscribe(subscriber, events.MonitorDeleted, func(_ context.Context, monitorID string) { deleted(monitorID) })
}
