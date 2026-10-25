package app

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

const testDSN = "https://public@sentry.invalid/1"

func withTransport(recorder *reportingtest.Recorder) func(*reporting.Options) {
	return func(o *reporting.Options) { o.Transport = recorder }
}

// What a report of each binary says about its install: the version, the mode its registration
// gives it (not the MODE setting), the storage backend, and the OS and architecture
func TestSetupErrorReporting_ReportsTheReleaseTheModeTheStorageAndThePlatform(t *testing.T) {
	type binary struct {
		register func(*dig.Container, *config.Config) error
		config   func(*testing.T) *config.Config
		mode, db string
	}
	postgres := func(t *testing.T) *config.Config {
		cfg := clusterConfig(t)
		cfg.DBType = "postgres"
		return cfg
	}
	for name, tc := range map[string]binary{
		"peekaping":         {RegisterSingle, singleConfig, "single", "sqlite"},
		"api":               {RegisterClusterAPI, clusterConfig, "cluster", "sqlite"},
		"api on PostgreSQL": {RegisterClusterAPI, postgres, "cluster", "postgres"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &reportingtest.Recorder{}
			cfg := tc.config(t)
			cfg.Mode = "prod" // MODE is dev, prod or test, and says nothing about single or cluster
			cfg.ErrorReportingDSN = testDSN
			container := dig.New()
			require.NoError(t, tc.register(container, cfg)) // provides the database, and connects to it only if something asks
			t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })

			require.NoError(t, SetupErrorReporting(container, cfg, withTransport(recorder)))
			reporting.SetEnabled(true)
			reporting.CapturePanic("test", "boom", nil)

			require.Len(t, recorder.Events(), 1)
			event := recorder.Events()[0]
			assert.Equal(t, version.Version, event.Release)
			assert.Equal(t, map[string]string{"mode": tc.mode, "db": tc.db, "os": runtime.GOOS + "/" + runtime.GOARCH, "component": "test"}, event.Tags)
		})
	}
}

func TestSetupErrorReporting_SendsNothingWhenTheEnvironmentForcesItOff(t *testing.T) {
	for name, set := range map[string]func(*config.Config){
		"ERROR_REPORTING=off": func(c *config.Config) { c.ErrorReporting = "off" },
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &reportingtest.Recorder{}
			cfg := singleConfig(t)
			cfg.ErrorReportingDSN = testDSN
			set(cfg)
			container := dig.New()
			require.NoError(t, RegisterSingle(container, cfg))
			t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })

			require.NoError(t, SetupErrorReporting(container, cfg, withTransport(recorder)))
			reporting.SetEnabled(true) // the admin's choice
			reporting.CapturePanic("test", "boom", nil)

			assert.False(t, reporting.Enabled())
			assert.Empty(t, recorder.Events())
		})
	}
}

type fakeErrorReportingService struct {
	error_reporting.Service
	applied int
	err     error
}

func (f *fakeErrorReportingService) Apply(context.Context) error {
	f.applied++
	return f.err
}

func TestApplyErrorReporting_AsksTheServiceToApplyTheStoredChoice(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))
	service := &fakeErrorReportingService{}
	require.NoError(t, container.Decorate(func(error_reporting.Service) error_reporting.Service { return service }))

	require.NoError(t, ApplyErrorReporting(context.Background(), container))
	assert.Equal(t, 1, service.applied)

	service.err = errors.New("no such table: settings")
	assert.ErrorContains(t, ApplyErrorReporting(context.Background(), container), "no such table")
}
