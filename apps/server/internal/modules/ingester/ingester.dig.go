package ingester

import (
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/stats"
	"peekaping/internal/utils"

	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers ingester dependencies in the DI container: the location store and
// the service that stores check results
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLocationStore, provideService)
}

// ProvideLocationStore has the ingester keep a monitor's state on each probe in the monitor's own
// repository, where the monitor API reads it from
func ProvideLocationStore(repository monitor.MonitorRepository) LocationStore {
	return repository
}

type serviceParams struct {
	dig.In

	HeartbeatService   heartbeat.Service
	CertificateService certificate.Service
	Locations          LocationStore
	EventBus           events.EventBus
	Logger             *zap.SugaredLogger
	DB                 *bun.DB       `optional:"true"`
	StatsService       stats.Service `optional:"true"`
}

func provideService(p serviceParams) Service {
	var opts []ServiceOption
	if p.DB != nil {
		opts = append(opts, WithSlowResponse(NewSQLSlowResponseStore(p.DB), p.StatsService))
	}
	return NewService(p.HeartbeatService, p.CertificateService, p.Locations, p.EventBus, p.Logger, opts...)
}
