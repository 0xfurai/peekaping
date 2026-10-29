package apitest

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-372: an instance admin deletes a workspace (docs/adr/0005 section 2 and 10). Everything in it
// goes by cascade, its monitors stop at once, and its members' open sockets leave its rooms.

// seededWorkspace is a workspace with a record of every kind that belongs to a workspace or to a
// monitor of it
type seededWorkspace struct {
	id          workspace.ID
	owner       credentials
	ownerEmail  string
	member      credentials
	memberEmail string
	monitors    []string
	pages       []string
	channels    []string
	keyToken    credentials
}

// seedWorkspace makes a workspace with an owner and a member, two monitors with heartbeats, stats,
// TLS information, a sent-notification record and links, a channel (that a probe also notifies
// through), a proxy, a tag, a maintenance window, a status page with a custom domain, an API key,
// an invite and a retention setting
func (a *app) seedWorkspace(t *testing.T, name, slug string) seededWorkspace {
	t.Helper()
	w := seededWorkspace{id: a.createWorkspace(t, name), ownerEmail: slug + "-owner@example.com", memberEmail: slug + "-member@example.com"}
	w.owner = a.userIn(t, w.ownerEmail, w.id, workspace.RoleOwner)
	w.member = a.userIn(t, w.memberEmail, w.id, workspace.RoleMember)

	channel := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody(slug+" channel"), w.owner), http.StatusCreated).ID
	proxy := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody(slug+".example"), w.owner), http.StatusCreated).ID
	tag := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody(slug+"-tag"), w.owner), http.StatusCreated).ID
	w.channels = []string{channel}
	require.NotEmpty(t, proxy)
	for _, monitorName := range []string{slug + " site", slug + " api"} {
		body := monitorWithLinksBody(monitorName, []string{channel}, []string{tag})
		w.monitors = append(w.monitors, decode[created](t, a.request(t, http.MethodPost, monitorsPath, body, w.owner), http.StatusCreated).ID)
	}
	require.NoError(t, a.container.Invoke(func(db *bun.DB, heartbeats heartbeat.Service, statService stats.Service, tls monitor_tls_info.Repository) error {
		ctx := context.Background()
		for _, id := range w.monitors {
			if _, err := heartbeats.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: id, Status: 1, Ping: 20, Time: time.Now()}); err != nil {
				return err
			}
			if err := statService.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{MonitorID: id, Status: 1, Ping: 20, Time: time.Now().Unix()}); err != nil {
				return err
			}
			if _, err := tls.Upsert(ctx, id, `{"valid":true}`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO notification_sent_history (id, type, monitor_id, days) VALUES (?, 'certificate', ?, 7)", uuid.NewString(), id); err != nil {
				return err
			}
		}
		_, err := db.ExecContext(ctx, "INSERT INTO probe_notification_channels (probe_id, notification_channel_id) VALUES ('local', ?)", channel)
		return err
	}))

	decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody(slug+" window"), "monitor_ids", w.monitors), w.owner), http.StatusCreated)
	w.pages = []string{a.createStatusPageAs(t, w.owner, statusPage{slug: slug + "-page", published: true, monitors: w.monitors, domains: []string{slug + ".example.com"}})}
	a.createKey(t, slug+" key", w.owner)
	a.invite(t, slug+"-invitee@example.com", "member", w.owner)
	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "33", "type": "int"}, w.owner), http.StatusOK)
	return w
}

// footprint counts the rows that belong to the workspace, table by table. A row belongs to it
// through a workspace_id, or through the monitor, status page or notification channel it hangs on:
// every table with such a column is counted, so a table added later is too.
func (a *app) footprint(t *testing.T, w seededWorkspace) map[string]int {
	t.Helper()
	counts := map[string]int{}
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		ctx := context.Background()
		owned := map[string][]string{
			"workspace_id":            {string(w.id)},
			"monitor_id":              w.monitors,
			"status_page_id":          w.pages,
			"notification_channel_id": w.channels,
		}
		for column, ids := range owned {
			var tables []string
			query := "SELECT table_name FROM information_schema.columns WHERE table_schema = current_schema() AND column_name = ?"
			if a.cfg.DBType == "sqlite" {
				query = "SELECT m.name FROM sqlite_master AS m, pragma_table_info(m.name) AS p WHERE m.type = 'table' AND p.name = ?"
			}
			if err := db.NewRaw(query, column).Scan(ctx, &tables); err != nil {
				return err
			}
			for _, table := range tables {
				var n int
				if err := db.NewSelect().TableExpr("?", bun.Ident(table)).ColumnExpr("count(*)").Where("? IN (?)", bun.Ident(column), bun.In(ids)).Scan(ctx, &n); err != nil {
					return err
				}
				counts[table] += n
			}
		}
		var n int
		if err := db.NewSelect().Table("workspaces").ColumnExpr("count(*)").Where("id = ?", string(w.id)).Scan(ctx, &n); err != nil {
			return err
		}
		counts["workspaces"] = n
		return nil
	}))
	return counts
}

// everyTableOfAWorkspace are the tables that must hold a row of a seeded workspace
var everyTableOfAWorkspace = []string{
	"workspaces", "workspace_members", "workspace_settings", "workspace_invites", "api_keys",
	"monitors", "notification_channels", "proxies", "tags", "maintenances", "status_pages",
	"heartbeats", "stat_minutely", "stat_hourly", "stat_daily", "monitor_notifications",
	"monitor_maintenances", "monitor_status_pages", "monitor_tags", "monitor_probes", "monitor_tls_info",
	"notification_sent_history", "domain_status_page", "probe_notification_channels",
}

func (a *app) deleteWorkspace(t *testing.T, id string, creds ...credentials) *httpRecorder {
	t.Helper()
	if len(creds) == 0 {
		creds = []credentials{a.asAdmin}
	}
	return a.request(t, http.MethodDelete, adminWorkspacesPath+"/"+id, nil, creds...)
}

// watchDeleted collects the monitors the bus announces as deleted
func watchDeleted(t *testing.T, a *app) *deletedFeed {
	t.Helper()
	feed := &deletedFeed{}
	require.NoError(t, a.container.Invoke(func(bus events.EventBus) {
		events.Subscribe(events.NewSubscriber(bus, "workspace-delete-test"), events.MonitorDeleted, feed.add)
	}))
	return feed
}

func TestAdminWorkspaceDelete_RemovesEveryRowOfTheWorkspaceAndNoneOfAnothers(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		doomed := a.seedWorkspace(t, "Doomed", "doomed")
		kept := a.seedWorkspace(t, "Kept", "kept")
		// the admin is in the workspace they delete
		a.addMember(t, doomed.id, a.userID(t, adminEmail), workspace.RoleAdmin)
		before, keptBefore := a.footprint(t, doomed), a.footprint(t, kept)
		for _, table := range everyTableOfAWorkspace {
			require.Positive(t, before[table], "the workspace has a row in "+table)
			require.Positive(t, keptBefore[table], "so has the other, in "+table)
		}
		total := a.listWorkspaces(t, "").Total

		w := a.deleteWorkspace(t, string(doomed.id))

		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		assert.Empty(t, w.Body.String())
		for table, n := range a.footprint(t, doomed) {
			assert.Zero(t, n, "nothing is left in "+table)
		}
		assert.Equal(t, keptBefore, a.footprint(t, kept), "another workspace keeps every row")
		assert.Equal(t, total-1, a.listWorkspaces(t, "").Total)
		for _, listed := range a.listWorkspaces(t, "?limit=100").Workspaces {
			assert.NotEqual(t, string(doomed.id), listed.ID)
		}
		// the proxy of a monitor that is gone is gone with its workspace, and the install's own rows stay
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, monitorsPath+"/"+kept.monitors[0], nil, kept.owner).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, monitorsPath+"/"+doomed.monitors[0], nil, a.asAdmin, inWorkspace(kept.id)).Code)
	})
}

func TestAdminWorkspaceDelete_TheMonitorsStopAtOnceAndTheirPublicPagesGoWithThem(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedWorkspace(t, "Doomed", "doomed")
		source := a.schedulerSource(t)
		var gone sync.Map
		source.Watch(func(string) {}, func(string) {}, func(id string) { gone.Store(id, true) })
		feed := watchDeleted(t, a)
		require.ElementsMatch(t, w.monitors, intersect(scheduled(t, source), w.monitors), "they run before")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/doomed-page", nil).Code)

		require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(w.id)).Code)

		assert.Empty(t, intersect(scheduled(t, source), w.monitors), "the scheduler's list")
		for _, id := range w.monitors {
			found, err := source.Find(context.Background(), id)
			require.NoError(t, err)
			assert.Nil(t, found, "the scheduler no longer has the monitor")
		}
		require.Eventually(t, func() bool {
			seen := feed.ids()
			return len(intersect(seen, w.monitors)) == len(w.monitors)
		}, 5*time.Second, 5*time.Millisecond, "MonitorDeleted arrived for each monitor")
		assert.ElementsMatch(t, w.monitors, intersect(feed.ids(), w.monitors), "once each")
		for _, id := range w.monitors {
			_, stopped := gone.Load(id)
			assert.True(t, stopped, "the scheduler is told to stop the monitor")
		}
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/doomed-page", nil).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/status-pages/domain/doomed.example.com", nil).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/push/doomed-site-token?status=up", nil).Code, "a push to a deleted monitor finds nothing")
		assert.Empty(t, a.storedHeartbeatsOf(t, w.monitors))
	})
}

func (a *app) storedHeartbeatsOf(t *testing.T, monitorIDs []string) []string {
	t.Helper()
	var out []string
	for _, id := range monitorIDs {
		for _, beat := range a.storedHeartbeats(t, id) {
			out = append(out, beat.MonitorID)
		}
	}
	return out
}

func TestAdminWorkspaceDelete_MembersLeaveTheRoomsOnlyOnceTheDeleteIsStored(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		leaves := &publishedLeaves{}
		a := newAppDecorated(t, func(c *dig.Container) error {
			return c.Decorate(func(bus events.EventBus, db *bun.DB) events.EventBus {
				return &recordingBus{EventBus: bus, db: func() *bun.DB { return db }, leaves: leaves}
			})
		}, backend)
		a.registerAdmin(t)
		doomed := a.seedWorkspace(t, "Doomed", "doomed")
		kept := a.seedWorkspace(t, "Kept", "kept")
		a.addMember(t, doomed.id, a.userID(t, adminEmail), workspace.RoleAdmin)
		// the member of both is announced for the one that is gone only
		shared := a.userID(t, kept.memberEmail)
		a.addMember(t, doomed.id, shared, workspace.RoleAdmin)
		var users []string
		for _, email := range []string{doomed.ownerEmail, doomed.memberEmail, adminEmail, kept.memberEmail} {
			users = append(users, a.userID(t, email))
		}

		require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(doomed.id)).Code)

		published, gone := leaves.snapshot()
		var want []workspace.MemberLeftEvent
		for _, user := range users {
			want = append(want, workspace.MemberLeftEvent{WorkspaceID: doomed.id, UserID: user})
		}
		assert.ElementsMatch(t, want, published, "one for each member of the workspace, and none for another")
		assert.Equal(t, slices.Repeat([]bool{true}, len(published)), gone, "they were gone when it was announced")
	})
}

func TestAdminWorkspaceDelete_MembersKeepTheirAccountsAndOtherWorkspaces(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		doomed := a.seedWorkspace(t, "Doomed", "doomed")
		kept := a.seedWorkspace(t, "Kept", "kept")
		both := a.createUser(t, "both@example.com")
		a.addMember(t, kept.id, both, workspace.RoleMember)
		a.addMember(t, doomed.id, both, workspace.RoleMember)
		bothCreds := a.loginAs(t, "both@example.com")
		require.Len(t, a.meOf(t, bothCreds).Workspaces, 2)
		require.Len(t, a.meOf(t, doomed.member).Workspaces, 1)

		require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(doomed.id)).Code)

		me := a.meOf(t, bothCreds)
		require.Len(t, me.Workspaces, 1, "the workspace is gone from /me")
		assert.Equal(t, string(kept.id), me.Workspaces[0].ID)
		require.NotNil(t, me.DefaultWorkspaceID)
		assert.Equal(t, string(kept.id), *me.DefaultWorkspaceID)

		// a member who had only that workspace keeps the account, signed in, with nothing
		for _, creds := range []credentials{doomed.owner, doomed.member} {
			alone := a.request(t, http.MethodGet, "/api/v1/me", nil, creds)
			me := decode[meResponse](t, alone, http.StatusOK)
			assert.NotNil(t, me.Workspaces, "an empty list, not null")
			assert.Empty(t, me.Workspaces)
			assert.Nil(t, me.DefaultWorkspaceID)
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, monitorsPath, nil, creds).Code, "they see no workspace")
			assert.Less(t, a.request(t, http.MethodGet, "/api/v1/notification-channels", nil, creds).Code, 500)
		}
		assert.Equal(t, http.StatusOK, a.login(t, userLogin(doomed.memberEmail)).Code, "they can still sign in")
		assert.Equal(t, 1, a.listUsers(t, "?q="+doomed.ownerEmail).Total, "and are still listed")
	})
}

func TestAdminWorkspaceDelete_AnAPIKeyOfTheWorkspaceStopsWorking(t *testing.T) {
	a := newAppWithAdmin(t)
	doomed := a.seedWorkspace(t, "Doomed", "doomed")
	key := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "ci"}, doomed.owner), http.StatusCreated).Token
	asKey := withHeader("X-API-Key", key)
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, monitorsPath, nil, asKey).Code)

	require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(doomed.id)).Code)

	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, monitorsPath, nil, asKey).Code)
}

func TestAdminWorkspaceDelete_AnUnknownWorkspaceIs404AndNothingChanges(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedWorkspace(t, "Kept", "kept")
		before := a.footprint(t, w)

		for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", "507f1f77bcf86cd799439011"} {
			assert.Equal(t, http.StatusNotFound, a.deleteWorkspace(t, id).Code, id)
		}
		assert.Equal(t, before, a.footprint(t, w))

		// and a second delete of the same workspace
		require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(w.id)).Code)
		assert.Equal(t, http.StatusNotFound, a.deleteWorkspace(t, string(w.id)).Code)
	})
}

func TestAdminWorkspaceDelete_OnlyAnInstanceAdminSignedInAsAUserMayDoIt(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Kept", "kept")
	key := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "ci"}, w.owner), http.StatusCreated).Token
	before := a.footprint(t, w)

	assert.Equal(t, http.StatusForbidden, a.deleteWorkspace(t, string(w.id), w.owner).Code, "the owner")
	assert.Equal(t, http.StatusForbidden, a.deleteWorkspace(t, string(w.id), w.member).Code, "a member")
	assert.Equal(t, http.StatusForbidden, a.deleteWorkspace(t, string(w.id), withHeader("X-API-Key", key)).Code, "its own API key")
	assert.Equal(t, http.StatusForbidden, a.deleteWorkspace(t, string(w.id), a.withAPIKey).Code, "another's API key")
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodDelete, adminWorkspacesPath+"/"+string(w.id), nil).Code)
	assert.Equal(t, before, a.footprint(t, w), "a refused delete changes nothing")
}

func TestAdminWorkspaceDelete_LogsWhoDidItToWhichWorkspace(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	adminID := a.userID(t, adminEmail)
	id := a.createWorkspace(t, "Logged")

	require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(id)).Code)

	entries := logs.FilterMessage("Admin action").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	assert.Len(t, fields, 3, "who, what and to which workspace, and nothing else")
	assert.Equal(t, adminID, fields["actorUserId"])
	assert.Equal(t, "delete_workspace", fields["action"])
	assert.Equal(t, string(id), fields["targetWorkspaceId"])

	// a refused delete logs nothing
	logs.TakeAll()
	require.Equal(t, http.StatusNotFound, a.deleteWorkspace(t, string(id)).Code)
	assert.Empty(t, logs.FilterMessage("Admin action").All())
}

func TestAdminWorkspaceDelete_AWorkspaceThatIsSuspendedCanBeDeletedToo(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Held", "held")
	a.setSuspended(t, w.id, true)

	require.Equal(t, http.StatusNoContent, a.deleteWorkspace(t, string(w.id)).Code)

	for table, n := range a.footprint(t, w) {
		assert.Zero(t, n, table)
	}
	names := []string{}
	for _, listed := range a.listWorkspaces(t, "?limit=100").Workspaces {
		names = append(names, listed.Name)
	}
	sort.Strings(names)
	assert.NotContains(t, names, "Held")
}
