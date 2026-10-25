package auth

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewRoute, NewTokenMaker, NewService, NewController, NewMiddlewareProvider)
}
