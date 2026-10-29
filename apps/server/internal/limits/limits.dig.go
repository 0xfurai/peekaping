package limits

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// RegisterDependencies provides the install's Chain, as the Source the services enforce and as the
// one place a workspace's own limits are written
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideChain, func(c *Chain) Source { return c })
}

func provideChain(db *bun.DB, cfg *config.Config) *Chain {
	return NewChain(NewSQLStore(db), FromConfig(cfg))
}
