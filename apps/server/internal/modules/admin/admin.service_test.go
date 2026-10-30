package admin_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/admin"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// forEachBackend runs fn on a migrated database of every SQL storage backend
func forEachBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	t.Helper()
	open := map[string]func(*testing.T) *bun.DB{
		"SQLite": func(t *testing.T) *bun.DB {
			db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "admin.db")}, zap.NewNop().Sugar())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			return db
		},
		"Postgres": func(t *testing.T) *bun.DB {
			dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
			}
			root := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
			schema := "admin_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			_, err := root.Exec("CREATE SCHEMA ?", bun.Ident(schema))
			require.NoError(t, err)
			connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
			db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
			t.Cleanup(func() {
				require.NoError(t, db.Close())
				_, err := root.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
				require.NoError(t, err)
				require.NoError(t, root.Close())
			})
			return db
		},
	}
	for name, openDB := range open {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(context.Background()))
			_, err := migrator.Migrate(context.Background())
			require.NoError(t, err)
			fn(t, db)
		})
	}
}

// brokenRepo is the admin repository whose steps fail where the test says. findFails is the
// numbers, from 1, of the FindUser calls that fail.
type brokenRepo struct {
	admin.Repository
	lock       error
	findFails  map[int]bool
	finds      int
	setAdmin   error
	setActive  error
	ownedAlone error
	del        error
	// setActiveEnds are the times SetActive was given to end sessions at
	setActiveEnds []*time.Time
}

func (r *brokenRepo) LockActiveInstanceAdmins(ctx context.Context) ([]string, error) {
	if r.lock != nil {
		return nil, r.lock
	}
	return r.Repository.LockActiveInstanceAdmins(ctx)
}

func (r *brokenRepo) FindUser(ctx context.Context, id string) (*admin.User, error) {
	r.finds++
	if r.findFails[r.finds] {
		return nil, errors.New("the user can't be read")
	}
	return r.Repository.FindUser(ctx, id)
}

func (r *brokenRepo) SetInstanceAdmin(ctx context.Context, id string, on bool) error {
	if r.setAdmin != nil {
		return r.setAdmin
	}
	return r.Repository.SetInstanceAdmin(ctx, id, on)
}

func (r *brokenRepo) SetActive(ctx context.Context, id string, active bool, endAt *time.Time) error {
	r.setActiveEnds = append(r.setActiveEnds, endAt)
	if r.setActive != nil {
		return r.setActive
	}
	return r.Repository.SetActive(ctx, id, active, endAt)
}

func (r *brokenRepo) WorkspacesOwnedAloneAnyWorkspace(ctx context.Context, userID string) ([]admin.WorkspaceRef, error) {
	if r.ownedAlone != nil {
		return nil, r.ownedAlone
	}
	return r.Repository.WorkspacesOwnedAloneAnyWorkspace(ctx, userID)
}

func (r *brokenRepo) DeleteUser(ctx context.Context, id string) error {
	if r.del != nil {
		return r.del
	}
	return r.Repository.DeleteUser(ctx, id)
}

type harness struct {
	db   *bun.DB
	repo *brokenRepo
	svc  admin.Service
	bus  *eventstest.Bus
	logs *observer.ObservedLogs
}

func newHarness(t *testing.T, db *bun.DB) *harness {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()
	repo := &brokenRepo{Repository: admin.NewSQLRepository(db), findFails: map[int]bool{}}
	bus := eventstest.NewBus()
	svc := admin.NewService(repo, dbtx.NewSQLTransactor(db), nil, nil, nil, bus, mailer.New(&config.Config{}, logger),
		installsettings.Static(&config.Config{ClientURL: "http://localhost:3000"}), nil, logger)
	return &harness{db: db, repo: repo, svc: svc, bus: bus, logs: logs}
}

// addUser stores a user, with the instance admin flag and the account active or disabled
func (h *harness) addUser(t *testing.T, email string, instanceAdmin, active bool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := h.db.Exec("INSERT INTO users (id, email, password, instance_admin, active) VALUES (?, ?, 'hash', ?, ?)", id, email, instanceAdmin, active)
	require.NoError(t, err)
	return id
}

// addWorkspace stores a workspace with the owners and members
func (h *harness) addWorkspace(t *testing.T, name string, owners []string, members ...string) workspace.ID {
	t.Helper()
	repo := workspace.NewSQLRepository(h.db)
	made, err := repo.Create(context.Background(), name)
	require.NoError(t, err)
	for _, id := range owners {
		require.NoError(t, repo.AddMember(context.Background(), made.ID, id, workspace.RoleOwner))
	}
	for _, id := range members {
		require.NoError(t, repo.AddMember(context.Background(), made.ID, id, workspace.RoleMember))
	}
	return made.ID
}

// actions are the admin actions the service logged, as "action:target", oldest first
func (h *harness) actions() []string {
	var out []string
	for _, e := range h.logs.FilterMessage("Admin action").All() {
		ctx := e.ContextMap()
		out = append(out, ctx["action"].(string)+":"+ctx["targetUserId"].(string))
	}
	return out
}

func (h *harness) user(t *testing.T, id string) *admin.User {
	t.Helper()
	user, err := admin.NewSQLRepository(h.db).FindUser(context.Background(), id)
	require.NoError(t, err)
	return user
}

func flag(b bool) *bool { return &b }

func TestService_UpdateUser_RefusesTheLastActiveInstanceAdminAnyWayOutAndNoOther(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		only := h.addUser(t, "only@example.com", true, true)
		actor := uuid.NewString()

		for name, update := range map[string]admin.UserUpdate{
			"losing the flag":          {InstanceAdmin: flag(false)},
			"being disabled":           {Active: flag(false)},
			"losing the flag and more": {InstanceAdmin: flag(false), Active: flag(true)},
			"both":                     {InstanceAdmin: flag(false), Active: flag(false)},
		} {
			user, err := h.svc.UpdateUser(ctx, actor, only, update)

			assert.ErrorIs(t, err, admin.ErrLastInstanceAdmin, name)
			assert.Nil(t, user, name)
		}
		assert.True(t, h.user(t, only).InstanceAdmin)
		assert.True(t, h.user(t, only).Active)
		assert.Empty(t, h.actions(), "a refused change is not logged")

		// What leaves the last admin able to sign in is no loss of one
		for name, update := range map[string]admin.UserUpdate{
			"keeping the flag": {InstanceAdmin: flag(true)},
			"being enabled":    {Active: flag(true)},
			"nothing":          {},
			"keeping both":     {InstanceAdmin: flag(true), Active: flag(true)},
		} {
			user, err := h.svc.UpdateUser(ctx, actor, only, update)

			require.NoError(t, err, name)
			assert.True(t, user.InstanceAdmin, name)
			assert.True(t, user.Active, name)
		}
		assert.Empty(t, h.actions(), "and changes nothing, so it is not logged")
	})
}

func TestService_UpdateUser_AnAdminWhoCannotSignInIsNotCountedAndMayBeChanged(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		active := h.addUser(t, "active@example.com", true, true)
		disabled := h.addUser(t, "disabled@example.com", true, false)
		plain := h.addUser(t, "plain@example.com", false, true)
		actor := uuid.NewString()

		_, err := h.svc.UpdateUser(ctx, actor, disabled, admin.UserUpdate{InstanceAdmin: flag(false)})
		require.NoError(t, err, "a disabled admin loses the flag though there is one active admin only")
		_, err = h.svc.UpdateUser(ctx, actor, plain, admin.UserUpdate{Active: flag(false)})
		require.NoError(t, err, "a user who is no admin can be disabled")
		_, err = h.svc.UpdateUser(ctx, actor, active, admin.UserUpdate{Active: flag(false)})
		assert.ErrorIs(t, err, admin.ErrLastInstanceAdmin, "the only active admin can't")

		_, err = h.svc.UpdateUser(ctx, actor, plain, admin.UserUpdate{InstanceAdmin: flag(true), Active: flag(true)})
		require.NoError(t, err)
		_, err = h.svc.UpdateUser(ctx, actor, active, admin.UserUpdate{InstanceAdmin: flag(false)})
		require.NoError(t, err, "with a second active admin it can")
		assert.Equal(t, []string{
			"set_instance_admin:" + disabled,
			"disable_user:" + plain,
			"set_instance_admin:" + plain,
			"enable_user:" + plain,
			"set_instance_admin:" + active,
		}, h.actions())
	})
}

func TestService_UpdateUser_LogsEachChangeInOrderAndEndsTheSessionsOfADisabledUser(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		h.addUser(t, "keeper@example.com", true, true)
		target := h.addUser(t, "target@example.com", false, true)

		updated, err := h.svc.UpdateUser(ctx, "actor-1", target, admin.UserUpdate{InstanceAdmin: flag(true), Active: flag(false)})

		require.NoError(t, err)
		assert.True(t, updated.InstanceAdmin)
		assert.False(t, updated.Active)
		assert.Equal(t, []string{"set_instance_admin:" + target, "disable_user:" + target}, h.actions(), "the flag first, then the disabling")
		for _, e := range h.logs.FilterMessage("Admin action").All() {
			assert.Equal(t, "actor-1", e.ContextMap()["actorUserId"])
		}
		assert.Equal(t, []auth.SessionsEndedEvent{{UserID: target}}, eventstest.Published(h.bus, auth.SessionsEnded))
		require.Len(t, h.repo.setActiveEnds, 1)
		require.NotNil(t, h.repo.setActiveEnds[0], "the sessions that began before now end")
		assert.WithinDuration(t, time.Now(), *h.repo.setActiveEnds[0], time.Minute)
		assert.Zero(t, h.repo.setActiveEnds[0].Nanosecond()%int(time.Millisecond), "to the millisecond")

		h.bus.TakeTopics()
		h.logs.TakeAll()
		_, err = h.svc.UpdateUser(ctx, "actor-1", target, admin.UserUpdate{Active: flag(true)})

		require.NoError(t, err)
		assert.Equal(t, []string{"enable_user:" + target}, h.actions())
		assert.Nil(t, h.repo.setActiveEnds[1], "enabling ends no session")
		assert.Empty(t, h.bus.Topics(), "and announces nothing")

		h.logs.TakeAll()
		_, err = h.svc.UpdateUser(ctx, "actor-1", target, admin.UserUpdate{Active: flag(true), InstanceAdmin: flag(true)})

		require.NoError(t, err)
		assert.Empty(t, h.actions(), "a change to what already is is not logged")
		assert.Len(t, h.repo.setActiveEnds, 2, "and writes nothing")
	})
}

func TestService_UpdateUser_RefusesToDisableOneselfAndAUserThatIsNotThere(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		me := h.addUser(t, "me@example.com", true, true)
		h.addUser(t, "other@example.com", true, true)

		_, err := h.svc.UpdateUser(ctx, me, me, admin.UserUpdate{Active: flag(false)})
		assert.ErrorIs(t, err, admin.ErrSelf)
		assert.Zero(t, h.repo.finds, "before anything is read")
		_, err = h.svc.UpdateUser(ctx, me, me, admin.UserUpdate{InstanceAdmin: flag(false)})
		require.NoError(t, err, "oneself may lose the flag, and disabling is the only thing refused")
		_, err = h.svc.UpdateUser(ctx, me, me, admin.UserUpdate{Active: flag(true)})
		require.NoError(t, err)

		for _, id := range []string{uuid.NewString(), "not-a-uuid"} {
			_, err = h.svc.UpdateUser(ctx, me, id, admin.UserUpdate{Active: flag(false)})
			assert.ErrorIs(t, err, admin.ErrUserNotFound)
		}
	})
}

func TestService_UpdateUser_AFailureAtAnyStepChangesNothingAndLogsNothing(t *testing.T) {
	steps := map[string]func(r *brokenRepo){
		"the admins can't be locked":   func(r *brokenRepo) { r.lock = errors.New("lock failed") },
		"the user can't be read":       func(r *brokenRepo) { r.findFails[1] = true },
		"the flag can't be stored":     func(r *brokenRepo) { r.setAdmin = errors.New("flag failed") },
		"the account can't be stored":  func(r *brokenRepo) { r.setActive = errors.New("active failed") },
		"the user can't be read again": func(r *brokenRepo) { r.findFails[2] = true },
	}
	for name, fail := range steps {
		t.Run(name, func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, db *bun.DB) {
				h := newHarness(t, db)
				h.addUser(t, "keeper@example.com", true, true)
				target := h.addUser(t, "target@example.com", false, true)
				fail(h.repo)

				updated, err := h.svc.UpdateUser(context.Background(), "actor", target, admin.UserUpdate{InstanceAdmin: flag(true), Active: flag(false)})

				require.Error(t, err)
				assert.Nil(t, updated)
				assert.Empty(t, h.actions())
				assert.Empty(t, h.bus.Topics())
				stored := h.user(t, target)
				assert.False(t, stored.InstanceAdmin, "the transaction is rolled back")
				assert.True(t, stored.Active)
			})
		})
	}
}

func TestService_DeleteUser_RefusesInOrder(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		only := h.addUser(t, "only@example.com", true, true)
		disabledAdmin := h.addUser(t, "disabled@example.com", true, false)
		sole := h.addUser(t, "sole@example.com", false, true)
		h.addWorkspace(t, "Acme", []string{sole})
		h.addWorkspace(t, "Beta", []string{sole})
		actor := uuid.NewString()

		assert.ErrorIs(t, h.svc.DeleteUser(ctx, only, only), admin.ErrSelf)
		assert.Zero(t, h.repo.finds, "oneself is refused before anything is read")
		assert.ErrorIs(t, h.svc.DeleteUser(ctx, actor, uuid.NewString()), admin.ErrUserNotFound)
		assert.ErrorIs(t, h.svc.DeleteUser(ctx, actor, only), admin.ErrLastInstanceAdmin, "the only active admin stays")

		err := h.svc.DeleteUser(ctx, actor, sole)
		var lastOwner *admin.LastOwnerError
		require.ErrorAs(t, err, &lastOwner)
		assert.ErrorIs(t, err, admin.ErrLastOwner)
		require.Len(t, lastOwner.Workspaces, 2)
		assert.Equal(t, []string{"Acme", "Beta"}, []string{lastOwner.Workspaces[0].Name, lastOwner.Workspaces[1].Name})
		assert.NotNil(t, h.user(t, sole), "and they are still there")
		assert.Empty(t, h.actions())
		assert.Empty(t, h.bus.Topics())

		require.NoError(t, h.svc.DeleteUser(ctx, actor, disabledAdmin), "an admin who can't sign in is not counted")
		assert.Nil(t, h.user(t, disabledAdmin))
	})
}

func TestService_DeleteUser_LogsAndAnnouncesOnceTheUserIsGone(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		keeper := h.addUser(t, "keeper@example.com", true, true)
		leaver := h.addUser(t, "leaver@example.com", false, true)
		first := h.addWorkspace(t, "First", []string{keeper}, leaver)
		time.Sleep(10 * time.Millisecond)
		second := h.addWorkspace(t, "Second", []string{keeper}, leaver)

		require.NoError(t, h.svc.DeleteUser(ctx, keeper+"-actor", leaver))

		assert.Nil(t, h.user(t, leaver))
		assert.Equal(t, []string{"delete_user:" + leaver}, h.actions())
		assert.Equal(t, []string{"auth.sessions.ended", "workspace.member.left", "workspace.member.left"}, h.bus.Topics(), "the sessions end first, then each place is left")
		assert.Equal(t, []auth.SessionsEndedEvent{{UserID: leaver}}, eventstest.Published(h.bus, auth.SessionsEnded))
		assert.Equal(t, []workspace.MemberLeftEvent{{WorkspaceID: first, UserID: leaver}, {WorkspaceID: second, UserID: leaver}}, eventstest.Published(h.bus, workspace.MemberLeft))
	})
}

func TestService_DeleteUser_AFailureAtAnyStepDeletesNothingAndAnnouncesNothing(t *testing.T) {
	steps := map[string]func(r *brokenRepo){
		"the admins can't be locked": func(r *brokenRepo) { r.lock = errors.New("lock failed") },
		"the user can't be read":     func(r *brokenRepo) { r.findFails[1] = true },
		"the workspaces can't be read": func(r *brokenRepo) {
			r.ownedAlone = errors.New("owned failed")
		},
		"the user can't be deleted": func(r *brokenRepo) { r.del = errors.New("delete failed") },
	}
	for name, fail := range steps {
		t.Run(name, func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, db *bun.DB) {
				h := newHarness(t, db)
				h.addUser(t, "keeper@example.com", true, true)
				target := h.addUser(t, "target@example.com", false, true)
				fail(h.repo)

				err := h.svc.DeleteUser(context.Background(), "actor", target)

				require.Error(t, err)
				assert.Empty(t, h.actions())
				assert.Empty(t, h.bus.Topics())
				assert.NotNil(t, h.user(t, target))
			})
		})
	}
}
