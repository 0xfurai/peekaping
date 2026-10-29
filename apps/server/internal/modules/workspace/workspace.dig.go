package workspace

import (
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/limits"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/events"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewSQLMemberRepository, NewSQLInviteRepository, provideService)
}

// serviceParams are what NewService takes
type serviceParams struct {
	dig.In

	Workspaces Repository
	Members    MemberRepository
	Invites    InviteRepository
	Transactor dbtx.Transactor
	Bus        events.EventBus
	Config     *config.Config
	Mailer     *mailer.Mailer
	Monitors   MonitorWaker
	Logger     *zap.SugaredLogger
	// Limits are the workspaces' limits, from package limits. A container without them has none.
	Limits limits.Source `optional:"true"`
}

func provideService(p serviceParams) Service {
	var opts []ServiceOption
	if p.Limits != nil {
		opts = append(opts, WithLimits(p.Limits))
	}
	return NewService(p.Workspaces, p.Members, p.Invites, p.Transactor, p.Bus, p.Config, p.Mailer, p.Monitors, p.Logger, opts...)
}
