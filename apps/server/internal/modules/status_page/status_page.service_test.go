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
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

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
	ws       workspace.ID
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
	return &harness{db, workspace.ID(workspacetest.OldestOrAdd(t, db)), status_page.NewService(status_page.NewSQLRepository(db), nil, monitors, domains, logger), monitors, domains}
}

// monitor stores a monitor in the harness's workspace and returns its ID
func (h *harness) monitor(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.InsertMonitor(t, h.db, id)
	return id
}

func (h *harness) create(t *testing.T, dto status_page.CreateStatusPageDTO) *status_page.Model {
	t.Helper()
	if dto.Title == "" {
		dto.Title = "Status of " + dto.Slug
	}
	created, err := h.svc.Create(context.Background(), h.ws, &dto)
	require.NoError(t, err)
	return created
}

func (h *harness) withMonitors(t *testing.T, id string) *status_page.StatusPageWithMonitorsResponseDTO {
	t.Helper()
	page, err := h.svc.FindByIDWithMonitors(context.Background(), h.ws, id)
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

	_, err := h.svc.Create(ctx, h.ws, &status_page.CreateStatusPageDTO{Slug: "copy", Title: "Copy", Domains: []string{"status.example.com"}})
	var used *status_page.DomainAlreadyUsedError
	require.ErrorAs(t, err, &used)
	assert.Equal(t, "DOMAIN_EXISTS", used.Code)
	assert.Equal(t, "status.example.com", used.Domain)
	all, err := h.svc.FindAll(ctx, h.ws, 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 2, "the rejected page isn't created")

	// Domains only: this holds whether or not Update saves other fields before checking domains.
	_, err = h.svc.Update(ctx, h.ws, internal.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Empty(t, h.withMonitors(t, internal.ID).Domains)

	_, err = h.svc.Update(ctx, h.ws, public.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com", "uptime.example.com"}})
	require.NoError(t, err, "a page can keep its own domain")
	assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, h.withMonitors(t, public.ID).Domains)
}

func TestStatusPageService_HidesUnpublishedPagesFromPublicLookups(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	public := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Published: true, Domains: []string{"status.example.com"}})
	h.create(t, status_page.CreateStatusPageDTO{Slug: "draft", Published: false, Domains: []string{"draft.example.com"}})

	found, err := h.svc.FindPublishedBySlugAnyWorkspace(ctx, "public")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, public.ID, found.ID)
	found, err = h.svc.FindPublishedByDomainAnyWorkspace(ctx, "status.example.com")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, public.ID, found.ID)

	for name, lookup := range map[string]func() (*status_page.Model, error){
		"unpublished slug": func() (*status_page.Model, error) { return h.svc.FindPublishedBySlugAnyWorkspace(ctx, "draft") },
		"unpublished domain": func() (*status_page.Model, error) {
			return h.svc.FindPublishedByDomainAnyWorkspace(ctx, "draft.example.com")
		},
		"unknown slug": func() (*status_page.Model, error) { return h.svc.FindPublishedBySlugAnyWorkspace(ctx, "missing") },
		"unknown domain": func() (*status_page.Model, error) {
			return h.svc.FindPublishedByDomainAnyWorkspace(ctx, "missing.example.com")
		},
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
	web, db, api := h.monitor(t), h.monitor(t), h.monitor(t)
	page := h.create(t, status_page.CreateStatusPageDTO{
		Slug:       "public",
		MonitorIDs: []string{web, db},
		Domains:    []string{"status.example.com", "uptime.example.com"},
	})
	created := h.withMonitors(t, page.ID)
	assert.ElementsMatch(t, []string{web, db}, created.MonitorIDs)
	assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, created.Domains)

	updated, err := h.svc.Update(ctx, h.ws, page.ID, &status_page.UpdateStatusPageDTO{
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

	_, err = h.svc.Update(ctx, h.ws, page.ID, &status_page.UpdateStatusPageDTO{Description: ptr("All systems")})
	require.NoError(t, err)
	untouched := h.withMonitors(t, page.ID)
	assert.ElementsMatch(t, []string{db, api}, untouched.MonitorIDs, "an update without monitor_ids keeps the monitors")
	assert.ElementsMatch(t, []string{"uptime.example.com", "health.example.com"}, untouched.Domains, "an update without domains keeps the domains")
}

func TestStatusPageService_DeleteRemovesThePageAndItsMonitorLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	web, db := h.monitor(t), h.monitor(t)
	page := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", MonitorIDs: []string{web, db}})
	other := h.create(t, status_page.CreateStatusPageDTO{Slug: "other", MonitorIDs: []string{web}})

	require.NoError(t, h.svc.Delete(ctx, h.ws, page.ID))

	missing, err := h.svc.FindByIDWithMonitors(ctx, h.ws, page.ID)
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
	onPublic, onDraft, onBoth, nowhere := h.monitor(t), h.monitor(t), h.monitor(t), uuid.NewString()
	public := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Published: true, MonitorIDs: []string{onPublic, onBoth}})
	h.create(t, status_page.CreateStatusPageDTO{Slug: "draft", Published: false, MonitorIDs: []string{onDraft, onBoth}})

	for monitorID, want := range map[string]bool{onPublic: true, onDraft: false, onBoth: true, nowhere: false} {
		published, err := h.svc.IsMonitorPublishedAnyWorkspace(ctx, monitorID)
		require.NoError(t, err)
		assert.Equal(t, want, published, monitorID)
	}

	_, err := h.svc.Update(ctx, h.ws, public.ID, &status_page.UpdateStatusPageDTO{Published: ptr(false)})
	require.NoError(t, err)
	published, err := h.svc.IsMonitorPublishedAnyWorkspace(ctx, onPublic)
	require.NoError(t, err)
	assert.False(t, published, "unpublishing the page unpublishes its monitors")
}

func TestStatusPageService_StoresDomainsLowerCaseAndFindsThemByAnyCase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	page := h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Published: true, Domains: []string{" Status.Example.COM ", "status.example.com"}})

	assert.Equal(t, []string{"status.example.com"}, h.withMonitors(t, page.ID).Domains, "stored lower-case, and once")
	for _, spelling := range []string{"STATUS.example.com", " status.example.com "} {
		found, err := h.svc.FindPublishedByDomainAnyWorkspace(ctx, spelling)
		require.NoError(t, err)
		require.NotNil(t, found, spelling)
		assert.Equal(t, page.ID, found.ID)
	}

	// Sending the page's own domain in another case keeps it
	_, err := h.svc.Update(ctx, h.ws, page.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"STATUS.Example.com", "Uptime.Example.com"}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, h.withMonitors(t, page.ID).Domains)
}

func TestStatusPageService_RejectsADomainAnotherPageOwnsInAnyCase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Domains: []string{"status.example.com"}})
	internal := h.create(t, status_page.CreateStatusPageDTO{Slug: "internal"})
	var used *status_page.DomainAlreadyUsedError

	_, err := h.svc.Create(ctx, h.ws, &status_page.CreateStatusPageDTO{Slug: "copy", Title: "Copy", Domains: []string{" STATUS.Example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Equal(t, "status.example.com", used.Domain)

	_, err = h.svc.Update(ctx, h.ws, internal.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"Status.Example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Empty(t, h.withMonitors(t, internal.ID).Domains)
}

// racingDomains is a domain service whose check finds nothing, as when another page takes the
// domain between the check and the write
type racingDomains struct{ domain_status_page.Service }

func (racingDomains) FindByDomain(context.Context, string) (*domain_status_page.Model, error) {
	return nil, nil
}

// Two pages racing for one domain: the database lets one in, and the other is told the domain is
// taken, as when the check found it
func TestStatusPageService_APageThatLosesARaceForADomainIsToldItIsTaken(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.create(t, status_page.CreateStatusPageDTO{Slug: "public", Domains: []string{"status.example.com"}})
	logger := zap.NewNop().Sugar()
	racing := status_page.NewService(status_page.NewSQLRepository(h.db), nil, h.monitors, racingDomains{h.domains}, logger)
	var used *status_page.DomainAlreadyUsedError

	_, err := racing.Create(ctx, h.ws, &status_page.CreateStatusPageDTO{Slug: "copy", Title: "Copy", Domains: []string{"Status.Example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Equal(t, "DOMAIN_EXISTS", used.Code)
	assert.Equal(t, "status.example.com", used.Domain)

	internal := h.create(t, status_page.CreateStatusPageDTO{Slug: "internal"})
	_, err = racing.Update(ctx, h.ws, internal.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com"}})
	require.ErrorAs(t, err, &used)
}

// Custom domains are unique across the install, so a domain a page of another workspace holds is
// taken, and the service says nothing else about that page
func TestStatusPageService_ADomainAPageOfAnotherWorkspaceHoldsIsTaken(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	theirs, err := h.svc.Create(ctx, workspacetest.Other, &status_page.CreateStatusPageDTO{Slug: "theirs", Title: "Theirs", Domains: []string{"status.example.com"}})
	require.NoError(t, err)
	mine := h.create(t, status_page.CreateStatusPageDTO{Slug: "mine"})
	var used *status_page.DomainAlreadyUsedError

	_, err = h.svc.Create(ctx, h.ws, &status_page.CreateStatusPageDTO{Slug: "copy", Title: "Copy", Domains: []string{"Status.Example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Equal(t, "DOMAIN_EXISTS", used.Code)
	assert.Equal(t, "status.example.com", used.Domain)
	assert.NotContains(t, err.Error(), theirs.ID)

	_, err = h.svc.Update(ctx, h.ws, mine.ID, &status_page.UpdateStatusPageDTO{Domains: &[]string{"status.example.com"}})
	require.ErrorAs(t, err, &used)
	assert.Empty(t, h.withMonitors(t, mine.ID).Domains)
	all, err := h.svc.FindAll(ctx, h.ws, 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 1, "the refused page isn't created, and the other workspace's isn't listed")
}

// A page's monitors must be in its workspace, and are checked before anything is written
func TestStatusPageService_RefusesAMonitorOfAnotherWorkspaceAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mine := h.monitor(t)
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	theirs := uuid.NewString()
	_, err := h.db.Exec("INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Theirs', 60, 10, 1, 10, 0)", theirs, string(workspacetest.Other))
	require.NoError(t, err)
	page := h.create(t, status_page.CreateStatusPageDTO{Slug: "page", Title: "Page", MonitorIDs: []string{mine}})

	for name, ids := range map[string][]string{"another workspace's": {mine, theirs}, "unknown": {mine, uuid.NewString()}} {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.Create(ctx, h.ws, &status_page.CreateStatusPageDTO{Slug: "doomed", Title: "Doomed", MonitorIDs: ids})
			assert.ErrorIs(t, err, monitor_status_page.ErrUnknownMonitor)
			found, err := h.svc.FindPublishedBySlugAnyWorkspace(ctx, "doomed")
			require.NoError(t, err)
			assert.Nil(t, found, "no page is stored")

			_, err = h.svc.Update(ctx, h.ws, page.ID, &status_page.UpdateStatusPageDTO{Title: ptr("Renamed"), MonitorIDs: &ids})
			assert.ErrorIs(t, err, monitor_status_page.ErrUnknownMonitor)
			saved := h.withMonitors(t, page.ID)
			assert.Equal(t, "Page", saved.Title, "the page keeps its title")
			assert.Equal(t, []string{mine}, saved.MonitorIDs, "and its monitors")
		})
	}
}

func TestStatusPageService_APageOfAnotherWorkspaceIsNotFound(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	workspacetest.Add(t, h.db, workspacetest.Other, "Other")
	theirs, err := h.svc.Create(ctx, workspacetest.Other, &status_page.CreateStatusPageDTO{Slug: "theirs", Title: "Theirs"})
	require.NoError(t, err)

	found, err := h.svc.FindByID(ctx, h.ws, theirs.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
	_, err = h.svc.Update(ctx, h.ws, theirs.ID, &status_page.UpdateStatusPageDTO{Title: ptr("Hijacked")})
	assert.ErrorIs(t, err, status_page.ErrNotFound)
	assert.ErrorIs(t, h.svc.Delete(ctx, h.ws, theirs.ID), status_page.ErrNotFound)
	found, err = h.svc.FindByID(ctx, workspacetest.Other, theirs.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Theirs", found.Title)
}
