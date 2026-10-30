package auth

import (
	"context"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
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
	return utils.Provide(container, NewSQLRepository, NewSQLAccountRepository, NewAccountRemover, NewAccountService, ProvideTokenMaker, NewRoute, ProvideService, NewSignupGate, NewController, NewMiddlewareProvider)
}

// ProvideService is the service of the app: it lets anyone register when the install's signup mode
// is open
func ProvideService(
	repo Repository,
	transactor dbtx.Transactor,
	workspaces workspace.Repository,
	invites Invites,
	tokenMaker *TokenMaker,
	verifier Verifier,
	remover *AccountRemover,
	cfg *config.Config,
	settings *installsettings.Resolver,
	logger *zap.SugaredLogger,
) Service {
	return NewService(repo, transactor, workspaces, invites, tokenMaker, verifier, logger, WithSignupMode(settings), WithDisabledMonitorTypes(cfg.DisabledMonitorTypeList()), WithMinCheckInterval(cfg.MinCheckIntervalSeconds()), WithSupportContact(cfg.SupportContact), WithCustomDomainVerification(cfg.VerifyCustomDomains, func(ctx context.Context) string {
		return config.CustomDomainCNAMETargetFor(cfg.CustomDomainCNAMETarget, settings.PublicURL(ctx))
	}), WithAccountRemoval(remover))
}

// ProvideTokenMaker is the token maker of the app: it refuses the tokens of sessions that a
// password reset ended
func ProvideTokenMaker(settings shared.SettingService, logger *zap.SugaredLogger, accounts AccountRepository) *TokenMaker {
	return NewTokenMaker(settings, logger).WithSessionCutoffs(accounts)
}
