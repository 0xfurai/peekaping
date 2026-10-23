package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe_notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

func TestUnsupportedRepository_EveryMethodReportsUnsupportedStorage(t *testing.T) {
	ctx := context.Background()
	repo := probe.NewUnsupportedRepository()
	name := "Paris"

	_, err := repo.Create(ctx, &probe.CreateModel{Name: "Frankfurt"})
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.FindAll(ctx)
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.FindByID(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.FindByTokenHash(ctx, "sha256$aa")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.Update(ctx, "local", &probe.UpdateModel{Name: &name})
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, repo.UpdateToken(ctx, "local", "sha256$aa", "pkp_aaaa"), probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, repo.Delete(ctx, "local"), probe.ErrUnsupportedStorage)
	_, err = repo.CountMonitors(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.FindInstances(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, repo.UpsertInstance(ctx, "local", &probe.Instance{ID: "instance-1"}), probe.ErrUnsupportedStorage)
	_, err = repo.TouchInstance(ctx, "local", "instance-1", time.Now())
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, repo.RetireInstance(ctx, "local", "instance-1", time.Now()), probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, repo.SetSupportedTypes(ctx, "local", []string{"http"}), probe.ErrUnsupportedStorage)
	_, err = repo.UpdateStatus(ctx, "local", probe.StatusOffline, probe.StatusOnline)
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = repo.DeleteInstancesUnseenSince(ctx, time.Now())
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)

	assert.EqualError(t, probe.ErrUnsupportedStorage, "probes need SQLite or PostgreSQL storage")
}

func TestService_OnMongoListsNoProbesAndRefusesTheRest(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop().Sugar()
	channels := probe_notification.NewService(probe_notification.NewUnsupportedRepository(), logger)
	svc := probe.NewService(probe.NewUnsupportedRepository(), dbtx.NewMongoTransactor(), channels, logger)
	name := "Paris"

	all, err := svc.FindAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, []*probe.Model{}, all)

	_, err = svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = svc.FindByID(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = svc.Update(ctx, "local", &probe.UpdateRequest{Name: &name})
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, svc.Delete(ctx, "local"), probe.ErrUnsupportedStorage)
	_, err = svc.RotateToken(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = svc.FindByToken(ctx, "some-token")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, svc.RegisterInstance(ctx, "local", &probe.Instance{ID: "instance-1"}, []string{"http"}), probe.ErrUnsupportedStorage)
	_, err = svc.TouchInstance(ctx, "local", "instance-1", time.Now())
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	assert.ErrorIs(t, svc.RetireInstance(ctx, "local", "instance-1", time.Now()), probe.ErrUnsupportedStorage)
	_, err = svc.MarkOnline(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)
	_, err = svc.MarkOffline(ctx, "local")
	assert.ErrorIs(t, err, probe.ErrUnsupportedStorage)

	assert.NoError(t, svc.Bootstrap(ctx, ""), "with no bootstrap token there is nothing to store")
	assert.ErrorIs(t, svc.Bootstrap(ctx, bootstrapToken), probe.ErrUnsupportedStorage)
}

// A MongoDB install has no *bun.DB. Its probe services must still resolve, so that it starts.
func TestRegisterDependencies_MongoStarts(t *testing.T) {
	cfg := &config.Config{DBType: "mongo"}
	container := dig.New()
	require.NoError(t, container.Provide(func() *zap.SugaredLogger { return zap.NewNop().Sugar() }))
	require.NoError(t, monitor.RegisterDependencies(container, cfg)) // provides the dbtx.Transactor
	require.NoError(t, probe_notification.RegisterDependencies(container, cfg))
	require.NoError(t, probe.RegisterDependencies(container, cfg))

	require.NoError(t, container.Invoke(func(svc probe.Service, channels probe_notification.Service) {
		all, err := svc.FindAll(context.Background())
		require.NoError(t, err)
		assert.Empty(t, all)
		assert.NoError(t, channels.DeleteByNotificationID(context.Background(), "any"))
	}))
}

func TestRegisterDependencies_SQLiteResolvesTheSQLRepository(t *testing.T) {
	cfg := &config.Config{DBType: "sqlite"}
	container := dig.New()
	require.NoError(t, container.Provide(func() *zap.SugaredLogger { return zap.NewNop().Sugar() }))
	require.NoError(t, container.Provide(func() *bun.DB { return sqliteDB(t) }))
	require.NoError(t, monitor.RegisterDependencies(container, cfg)) // provides the dbtx.Transactor
	require.NoError(t, probe_notification.RegisterDependencies(container, cfg))
	require.NoError(t, probe.RegisterDependencies(container, cfg))

	require.NoError(t, container.Invoke(func(repo probe.Repository, svc probe.Service) {
		assert.IsType(t, &probe.SQLRepositoryImpl{}, repo)
		assert.NotNil(t, svc)
	}))
}
