package auth

import (
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	if err := utils.Provide(container, func(service workspace.Service) Invites { return service }); err != nil {
		return err
	}
	if err := utils.Provide(container, func(accounts *AccountService) Verifier { return accounts }); err != nil {
		return err
	}
	return utils.Provide(container, NewSQLRepository, NewSQLAccountRepository, NewAccountService, ProvideTokenMaker, NewRoute, NewService, NewController, NewMiddlewareProvider)
}

// ProvideTokenMaker is the token maker of the app: it refuses the tokens of sessions that a
// password reset ended
func ProvideTokenMaker(settings shared.SettingService, logger *zap.SugaredLogger, accounts AccountRepository) *TokenMaker {
	return NewTokenMaker(settings, logger).WithSessionCutoffs(accounts)
}
