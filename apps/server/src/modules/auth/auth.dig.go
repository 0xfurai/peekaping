package auth

import (
	"peekaping/src/config"
	"peekaping/src/modules/auth/login_attempt"
	"peekaping/src/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)
	utils.RegisterRepositoryByDBType(container, cfg, login_attempt.NewLoginAttemptSQLRepository, login_attempt.NewLoginAttemptMongoRepository)

	container.Provide(NewRoute)
	container.Provide(NewTokenMaker)
	container.Provide(NewService)
	container.Provide(NewController)
	container.Provide(login_attempt.NewBruteforceService)
	container.Provide(login_attempt.NewBruteforceMiddleware)
	container.Provide(NewMiddlewareProvider)
	container.Provide(login_attempt.NewCleanupService)
}
