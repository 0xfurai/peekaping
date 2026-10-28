package proxy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// removal is one call of RemoveProxyReference
type removal struct {
	workspace workspace.ID
	proxy     string
}

// recordingMonitors answers the one monitor service call the proxy service makes
type recordingMonitors struct {
	monitor.Service
	removals []removal
	err      error
	// onRemove runs inside the call, to see what the proxy is like by then
	onRemove func()
}

func (m *recordingMonitors) RemoveProxyReference(_ context.Context, workspaceID workspace.ID, proxyID string) error {
	m.removals = append(m.removals, removal{workspaceID, proxyID})
	if m.onRemove != nil {
		m.onRemove()
	}
	return m.err
}

type harness struct {
	db       *bun.DB
	svc      proxy.Service
	monitors *recordingMonitors
	// ws is the workspace the tests' requests run in: the install's oldest one
	ws workspace.ID
}

// newHarness builds the proxy service over a migrated SQLite database, with foreign keys on
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	logger := zap.NewNop().Sugar()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "proxy.db")}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)

	monitors := &recordingMonitors{}
	svc := proxy.NewService(proxy.NewServiceParams{Repository: proxy.NewSQLRepository(db), MonitorService: monitors, Logger: logger})
	return &harness{db: db, svc: svc, monitors: monitors, ws: workspace.ID(workspacetest.OldestOrAdd(t, db))}
}

func (h *harness) create(t *testing.T, workspaceID workspace.ID, host string) *proxy.Model {
	t.Helper()
	created, err := h.svc.Create(context.Background(), workspaceID, &proxy.CreateUpdateDto{Protocol: "http", Host: host, Port: 8080})
	require.NoError(t, err)
	return created
}

func ptr[T any](v T) *T { return &v }

func TestProxyService_CreateStoresTheProxyInTheWorkspace(t *testing.T) {
	h := newHarness(t)
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")

	mine := h.create(t, h.ws, "proxy.example.com")
	theirs := h.create(t, workspacetest.Other, "proxy.example.com")

	assert.Equal(t, h.ws, mine.WorkspaceID)
	assert.Equal(t, workspacetest.Other, theirs.WorkspaceID)
	assert.Equal(t, "proxy.example.com", mine.Host)
	found, err := h.svc.FindByID(context.Background(), h.ws, mine.ID)
	require.NoError(t, err)
	assert.Equal(t, mine.ID, found.ID)
	assert.Equal(t, h.ws, found.WorkspaceID)
}

func TestProxyService_APasswordIsStoredAndReadBack(t *testing.T) {
	h := newHarness(t)

	created, err := h.svc.Create(context.Background(), h.ws, &proxy.CreateUpdateDto{Protocol: "socks5", Host: "10.0.0.9", Port: 1080, Auth: true, Username: "user", Password: "secret"})
	require.NoError(t, err)

	found, err := h.svc.FindByID(context.Background(), h.ws, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "secret", found.Password)
	assert.Equal(t, "user", found.Username)
	assert.True(t, found.Auth)
}

func TestProxyService_ListShowsOnlyTheWorkspacesProxies(t *testing.T) {
	h := newHarness(t)
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	mine := h.create(t, h.ws, "mine.example.com")
	theirs := h.create(t, workspacetest.Other, "theirs.example.com")

	inMine, err := h.svc.FindAll(context.Background(), h.ws, 0, 10, "")
	require.NoError(t, err)
	inTheirs, err := h.svc.FindAll(context.Background(), workspacetest.Other, 0, 10, "")
	require.NoError(t, err)
	searched, err := h.svc.FindAll(context.Background(), h.ws, 0, 10, "theirs")
	require.NoError(t, err)

	require.Len(t, inMine, 1)
	assert.Equal(t, mine.ID, inMine[0].ID)
	require.Len(t, inTheirs, 1)
	assert.Equal(t, theirs.ID, inTheirs[0].ID)
	assert.Empty(t, searched, "a search doesn't find another workspace's proxy")
}

func TestProxyService_UpdatesChangeTheProxyAndKeepItsWorkspace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	created := h.create(t, h.ws, "old.example.com")

	full, err := h.svc.UpdateFull(ctx, h.ws, created.ID, &proxy.CreateUpdateDto{Protocol: "https", Host: "new.example.com", Port: 443})
	require.NoError(t, err)
	assert.Equal(t, "new.example.com", full.Host)
	partial, err := h.svc.UpdatePartial(ctx, h.ws, created.ID, &proxy.PartialUpdateDto{Port: ptr(8443)})
	require.NoError(t, err)

	assert.Equal(t, 8443, partial.Port)
	assert.Equal(t, "new.example.com", partial.Host)
	assert.Equal(t, h.ws, partial.WorkspaceID)
}

func TestProxyService_AProxyOfAnotherWorkspaceIsNotFoundAndLeftAlone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	mine := h.create(t, h.ws, "mine.example.com")

	found, err := h.svc.FindByID(ctx, workspacetest.Other, mine.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
	_, err = h.svc.UpdateFull(ctx, workspacetest.Other, mine.ID, &proxy.CreateUpdateDto{Protocol: "http", Host: "hijacked.example.com", Port: 1})
	assert.ErrorIs(t, err, proxy.ErrNotFound)
	_, err = h.svc.UpdatePartial(ctx, workspacetest.Other, mine.ID, &proxy.PartialUpdateDto{Host: ptr("hijacked.example.com")})
	assert.ErrorIs(t, err, proxy.ErrNotFound)
	_, err = h.svc.UpdatePartial(ctx, workspacetest.Other, mine.ID, &proxy.PartialUpdateDto{})
	assert.ErrorIs(t, err, proxy.ErrNotFound, "even an update that changes nothing")
	assert.ErrorIs(t, h.svc.Delete(ctx, workspacetest.Other, mine.ID), proxy.ErrNotFound)

	assert.Empty(t, h.monitors.removals, "no monitor is told about a proxy that wasn't deleted")
	kept, err := h.svc.FindByID(ctx, h.ws, mine.ID)
	require.NoError(t, err)
	require.NotNil(t, kept)
	assert.Equal(t, "mine.example.com", kept.Host)
}

func TestProxyService_DeleteClearsTheProxyFromTheWorkspacesMonitorsAndDeletesIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	created := h.create(t, h.ws, "proxy.example.com")
	h.monitors.onRemove = func() {
		found, err := h.svc.FindByID(ctx, h.ws, created.ID)
		require.NoError(t, err)
		assert.NotNil(t, found, "the monitors are cleared while the proxy still exists, so the database clears nothing silently")
	}

	require.NoError(t, h.svc.Delete(ctx, h.ws, created.ID))

	assert.Equal(t, []removal{{h.ws, created.ID}}, h.monitors.removals)
	found, err := h.svc.FindByID(ctx, h.ws, created.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestProxyService_AFailureToClearTheMonitorsKeepsTheProxy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	created := h.create(t, h.ws, "proxy.example.com")
	h.monitors.err = errors.New("clearing failed")

	err := h.svc.Delete(ctx, h.ws, created.ID)

	assert.ErrorContains(t, err, "clearing failed")
	found, findErr := h.svc.FindByID(ctx, h.ws, created.ID)
	require.NoError(t, findErr)
	assert.NotNil(t, found, "deleting it would clear the monitors with no event")
}

func TestProxyService_DeletingAMissingProxyIsNotFound(t *testing.T) {
	h := newHarness(t)

	err := h.svc.Delete(context.Background(), h.ws, uuid.NewString())

	assert.ErrorIs(t, err, proxy.ErrNotFound)
	assert.Empty(t, h.monitors.removals)
}
