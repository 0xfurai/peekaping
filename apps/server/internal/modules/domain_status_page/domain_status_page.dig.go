package domain_status_page

import (
	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewNetResolver, provideService)
}

// serviceParams are what the service is built from
type serviceParams struct {
	dig.In

	Repository Repository
	Resolver   Resolver
	Config     *config.Config
	// Settings give the public URL. A container without them has no CNAME target of its own.
	Settings *installsettings.Resolver `optional:"true"`
	Logger   *zap.SugaredLogger
}

// provideService is the service of the app: it verifies custom domains when VERIFY_CUSTOM_DOMAINS
// says so
func provideService(p serviceParams) Service {
	opts := []Option{WithVerification(p.Config.VerifyCustomDomains, p.Config.CustomDomainCNAMETarget), WithResolver(p.Resolver)}
	if p.Settings != nil {
		opts = append(opts, WithPublicURL(p.Settings))
	}
	return NewService(p.Repository, p.Logger, opts...)
}
