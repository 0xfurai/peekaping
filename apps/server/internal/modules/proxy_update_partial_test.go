package modules_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/proxy"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
)

// forEachRepositories runs fn on the repositories over every SQL storage backend. The PostgreSQL
// run is skipped without PEE_TEST_POSTGRES_DSN.
func forEachRepositories(t *testing.T, fn func(t *testing.T, repos repositories)) {
	t.Helper()
	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "repositories.db"))
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		fn(t, sqlRepositories(t, bun.NewDB(sqlDB, sqlitedialect.New())))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "repos_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]interface{}{"search_path": schema}))
		fn(t, sqlRepositories(t, bun.NewDB(sql.OpenDB(connector), pgdialect.New())))
	})
}

func TestProxyRepository_UpdatePartialChangesOnlyTheFieldsItIsGiven(t *testing.T) {
	forEachRepositories(t, func(t *testing.T, repos repositories) {
		ctx := context.Background()
		start := &proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "alice", Password: "s3cret"}
		str := func(s string) *string { return &s }
		num := func(n int) *int { return &n }
		flag := func(b bool) *bool { return &b }

		tests := []struct {
			name   string
			update proxy.UpdateModel
			want   proxy.Model // the proxy after the update, without its times
		}{
			{"the protocol", proxy.UpdateModel{Protocol: str("socks5")}, proxy.Model{Protocol: "socks5", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "alice", Password: "s3cret"}},
			{"the host", proxy.UpdateModel{Host: str("other.example.com")}, proxy.Model{Protocol: "http", Host: "other.example.com", Port: 8080, Auth: true, Username: "alice", Password: "s3cret"}},
			{"the port", proxy.UpdateModel{Port: num(3128)}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 3128, Auth: true, Username: "alice", Password: "s3cret"}},
			{"the auth flag, turned off", proxy.UpdateModel{Auth: flag(false)}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: false, Username: "alice", Password: "s3cret"}},
			{"the username", proxy.UpdateModel{Username: str("bob")}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "bob", Password: "s3cret"}},
			{"the username, emptied", proxy.UpdateModel{Username: str("")}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "", Password: "s3cret"}},
			{"the password", proxy.UpdateModel{Password: str("n3w")}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "alice", Password: "n3w"}},
			{"the password, emptied", proxy.UpdateModel{Password: str("")}, proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "alice", Password: ""}},
			{"every field", proxy.UpdateModel{Protocol: str("https"), Host: str("all.example.com"), Port: num(443), Auth: flag(false), Username: str(""), Password: str("")}, proxy.Model{Protocol: "https", Host: "all.example.com", Port: 443, Auth: false, Username: "", Password: ""}},
			{"two fields", proxy.UpdateModel{Host: str("two.example.com"), Password: str("p2")}, proxy.Model{Protocol: "http", Host: "two.example.com", Port: 8080, Auth: true, Username: "alice", Password: "p2"}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				created, err := repos.proxies.Create(ctx, repos.workspaceID, start)
				require.NoError(t, err)
				time.Sleep(20 * time.Millisecond)

				updated, err := repos.proxies.UpdatePartial(ctx, repos.workspaceID, created.ID, &tc.update)

				require.NoError(t, err)
				read, err := repos.proxies.FindByID(ctx, repos.workspaceID, created.ID)
				require.NoError(t, err)
				for _, got := range []*proxy.Model{updated, read} {
					assert.Equal(t, tc.want.Protocol, got.Protocol)
					assert.Equal(t, tc.want.Host, got.Host)
					assert.Equal(t, tc.want.Port, got.Port)
					assert.Equal(t, tc.want.Auth, got.Auth)
					assert.Equal(t, tc.want.Username, got.Username)
					assert.Equal(t, tc.want.Password, got.Password)
					assert.Equal(t, repos.workspaceID, got.WorkspaceID)
					assert.Equal(t, created.ID, got.ID)
				}
				assert.True(t, read.UpdatedAt.After(created.UpdatedAt), "updated_at moves: %s, then %s", created.UpdatedAt, read.UpdatedAt)
				assert.WithinDuration(t, created.CreatedAt, read.CreatedAt, time.Millisecond, "created_at stays")
			})
		}
	})
}

func TestProxyRepository_UpdatePartialWithNothingToChangeTouchesNothing(t *testing.T) {
	forEachRepositories(t, func(t *testing.T, repos repositories) {
		ctx := context.Background()
		created, err := repos.proxies.Create(ctx, repos.workspaceID, &proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080})
		require.NoError(t, err)
		before, err := repos.proxies.FindByID(ctx, repos.workspaceID, created.ID)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		got, err := repos.proxies.UpdatePartial(ctx, repos.workspaceID, created.ID, &proxy.UpdateModel{})

		require.NoError(t, err)
		assert.Equal(t, before, got, "the proxy as it is, with its update time")
		after, err := repos.proxies.FindByID(ctx, repos.workspaceID, created.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})
}

func TestProxyRepository_UpdatePartialIsNotFoundForAProxyThatIsNotInTheWorkspace(t *testing.T) {
	forEachRepositories(t, func(t *testing.T, repos repositories) {
		ctx := context.Background()
		created, err := repos.proxies.Create(ctx, repos.workspaceID, &proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080})
		require.NoError(t, err)
		host := "hijacked.example.com"

		for name, update := range map[string]*proxy.UpdateModel{"a change": {Host: &host}, "no change": {}} {
			_, err = repos.proxies.UpdatePartial(ctx, repos.otherWorkspaceID, created.ID, update)
			assert.ErrorIs(t, err, proxy.ErrNotFound, "another workspace, "+name)
			_, err = repos.proxies.UpdatePartial(ctx, repos.workspaceID, uuid.NewString(), update)
			assert.ErrorIs(t, err, proxy.ErrNotFound, "no such proxy, "+name)
		}

		read, err := repos.proxies.FindByID(ctx, repos.workspaceID, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "proxy.example.com", read.Host)
	})
}
