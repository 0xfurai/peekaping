package tag_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"peekaping/internal/modules/workspace/workspacetest"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"

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
	db *bun.DB
	// ws is the workspace the tests' requests run in: the install's oldest one
	ws          workspace.ID
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
	return &harness{db, workspace.ID(workspacetest.OldestOrAdd(t, db)), tag.NewService(tag.NewSQLRepository(db), monitorTags, logger), monitorTags}
}

func (h *harness) create(t *testing.T, name string) *tag.Model {
	t.Helper()
	created, err := h.svc.Create(context.Background(), h.ws, &tag.CreateUpdateDto{Name: name, Color: "#3B82F6"})
	require.NoError(t, err)
	return created
}

func (h *harness) monitor(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.OldestOrAdd(t, h.db)
	_, err := h.db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
	require.NoError(t, err)
	return id
}

func (h *harness) find(t *testing.T, id string) *tag.Model {
	t.Helper()
	found, err := h.svc.FindByID(context.Background(), h.ws, id)
	require.NoError(t, err)
	return found
}

func ptr[T any](v T) *T { return &v }

func TestTagService_CreateRejectsADuplicateName(t *testing.T) {
	h := newHarness(t)
	h.create(t, "Production")

	_, err := h.svc.Create(context.Background(), h.ws, &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444"})
	assert.EqualError(t, err, "tag with this name already exists")

	all, err := h.svc.FindAll(context.Background(), h.ws, 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestTagService_UpdateFullRejectsAnotherTagsName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	production := h.create(t, "Production")
	staging := h.create(t, "Staging")

	_, err := h.svc.UpdateFull(ctx, h.ws, staging.ID, &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444"})
	assert.EqualError(t, err, "tag with this name already exists")
	assert.Equal(t, "Staging", h.find(t, staging.ID).Name)

	_, err = h.svc.UpdateFull(ctx, h.ws, production.ID, &tag.CreateUpdateDto{Name: "Production", Color: "#EF4444", Description: ptr("Live")})
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

	_, err := h.svc.UpdatePartial(ctx, h.ws, staging.ID, &tag.PartialUpdateDto{Name: ptr("Production")})
	assert.EqualError(t, err, "tag with this name already exists")
	assert.Equal(t, "Staging", h.find(t, staging.ID).Name)

	updated, err := h.svc.UpdatePartial(ctx, h.ws, production.ID, &tag.PartialUpdateDto{Name: ptr("Production"), Color: ptr("#10B981")})
	require.NoError(t, err, "a tag can keep its own name")
	assert.Equal(t, "#10B981", updated.Color)

	updated, err = h.svc.UpdatePartial(ctx, h.ws, staging.ID, &tag.PartialUpdateDto{Name: ptr("QA")})
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

	require.NoError(t, h.svc.Delete(ctx, h.ws, production.ID))

	assert.Nil(t, h.find(t, production.ID))
	links, err := h.monitorTags.FindByTagID(ctx, production.ID)
	require.NoError(t, err)
	assert.Empty(t, links)
	links, err = h.monitorTags.FindByTagID(ctx, staging.ID)
	require.NoError(t, err)
	assert.Len(t, links, 1, "other tags keep their links")
}

// Tag names are unique within a workspace and not across the install (docs/adr/0005 section 6)
func TestTagService_TwoWorkspacesCanEachHaveATagWithTheSameName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	mine := h.create(t, "production")

	theirs, err := h.svc.Create(ctx, workspacetest.Other, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})
	require.NoError(t, err)

	assert.NotEqual(t, mine.ID, theirs.ID)
	assert.Equal(t, workspacetest.Other, theirs.WorkspaceID)
	_, err = h.svc.Create(ctx, workspacetest.Other, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})
	assert.ErrorIs(t, err, tag.ErrNameTaken, "a second one in the same workspace is refused")
	_, err = h.svc.Create(ctx, h.ws, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})
	assert.ErrorIs(t, err, tag.ErrNameTaken)

	// and a rename into a name that only the other workspace has is fine
	renamed, err := h.svc.UpdatePartial(ctx, h.ws, mine.ID, &tag.PartialUpdateDto{Name: ptr("staging")})
	require.NoError(t, err)
	assert.Equal(t, "staging", renamed.Name)
	_, err = h.svc.UpdateFull(ctx, h.ws, mine.ID, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})
	require.NoError(t, err, "the name is free in this workspace, though the other one has it")
}

func TestTagService_ATagOfAnotherWorkspaceIsNotFoundAndLeftAlone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	mine := h.create(t, "production")
	monitorID := h.monitor(t)
	_, err := h.monitorTags.Create(ctx, monitorID, mine.ID)
	require.NoError(t, err)

	found, err := h.svc.FindByID(ctx, workspacetest.Other, mine.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
	byName, err := h.svc.FindByName(ctx, workspacetest.Other, "production")
	require.NoError(t, err)
	assert.Nil(t, byName)
	all, err := h.svc.FindAll(ctx, workspacetest.Other, 0, 10, "")
	require.NoError(t, err)
	assert.Empty(t, all)
	_, err = h.svc.UpdateFull(ctx, workspacetest.Other, mine.ID, &tag.CreateUpdateDto{Name: "hijacked", Color: "#000000"})
	assert.ErrorIs(t, err, tag.ErrNotFound)
	_, err = h.svc.UpdatePartial(ctx, workspacetest.Other, mine.ID, &tag.PartialUpdateDto{Name: ptr("hijacked")})
	assert.ErrorIs(t, err, tag.ErrNotFound)
	_, err = h.svc.UpdatePartial(ctx, workspacetest.Other, mine.ID, &tag.PartialUpdateDto{})
	assert.ErrorIs(t, err, tag.ErrNotFound, "even an update that changes nothing")
	assert.ErrorIs(t, h.svc.Delete(ctx, workspacetest.Other, mine.ID), tag.ErrNotFound)

	assert.Equal(t, "production", h.find(t, mine.ID).Name, "the tag is as it was")
	links, err := h.monitorTags.FindByTagID(ctx, mine.ID)
	require.NoError(t, err)
	assert.Len(t, links, 1, "with its links")
}
