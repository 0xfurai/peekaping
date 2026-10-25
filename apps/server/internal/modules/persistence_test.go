package modules_test

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/status_page"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
)

func TestRepositoryInterfacesUseDomainTypes(t *testing.T) {
	require.NoError(t, filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".repository.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			iface, ok := node.(*ast.InterfaceType)
			if !ok {
				return true
			}
			ast.Inspect(iface, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok {
					assert.False(t, strings.HasSuffix(strings.ToLower(ident.Name), "dto"), "%s references HTTP type %s", path, ident.Name)
				}
				return true
			})
			return false
		})
		return nil
	}))
}

type repositories struct {
	maintenance   maintenance.Repository
	monitors      monitor_status_page.Repository
	domains       domain_status_page.Repository
	pages         status_page.Repository
	settings      setting.Repository
	tls           monitor_tls_info.Repository
	history       notification_sent_history.Repository
	statusPageID  string
	monitorID     string
	replacementID string
}

func sqlRepositories(t *testing.T, db *bun.DB) repositories {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
	statusID, monitorID, replacementID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = db.Exec("INSERT INTO status_pages (id, slug, title) VALUES (?, ?, ?)", statusID, "domain-models", "Domain models")
	require.NoError(t, err)
	for _, id := range []string{monitorID, replacementID} {
		_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
		require.NoError(t, err)
	}
	return repositories{
		maintenance.NewSQLRepository(db), monitor_status_page.NewSQLRepository(db), domain_status_page.NewSQLRepository(db), status_page.NewSQLRepository(db),
		setting.NewSQLRepository(db), monitor_tls_info.NewSQLRepository(db), notification_sent_history.NewSQLRepository(db),
		statusID, monitorID, replacementID,
	}
}

func TestRepositoriesUseDomainModels(t *testing.T) {
	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "domain-models.db"))
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1) // as infra.NewSQL does: one connection, so writes queue instead of failing as locked
		exerciseRepositories(t, sqlRepositories(t, bun.NewDB(sqlDB, sqlitedialect.New())))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pee90_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]interface{}{"search_path": schema}))
		exerciseRepositories(t, sqlRepositories(t, bun.NewDB(sql.OpenDB(connector), pgdialect.New())))
	})
}

func exerciseRepositories(t *testing.T, repos repositories) {
	t.Helper()
	ctx := context.Background()
	t.Run("Maintenance", func(t *testing.T) {
		timezone, cron := "UTC", "0 9 * * 1,3"
		startDate, endDate, interval, duration := "2026-01-01T00:00", "2026-12-31T00:00", 2, 90
		input := &maintenance.Model{Title: "Maintenance", Description: "Description", Active: true, Strategy: "recurring-weekday", StartDateTime: &startDate, EndDateTime: &endDate, Weekdays: []int{1, 3}, DaysOfMonth: []int{5, 10}, IntervalDay: &interval, Cron: &cron, Timezone: &timezone, Duration: &duration}
		created, err := repos.maintenance.Create(ctx, input)
		require.NoError(t, err)
		actual, err := repos.maintenance.FindByID(ctx, created.ID)
		require.NoError(t, err)
		expected := *input
		expected.ID, expected.CreatedAt, expected.UpdatedAt = actual.ID, actual.CreatedAt, actual.UpdatedAt
		assert.Equal(t, &expected, actual)
		title, active, zero := "", false, 0
		updated, err := repos.maintenance.UpdatePartial(ctx, created.ID, &maintenance.UpdateModel{Title: &title, Active: &active, IntervalDay: &zero, Weekdays: []int{2, 4}})
		require.NoError(t, err)
		assert.Empty(t, updated.Title)
		assert.False(t, updated.Active)
		assert.Equal(t, &zero, updated.IntervalDay)
		assert.Equal(t, []int{2, 4}, updated.Weekdays)
		assert.Equal(t, input.Description, updated.Description)
		input.Title = "Replaced"
		_, err = repos.maintenance.UpdateFull(ctx, created.ID, input)
		require.NoError(t, err)
		actual, err = repos.maintenance.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, input.Title, actual.Title)
	})
	t.Run("StatusPageShowLocations", func(t *testing.T) {
		plain, err := repos.pages.Create(ctx, &status_page.Model{Slug: "plain-page", Title: "Plain"})
		require.NoError(t, err)
		assert.False(t, plain.ShowLocations, "a page made without the setting")
		eachPlace, err := repos.pages.Create(ctx, &status_page.Model{Slug: "each-place", Title: "Each place", ShowLocations: true})
		require.NoError(t, err)
		found, err := repos.pages.FindByID(ctx, eachPlace.ID)
		require.NoError(t, err)
		assert.True(t, found.ShowLocations, "stored on create")
		title, off, on := "Renamed", false, true
		require.NoError(t, repos.pages.Update(ctx, eachPlace.ID, &status_page.UpdateModel{Title: &title}))
		found, err = repos.pages.FindBySlug(ctx, "each-place")
		require.NoError(t, err)
		assert.True(t, found.ShowLocations, "an update that leaves it out keeps it")
		require.NoError(t, repos.pages.Update(ctx, eachPlace.ID, &status_page.UpdateModel{ShowLocations: &off}))
		found, err = repos.pages.FindByID(ctx, eachPlace.ID)
		require.NoError(t, err)
		assert.False(t, found.ShowLocations, "turned off")
		require.NoError(t, repos.pages.Update(ctx, plain.ID, &status_page.UpdateModel{ShowLocations: &on}))
		found, err = repos.pages.FindByID(ctx, plain.ID)
		require.NoError(t, err)
		assert.True(t, found.ShowLocations, "turned on")
	})
	t.Run("StatusPageMonitors", func(t *testing.T) {
		created, err := repos.monitors.Create(ctx, &monitor_status_page.Model{StatusPageID: repos.statusPageID, MonitorID: repos.monitorID, Active: true, Order: 3})
		require.NoError(t, err)
		updated, err := repos.monitors.UpdatePartial(ctx, created.ID, &monitor_status_page.UpdateModel{MonitorID: &repos.replacementID})
		require.NoError(t, err)
		assert.Equal(t, repos.replacementID, updated.MonitorID)
		assert.Equal(t, repos.statusPageID, updated.StatusPageID)
		_, err = repos.monitors.UpdateFull(ctx, created.ID, &monitor_status_page.Model{StatusPageID: repos.statusPageID, MonitorID: repos.monitorID})
		require.NoError(t, err)
		actual, err := repos.monitors.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, repos.monitorID, actual.MonitorID)
	})
	t.Run("StatusPageDomains", func(t *testing.T) {
		created, err := repos.domains.Create(ctx, &domain_status_page.Model{StatusPageID: repos.statusPageID, Domain: "before.example"})
		require.NoError(t, err)
		domain := "after.example"
		updated, err := repos.domains.UpdatePartial(ctx, created.ID, &domain_status_page.UpdateModel{Domain: &domain})
		require.NoError(t, err)
		assert.Equal(t, domain, updated.Domain)
		assert.Equal(t, repos.statusPageID, updated.StatusPageID)
		_, err = repos.domains.UpdateFull(ctx, created.ID, &domain_status_page.Model{StatusPageID: repos.statusPageID, Domain: "full.example"})
		require.NoError(t, err)
		actual, err := repos.domains.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "full.example", actual.Domain)
	})
	t.Run("Settings", func(t *testing.T) {
		_, err := repos.settings.SetByKey(ctx, "domain-model", &setting.Model{Value: "before", Type: "string"})
		require.NoError(t, err)
		_, err = repos.settings.SetByKey(ctx, "domain-model", &setting.Model{Value: "", Type: "string"})
		require.NoError(t, err)
		actual, err := repos.settings.GetByKey(ctx, "domain-model")
		require.NoError(t, err)
		assert.Empty(t, actual.Value)
		assert.Equal(t, "string", actual.Type)
	})
	// The usage ping's instance ID is created by whichever api replica gets there first. All of
	// them must end up with the one value that was stored, whoever raced.
	t.Run("SettingsCreateIfAbsent", func(t *testing.T) {
		const racers = 12
		returned := make([]string, racers)
		var wg sync.WaitGroup
		for n := range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				created, err := repos.settings.CreateIfAbsent(ctx, "create-if-absent", &setting.Model{Value: uuid.NewString(), Type: "string"})
				if assert.NoError(t, err) {
					returned[n] = created.Value
				}
			}()
		}
		wg.Wait()

		stored, err := repos.settings.GetByKey(ctx, "create-if-absent")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.NotEmpty(t, stored.Value)
		assert.Equal(t, "string", stored.Type)
		for n, value := range returned {
			assert.Equal(t, stored.Value, value, "racer %d got another value than the stored one", n)
		}

		again, err := repos.settings.CreateIfAbsent(ctx, "create-if-absent", &setting.Model{Value: "later", Type: "int"})
		require.NoError(t, err)
		assert.Equal(t, stored.Value, again.Value, "a stored value is not replaced")
		assert.Equal(t, "string", again.Type)

		_, err = repos.settings.SetByKey(ctx, "set-then-create", &setting.Model{Value: "chosen", Type: "string"})
		require.NoError(t, err)
		kept, err := repos.settings.CreateIfAbsent(ctx, "set-then-create", &setting.Model{Value: "default", Type: "string"})
		require.NoError(t, err)
		assert.Equal(t, "chosen", kept.Value)

		require.NoError(t, repos.settings.DeleteByKey(ctx, "create-if-absent"))
		fresh, err := repos.settings.CreateIfAbsent(ctx, "create-if-absent", &setting.Model{Value: "after delete", Type: "string"})
		require.NoError(t, err)
		assert.Equal(t, "after delete", fresh.Value, "a deleted key is created again")
	})
	t.Run("TLS", func(t *testing.T) {
		_, err := repos.tls.Create(ctx, &monitor_tls_info.Model{MonitorID: repos.monitorID, InfoJSON: `{"valid":true}`})
		require.NoError(t, err)
		_, err = repos.tls.Update(ctx, repos.monitorID, &monitor_tls_info.Model{InfoJSON: `{"valid":false}`})
		require.NoError(t, err)
		actual, err := repos.tls.GetByMonitorID(ctx, repos.monitorID)
		require.NoError(t, err)
		assert.Equal(t, `{"valid":false}`, actual.InfoJSON)
		assert.Equal(t, repos.monitorID, actual.MonitorID)
	})
	t.Run("NotificationHistory", func(t *testing.T) {
		input := &notification_sent_history.Model{Type: "certificate", MonitorID: repos.monitorID, Days: 7}
		require.NoError(t, repos.history.RecordSent(ctx, input))
		require.NoError(t, repos.history.RecordSent(ctx, input))
		actual, err := repos.history.GetByMonitorAndType(ctx, repos.monitorID, "certificate")
		require.NoError(t, err)
		require.Len(t, actual, 1)
		assert.Equal(t, 7, actual[0].Days)
		assert.Equal(t, input.MonitorID, actual[0].MonitorID)
	})
	// A threshold counts as sent once it or a nearer threshold has been sent,
	// so a certificate counting down still gets its 14- and 7-day certificate
	// expiry notifications.
	t.Run("NotificationHistoryThresholds", func(t *testing.T) {
		sent := func(days int) bool {
			t.Helper()
			ok, err := repos.history.CheckIfSent(ctx, "certificate", repos.replacementID, days)
			require.NoError(t, err)
			return ok
		}
		require.NoError(t, repos.history.RecordSent(ctx, &notification_sent_history.Model{Type: "certificate", MonitorID: repos.replacementID, Days: 21}))
		assert.True(t, sent(21), "21 was sent")
		assert.False(t, sent(14), "14 is still due after 21")
		assert.False(t, sent(7), "7 is still due after 21")

		require.NoError(t, repos.history.RecordSent(ctx, &notification_sent_history.Model{Type: "certificate", MonitorID: repos.replacementID, Days: 7}))
		assert.True(t, sent(14), "7 covers the farther 14")
		assert.True(t, sent(7))
	})
}
