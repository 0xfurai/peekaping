package monitor

import (
	"context"

	"peekaping/internal/modules/shared"
)

type MonitorRepository interface {
	Create(ctx context.Context, monitor *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Model, error)
	FindAll(
		ctx context.Context,
		page int,
		limit int,
		q string,
		active *bool,
		status *int,
		tagIds []string,
	) ([]*Model, error)
	FindActive(ctx context.Context) ([]*Model, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error)
	// FindActiveByProbe returns a page of the active monitors the probe runs
	FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, monitor *Model) error
	UpdatePartial(ctx context.Context, id string, monitor *UpdateModel) error
	Delete(ctx context.Context, id string) error
	RemoveProxyReference(ctx context.Context, proxyId string) error
	FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error)
	FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error)
	// RecordLocation stores what a result left on the monitor's row for the probe that ran it,
	// then calls judge with the monitor's last recorded status and its rows from just before and
	// just after, and records the status judge returns as the monitor's (nil clears it). All of it
	// happens with the monitor held, so two results of one monitor see one another in turn: each
	// one's recorded status is what the other's judge returned. A monitor without a row for the
	// probe has nothing stored on the row, and one without rows is judged on none.
	RecordLocation(ctx context.Context, monitorID string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error
	// ResetLocations forgets where the monitor stood: its probes' rows lose their state and the
	// monitor loses its recorded status, so that its next result is its first
	ResetLocations(ctx context.Context, monitorID string) error
}
