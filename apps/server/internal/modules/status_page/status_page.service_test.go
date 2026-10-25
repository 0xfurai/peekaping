package status_page_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/status_page"

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
	svc      status_page.Service
	monitors monitor_status_page.Service
	domains  domain_status_page.Service
}

// newHarness builds the status page service over a migrated SQLite database.
// Foreign keys stay off, so links go away only if the service removes them.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "status_page.db"))
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)

	logger := zap.NewNop().Sugar()
	monitors := monitor_status_page.NewService(monitor_status_page.NewSQLRepository(db), logger)
	domains := domain_status_page.NewService(domain_status_page.NewSQLRepository(db), logger)
	return &harness{status_page.NewService(status_page.NewSQLRepository(db), nil, monitors, domains, logger), monitors, domains}
}

func (h *harness) create(t *testing.T, dto status_page.CreateStatusPageDTO) *status_page.Model {
	t.Helper()
	if dto.Title == "" {
		dto.Title = "Status of " + dto.Slug
	}
	created, err := h.svc.Create(context.Background(), &dto)
	require.NoError(t, err)
	return created
}

func (h *harness) withMonitors(t *testing.T, id string) *status_page.StatusPageWithMonitorsResponseDTO {
	t.Helper()
	page, err := h.svc.FindByIDWithMonitors(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, page)
	return page
}

func ptr[T any](v T) *T { return &v }

func TestStatusPageService_RejectsADomainAnotherPageOwns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	public := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Domains: []string{"status.example.com"}})
	internal := h.create(t, status_page.CreateStatusPageDTO{Slug: "internal"})

	_, err := h.svc.Create(ctx, &status_page.CreateStatusPageDTO{Slug: "copy", Title: "Copy", Domains: []string{"status.example.com"}})
	var used *status_page.DomainAlreadyUsedError
	require.ErrorAs(t, err, &used)
	assert.Equal(t, "DOMAIN_EXISTS", used.Code)
	assert.Equal(t, "status.example.com", used.Domain)
	all, err := h.svc.FindAll(ctx, 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 2, "the rejected page isn't created")

	// Domains only: this holds whether or not Update saves other fields before checking domains.
	_, err = h.svc.Update(ctx, internal.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Empty(t, h.withMonitors(t, internal.ID).Domains)

	_, err = h.svc.Update(ctx, public.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com", "uptime.example.com"}})
	require.NoError(t, err, "a page can keep its own domain")
	assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, h.withMonitors(t, public.ID).Domains)
}

func TestStatusPageService_HidesUnpublishedPagesFromPublicLookups(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	public := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Published: true, Domains: []string{"status.example.com"}})
	h.create(t, status_page.CreateStatusPageDTO{Slug: "draft", Published: false, Domains: []string{"draft.example.com"}})

	found, err := h.svc.FindPublishedBySlug(ctx, "public")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, public.ID, found.ID)
	found, err = h.svc.FindPublishedByDomain(ctx, "status.example.com")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, public.ID, found.ID)

	for name, lookup := range map[string]func() (*status_page.Model, error){
		"unpublished slug":   func() (*status_page.Model, error) { return h.svc.FindPublishedBySlug(ctx, "draft") },
		"unpublished domain": func() (*status_page.Model, error) { return h.svc.FindPublishedByDomain(ctx, "draft.example.com") },
		"unknown slug":       func() (*status_page.Model, error) { return h.svc.FindPublishedBySlug(ctx, "missing") },
		"unknown domain":     func() (*status_page.Model, error) { return h.svc.FindPublishedByDomain(ctx, "missing.example.com") },
	} {
		t.Run(name, func(t *testing.T) {
			page, err := lookup()
			require.NoError(t, err)
			assert.Nil(t, page)
		})
	}
}

func TestStatusPageService_UpdateReplacesMonitorsAndDomains(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	web, db, api := uuid.NewString(), uuid.NewString(), uuid.NewString()
	page := h.create(t, status_page.CreateStatusPageDTO{
		Slug:       "public",
		MonitorIDs: []string{web, db},
		Domains:    []string{"status.example.com", "uptime.example.com"},
	})
	created := h.withMonitors(t, page.ID)
	assert.ElementsMatch(t, []string{web, db}, created.MonitorIDs)
	assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, created.Domains)

	updated, err := h.svc.Update(ctx, page.ID, &status_page.UpdateStatusPageDTO{
		Title:      ptr("Public status"),
		MonitorIDs: &[]string{db, api},
		Domains:    &[]string{"uptime.example.com", "health.example.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Public status", updated.Title)
	saved := h.withMonitors(t, page.ID)
	assert.ElementsMatch(t, []string{db, api}, saved.MonitorIDs)
	assert.ElementsMatch(t, []string{"uptime.example.com", "health.example.com"}, saved.Domains)
	gone, err := h.domains.FindByDomain(ctx, "status.example.com")
	require.NoError(t, err)
	assert.Nil(t, gone, "a removed domain is free for other pages")

	_, err = h.svc.Update(ctx, page.ID, &status_page.UpdateStatusPageDTO{Description: ptr("All systems")})
	require.NoError(t, err)
	untouched := h.withMonitors(t, page.ID)
	assert.ElementsMatch(t, []string{db, api}, untouched.MonitorIDs, "an update without monitor_ids keeps the monitors")
	assert.ElementsMatch(t, []string{"uptime.example.com", "health.example.com"}, untouched.Domains, "an update without domains keeps the domains")
}

func TestStatusPageService_DeleteRemovesThePageAndItsMonitorLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	web, db := uuid.NewString(), uuid.NewString()
	page := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", MonitorIDs: []string{web, db}})
	other := h.create(t, status_page.CreateStatusPageDTO{Slug: "other", MonitorIDs: []string{web}})

	require.NoError(t, h.svc.Delete(ctx, page.ID))

	missing, err := h.svc.FindByIDWithMonitors(ctx, page.ID)
	require.NoError(t, err)
	assert.Nil(t, missing)
	links, err := h.monitors.GetMonitorsForStatusPage(ctx, page.ID)
	require.NoError(t, err)
	assert.Empty(t, links)
	assert.Equal(t, []string{web}, h.withMonitors(t, other.ID).MonitorIDs, "other pages keep their monitors")
}

func TestStatusPageService_IsMonitorPublishedOnlyThroughAPublishedPage(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	onPublic, onDraft, onBoth, nowhere := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	public := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Published: true, MonitorIDs: []string{onPublic, onBoth}})
	h.create(t, status_page.CreateStatusPageDTO{Slug: "draft", Published: false, MonitorIDs: []string{onDraft, onBoth}})

	for monitorID, want := range map[string]bool{onPublic: true, onDraft: false, onBoth: true, nowhere: false} {
		published, err := h.svc.IsMonitorPublished(ctx, monitorID)
		require.NoError(t, err)
		assert.Equal(t, want, published, monitorID)
	}

	_, err := h.svc.Update(ctx, public.ID, &status_page.UpdateStatusPageDTO{Published: ptr(false)})
	require.NoError(t, err)
	published, err := h.svc.IsMonitorPublished(ctx, onPublic)
	require.NoError(t, err)
	assert.False(t, published, "unpublishing the page unpublishes its monitors")
}
