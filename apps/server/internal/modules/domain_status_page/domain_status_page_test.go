package domain_status_page_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/domain_status_page"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "domain_status_page.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "domain_status_page_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return db
}

// fixture is the custom domain service plus a constructor for the status pages a domain points at.
type fixture struct {
	svc  domain_status_page.Service
	page func() string
}

// forEachBackend runs fn against the custom domain service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			fn(t, fixture{
				svc: domain_status_page.NewService(domain_status_page.NewSQLRepository(db), zap.NewNop().Sugar()),
				page: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO status_pages (id, slug, title) VALUES (?, ?, 'Status')", id, "page-"+id)
					require.NoError(t, err)
					return id
				},
			})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

func add(t *testing.T, svc domain_status_page.Service, pageID, domain string) *domain_status_page.Model {
	t.Helper()
	added, err := svc.AddDomainToStatusPage(context.Background(), pageID, domain)
	require.NoError(t, err)
	require.NotNil(t, added)
	return added
}

func domainsOf(t *testing.T, svc domain_status_page.Service, pageID string) []string {
	t.Helper()
	links, err := svc.GetDomainsForStatusPage(context.Background(), pageID)
	require.NoError(t, err)
	domains := []string{}
	for _, l := range links {
		domains = append(domains, l.Domain)
	}
	return domains
}

func TestCustomDomains_AddingADomainTwiceKeepsOneRow(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		page := f.page()
		first := add(t, f.svc, page, "status.example.com")
		second := add(t, f.svc, page, "status.example.com")

		assert.Equal(t, first.ID, second.ID)
		assert.Equal(t, []string{"status.example.com"}, domainsOf(t, f.svc, page))
	})
}

func TestCustomDomains_FindsThePageServedAtADomain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		public, internal := f.page(), f.page()
		add(t, f.svc, public, "status.example.com")
		add(t, f.svc, public, "uptime.example.com")
		add(t, f.svc, internal, "internal.example.com")

		found, err := f.svc.FindByDomain(ctx, "uptime.example.com")
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, public, found.StatusPageID)

		found, err = f.svc.FindByStatusPageAndDomain(ctx, internal, "internal.example.com")
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "internal.example.com", found.Domain)

		for _, lookup := range []func() (*domain_status_page.Model, error){
			func() (*domain_status_page.Model, error) { return f.svc.FindByDomain(ctx, "unknown.example.com") },
			func() (*domain_status_page.Model, error) {
				return f.svc.FindByStatusPageAndDomain(ctx, internal, "status.example.com")
			},
		} {
			missing, err := lookup()
			require.NoError(t, err)
			assert.Nil(t, missing)
		}

		assert.ElementsMatch(t, []string{"status.example.com", "uptime.example.com"}, domainsOf(t, f.svc, public))
	})
}

func TestCustomDomains_RemovesOnlyTheDomainsTheyName(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		public, internal := f.page(), f.page()
		add(t, f.svc, public, "status.example.com")
		add(t, f.svc, public, "uptime.example.com")
		add(t, f.svc, internal, "internal.example.com")
		add(t, f.svc, internal, "ops.example.com")

		require.NoError(t, f.svc.RemoveDomainFromStatusPage(ctx, public, "status.example.com"))
		assert.Equal(t, []string{"uptime.example.com"}, domainsOf(t, f.svc, public))
		gone, err := f.svc.FindByDomain(ctx, "status.example.com")
		require.NoError(t, err)
		assert.Nil(t, gone)

		require.NoError(t, f.svc.DeleteAllDomainsForStatusPage(ctx, internal))
		assert.Empty(t, domainsOf(t, f.svc, internal))
		assert.Equal(t, []string{"uptime.example.com"}, domainsOf(t, f.svc, public), "other pages keep their domains")
	})
}
