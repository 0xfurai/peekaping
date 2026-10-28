package admin

import (
	"peekaping/internal/modules/auth"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	if err := utils.Provide(container, func(accounts *auth.AccountService) ResetLinks { return accounts }); err != nil {
		return err
	}
	return utils.Provide(container, NewSQLRepository, NewService, NewController, NewRoute)
}
