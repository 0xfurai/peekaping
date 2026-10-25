package auth

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	return utils.Provide(container, NewSQLRepository, NewRoute, NewTokenMaker, NewService, NewController, NewMiddlewareProvider)
}
