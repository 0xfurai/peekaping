package limits

import (
	"peekaping/internal/installsettings"
	"peekaping/internal/utils"

	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// RegisterDependencies provides the install's Chain, as the Source the services enforce and as the
// one place a workspace's own limits are written, and the Usage that reports them
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideChain, func(c *Chain) Source { return c }, NewUsage)
}

func provideChain(db *bun.DB, settings *installsettings.Resolver) *Chain {
	return NewChain(NewSQLStore(db), SettingsDefaults{Settings: settings})
}
