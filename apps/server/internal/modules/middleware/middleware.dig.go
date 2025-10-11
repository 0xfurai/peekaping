package middleware

import (
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) {
	// Provide authentication chain
	container.Provide(func(
		jwtMiddleware *auth.MiddlewareProvider,
		apiKeyMiddleware *api_key.MiddlewareProvider,
		logger *zap.SugaredLogger,
	) *AuthChain {
		return NewAuthChain(jwtMiddleware, apiKeyMiddleware, logger)
	})
}
