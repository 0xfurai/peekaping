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
	// RecordLocation stores what a result left on the monitor's row for the probe that ran it, and
	// returns the monitor's rows (the probes it runs on, in id order) as they were just before and
	// just after, read together with the write so that two results of one monitor see one another
	// in turn. A monitor without a row for the probe has nothing stored; one without rows returns
	// none.
	RecordLocation(ctx context.Context, monitorID string, state shared.LocationState) (before, after []shared.MonitorLocation, err error)
}
