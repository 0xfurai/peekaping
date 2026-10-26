package scheduler

import (
	"context"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/shared"
)

// Source is where the scheduler's monitors come from: the database and the event bus in the
// peekaping process, the assignment long-poll in a remote probe. The scheduler reads the
// monitors this process checks from it, loads each check from it, and hears from it when one of
// them is created, changed or gone. It knows nothing else about where they come from.
type Source interface {
	// ListActive returns a page of the active monitors this process checks. The scheduler reads
	// it when it starts and on every reconcile, page by page until a page is short.
	ListActive(ctx context.Context, page int, limit int) ([]*shared.Monitor, error)
	// Find returns the monitor as it is now, or nil when it is gone or is not this process's to
	// check (it moved to another probe).
	Find(ctx context.Context, monitorID string) (*shared.Monitor, error)
	// Load returns what one check of the monitor needs. It returns job.ErrMonitorNotFound for
	// a monitor that is gone and job.ErrMonitorPaused for one that is paused.
	Load(ctx context.Context, monitorID string) (*job.Job, error)
	// Watch calls the functions when a monitor is created, changed (paused and resumed
	// included) or gone. A call is only a notice: the scheduler reads the monitor with Find.
	Watch(created, updated, deleted func(monitorID string))
}
