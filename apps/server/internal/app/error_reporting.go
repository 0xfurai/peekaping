package app

import (
	"context"
	"strings"

	"peekaping/internal/config"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/reporting"
	"peekaping/internal/version"

	"go.uber.org/dig"
)

// SetupErrorReporting prepares error reporting in this process, right after the binary has
// registered its dependencies: the release, the mode its registration gives it (single or cluster,
// as for the usage ping), the storage backend, the DSN and whether the environment forces
// reporting off. Reporting stays off until the admin's choice is applied (ApplyErrorReporting in
// the api and in single mode). A test may change the options, to catch the events.
func SetupErrorReporting(container *dig.Container, cfg *config.Config, change ...func(*reporting.Options)) error {
	return container.Invoke(func(mode telemetry.Mode) {
		opts := errorReportingOptions(cfg, mode)
		for _, c := range change {
			c(&opts)
		}
		reporting.Setup(opts)
	})
}

func errorReportingOptions(cfg *config.Config, mode telemetry.Mode) reporting.Options {
	return reporting.Options{
		DSN:       cfg.ErrorReportingDSN,
		Release:   version.Version,
		Mode:      string(mode),
		DB:        storageTag(cfg.DBType),
		ForcedOff: cfg.ErrorReportingForcedOff(),
	}
}

// storageTag is the storage backend as an error report names it, or "unknown" for a DB_TYPE that
// names none of the backends
func storageTag(dbType string) string {
	switch strings.ToLower(dbType) {
	case "sqlite":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	}
	return "unknown"
}

// ApplyErrorReporting turns reporting on or off in this process by the choice stored in the
// settings, as it was before the restart. The caller logs a failure and goes on: reporting stays off.
func ApplyErrorReporting(ctx context.Context, container *dig.Container) error {
	return container.Invoke(func(service error_reporting.Service) error {
		return service.Apply(ctx)
	})
}
