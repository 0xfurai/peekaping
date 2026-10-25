package app

import (
	"context"
	"strings"
	"time"

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
// the api and in single mode; then StartErrorReportingPublisher in the cluster api). A test may
// change the options, to catch the events.
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
// settings, as it was before the restart. In the cluster api it also copies the choice to Redis,
// for the other processes. The caller logs a failure and goes on: reporting stays off.
func ApplyErrorReporting(ctx context.Context, container *dig.Container) error {
	return container.Invoke(func(service error_reporting.Service) error {
		return service.Apply(ctx)
	})
}

// StartErrorReportingPublisher makes the cluster api keep Redis true to the admin's choice: every
// interval until ctx is done it applies the stored setting, which turns reporting on or off in the
// api and copies the choice to Redis for the other processes. It is how a choice that missed a
// Redis that was down, or a key that Redis lost, is back within one interval, and how a change
// made on another api replica reaches this one. The channel it returns closes once it has stopped.
func StartErrorReportingPublisher(ctx context.Context, container *dig.Container, every time.Duration) (<-chan struct{}, error) {
	done := make(chan struct{})
	err := container.Invoke(func(service error_reporting.Service) {
		reporting.Go("error-reporting", func() {
			defer close(done)
			service.Run(ctx, every)
		})
	})
	if err != nil {
		return nil, err
	}
	return done, nil
}
