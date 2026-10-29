package setting

import (
	"peekaping/internal/limits"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewService, NewSQLWorkspaceRepository, NewWorkspaceService, provideAPIService, NewController, NewRoute)
}

type apiServiceParams struct {
	dig.In

	WorkspaceSettings WorkspaceService
	// Limits are the workspaces' limits, from package limits. A container without them has none.
	Limits limits.Source `optional:"true"`
}

func provideAPIService(p apiServiceParams) APIService {
	var opts []APIServiceOption
	if p.Limits != nil {
		opts = append(opts, WithLimits(p.Limits))
	}
	return NewAPIService(p.WorkspaceSettings, opts...)
}
