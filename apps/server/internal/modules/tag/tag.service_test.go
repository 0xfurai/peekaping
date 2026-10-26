package tag_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/tag"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

type harness struct {
	db          *bun.DB
	svc         tag.Service
	monitorTags monitor_tag.Service
}

// newHarness builds the tag service over a migrated SQLite database. Foreign
// keys stay off, so links go away only if the service removes them.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "tag.db"))
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)

	logger := zap.NewNop().Sugar()
	monitorTags := monitor_tag.NewService(monitor_tag.NewSQLRepository(db), logger)
	return &harness{db, tag.NewService(tag.NewSQLRepository(db), monitorTags, logger), monitorTags}
}

func (h *harness) create(t *testing.T, name string) *tag.Model {
	t.Helper()
	created, err := h.svc.Create(context.Background(), &tag.CreateUpdateDto{Name: name, Color: "#3B82F6"})
	require.NoError(t, err)
	return created
}

func (h *harness) monitor(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	_, err := h.db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
	require.NoError(t, err)
	return id
}

func (h *harness) find(t *testing.T, id string) *tag.Model {
	t.Helper()
	found, err := h.svc.FindByID(context.Background(), id)
	require.NoError(t, err)
	return found
}

func ptr[T any](v T) *T { return &v }

func TestTagService_CreateRejectsADuplicateName(t *testing.T) {
	h := newHarness(t)
	h.create(t, "Production")

	_, err := h.svc.Create(context.Background(), &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444"})
	assert.EqualError(t, err, "tag with this name already exists")

	all, err := h.svc.FindAll(context.Background(), 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestTagService_UpdateFullRejectsAnotherTagsName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	production := h.create(t, "Production")
	staging := h.create(t, "Staging")

	_, err := h.svc.UpdateFull(ctx, staging.ID, &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444"})
	assert.EqualError(t, err, "tag with this name already exists")
	assert.Equal(t, "Staging", h.find(t, staging.ID).Name)

	_, err = h.svc.UpdateFull(ctx, production.ID, &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444", Description: ptr("Live")})
	require.NoError(t, err, "a tag can keep its own name")
	saved := h.find(t, production.ID)
	assert.Equal(t, "#EF4444", saved.Color)
	assert.Equal(t, ptr("Live"), saved.Description)
}

func TestTagService_UpdatePartialRejectsAnotherTagsName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	production := h.create(t, "Production")
	staging := h.create(t, "Staging")

	_, err := h.svc.UpdatePartial(ctx, staging.ID, &tag.PartialUpdateDto{Name: ptr("Production")})
	assert.EqualError(t, err, "tag with this name already exists")
	assert.Equal(t, "Staging", h.find(t, staging.ID).Name)

	updated, err := h.svc.UpdatePartial(ctx, production.ID, &tag.PartialUpdateDto{Name: ptr("Production"), Color: ptr("#10B981")})
	require.NoError(t, err, "a tag can keep its own name")
	assert.Equal(t, "#10B981", updated.Color)

	updated, err = h.svc.UpdatePartial(ctx, staging.ID, &tag.PartialUpdateDto{Name: ptr("QA")})
	require.NoError(t, err)
	assert.Equal(t, "QA", updated.Name)
}

func TestTagService_DeleteRemovesTheTagsMonitorLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	production := h.create(t, "Production")
	staging := h.create(t, "Staging")
	web, db := h.monitor(t), h.monitor(t)
	for _, pair := range [][2]string{{web, production.ID}, {db, production.ID}, {web, staging.ID}} {
		_, err := h.monitorTags.Create(ctx, pair[0], pair[1])
		require.NoError(t, err)
	}

	require.NoError(t, h.svc.Delete(ctx, production.ID))

	assert.Nil(t, h.find(t, production.ID))
	links, err := h.monitorTags.FindByTagID(ctx, production.ID)
	require.NoError(t, err)
	assert.Empty(t, links)
	links, err = h.monitorTags.FindByTagID(ctx, staging.ID)
	require.NoError(t, err)
	assert.Len(t, links, 1, "other tags keep their links")
}
