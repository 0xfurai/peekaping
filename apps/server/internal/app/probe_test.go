package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/probe_notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// bootstrapToken is 43 characters
const bootstrapToken = "bootstrap-token-0123456789-abcdefghijklmnop"

// probeContainer registers the app on cfg, with a logger the test reads, and migrates its SQLite
// database unless cfg names another storage backend
func probeContainer(t *testing.T, cfg *config.Config, migrateDatabase bool) (*dig.Container, *observer.ObservedLogs) {
	t.Helper()
	container := dig.New()
	require.NoError(t, Register(container, cfg))
	core, logs := observer.New(zap.DebugLevel)
	require.NoError(t, container.Decorate(func() *zap.SugaredLogger { return zap.New(core).Sugar() }))
	if cfg.DBType == "sqlite" {
		require.NoError(t, container.Invoke(func(db *bun.DB) error {
			t.Cleanup(func() { assert.NoError(t, db.Close()) })
			if !migrateDatabase {
				return nil
			}
			return migrateDB(db)
		}))
	}
	return container, logs
}

// migrateDB migrates the database as the startup script does with cmd/bun
func migrateDB(db *bun.DB) error {
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	if err := migrator.Init(ctx); err != nil {
		return err
	}
	_, err := migrator.Migrate(ctx)
	return err
}

func findByToken(t *testing.T, container *dig.Container, token string) *probe.Model {
	t.Helper()
	var found *probe.Model
	require.NoError(t, container.Invoke(func(service probe.Service) error {
		var err error
		found, err = service.FindByToken(context.Background(), token)
		return err
	}))
	return found
}

// noEntryHolds asserts that nothing the logger recorded, message or fields, contains text
func noEntryHolds(t *testing.T, logs *observer.ObservedLogs, text string) {
	t.Helper()
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, text)
		assert.NotContains(t, fmt.Sprint(entry.ContextMap()), text)
	}
}

func TestRegister_SQLiteInstallResolvesTheProbeServices(t *testing.T) {
	container := dig.New()
	require.NoError(t, Register(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(probes probe.Service, channels probe_notification.Service) {
		assert.NotNil(t, probes)
		assert.NotNil(t, channels)
	}))
}

func TestBootstrapLocalProbe_StoresTheTokenOnLocal(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.ProbeBootstrapToken = bootstrapToken
	container, logs := probeContainer(t, cfg, true)

	require.NoError(t, BootstrapLocalProbe(context.Background(), container))

	local := findByToken(t, container, bootstrapToken)
	require.NotNil(t, local, "the bootstrap token finds the local probe")
	assert.Equal(t, "local", local.ID)
	assert.Equal(t, "bootstra", local.TokenPrefix)
	applied := logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All()
	require.Len(t, applied, 1)
	assert.Equal(t, zap.InfoLevel, applied[0].Level)
	assert.Contains(t, applied[0].Message, "local probe")
	noEntryHolds(t, logs, bootstrapToken)
	noEntryHolds(t, logs, "bootstra")

	// A restart with the same token changes nothing
	require.NoError(t, BootstrapLocalProbe(context.Background(), container))
	assert.NotNil(t, findByToken(t, container, bootstrapToken))
}

func TestBootstrapLocalProbe_WithoutATokenLeavesLocal(t *testing.T) {
	cfg := clusterConfig(t)
	container, logs := probeContainer(t, cfg, true)

	require.NoError(t, BootstrapLocalProbe(context.Background(), container))

	assert.Empty(t, logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All())
	require.NoError(t, container.Invoke(func(service probe.Service) error {
		local, err := service.FindByID(context.Background(), probe.LocalID)
		require.NotNil(t, local)
		assert.Equal(t, "", local.TokenHash, "local has no token")
		assert.Equal(t, "", local.TokenPrefix)
		return err
	}))
}

func TestBootstrapLocalProbe_WithoutATokenNeedsNoProbesTable(t *testing.T) {
	cfg := clusterConfig(t)
	container, _ := probeContainer(t, cfg, false)

	assert.NoError(t, BootstrapLocalProbe(context.Background(), container), "with no token it doesn't read the database")
}

func TestBootstrapLocalProbe_FailsWhenTheTokenCantBeStored(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.ProbeBootstrapToken = bootstrapToken
	container, _ := probeContainer(t, cfg, false)

	err := BootstrapLocalProbe(context.Background(), container)

	require.Error(t, err, "a token that can't be stored would leave the local probe with none, so startup stops")
	assert.NotContains(t, err.Error(), bootstrapToken)
}

func TestBootstrapLocalProbe_RefusesATokenUnder32Characters(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.ProbeBootstrapToken = strings.Repeat("k", 31)
	container, _ := probeContainer(t, cfg, true)

	err := BootstrapLocalProbe(context.Background(), container)

	assert.ErrorContains(t, err, "PROBE_BOOTSTRAP_TOKEN must be at least 32 characters")
}

// discardingIngester is an ingester.Service that stores nothing
type discardingIngester struct{}

func (discardingIngester) Ingest(context.Context, *ingester.Result) error { return nil }

func TestRegister_SQLiteInstallResolvesTheProbeProtocol(t *testing.T) {
	container := dig.New()
	require.NoError(t, Register(container, clusterConfig(t)))
	// The transport provides the event bus that the waker and the controller publish to, and the
	// ingester service that stores the results a probe sends
	bus := events.NewMemoryBus()
	t.Cleanup(func() { assert.NoError(t, bus.Close(context.Background())) })
	require.NoError(t, container.Provide(func() events.EventBus { return bus }))
	require.NoError(t, container.Provide(func() ingester.Service { return discardingIngester{} }))

	require.NoError(t, container.Invoke(func(route *protocol.Route, controller *protocol.Controller, limiter *protocol.RateLimiter, waker *protocol.Waker, source protocol.MonitorSource) {
		assert.NotNil(t, route)
		assert.NotNil(t, controller)
		assert.NotNil(t, limiter)
		assert.NotNil(t, waker)
		assert.NotNil(t, source)
	}))
}
