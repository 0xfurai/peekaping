package auth

import (
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	if err := utils.Provide(container, func(service workspace.Service) Invites { return service }); err != nil {
		return err
	}
	return utils.Provide(container, NewSQLRepository, NewRoute, NewTokenMaker, NewService, NewController, NewMiddlewareProvider)
}
