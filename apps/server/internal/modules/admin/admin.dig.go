package admin

import (
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	if err := utils.Provide(container, func(accounts *auth.AccountService) ResetLinks { return accounts }); err != nil {
		return err
	}
	if err := utils.Provide(container, func(service workspace.Service) WorkspaceDeleter { return service }); err != nil {
		return err
	}
	if err := utils.Provide(container, func(remover *auth.AccountRemover) AccountRemover { return remover }); err != nil {
		return err
	}
	return utils.Provide(container, NewSQLRepository, NewService, NewInstallSettings, NewController, NewRoute)
}
