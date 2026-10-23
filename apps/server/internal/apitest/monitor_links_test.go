package apitest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// unknownID is a well-formed ID that no tag, notification channel or monitor has.
const unknownID = "00000000-0000-0000-0000-000000000001"

// monitorFeed is what a bus subscriber has seen on a monitor topic.
type monitorFeed struct {
	mu   sync.Mutex
	seen []shared.Monitor
}

func (f *monitorFeed) add(m shared.Monitor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, m)
}

// names returns the names of the monitors the subscriber has been told about, oldest first.
func (f *monitorFeed) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.seen))
	for _, m := range f.seen {
		names = append(names, m.Name)
	}
	return names
}

// subscribeMonitors subscribes handler to topic on the app's event bus, as a listener in the
// app would. Events are delivered asynchronously.
func (a *app) subscribeMonitors(t *testing.T, name string, topic events.Topic[shared.Monitor], handler func(context.Context, shared.Monitor)) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(bus events.EventBus) {
		events.Subscribe(events.NewSubscriber(bus, name), topic, handler)
	}))
}

// watchMonitors records the monitors published on topic.
func (a *app) watchMonitors(t *testing.T, name string, topic events.Topic[shared.Monitor]) *monitorFeed {
	t.Helper()
	feed := &monitorFeed{}
	a.subscribeMonitors(t, name, topic, func(_ context.Context, m shared.Monitor) { feed.add(m) })
	return feed
}

// waitForMonitor waits until the feed has been told about the monitor with this name.
func (f *monitorFeed) waitForMonitor(t *testing.T, name string) {
	t.Helper()
	require.Eventually(t, func() bool { return slices.Contains(f.names(), name) }, 5*time.Second, 5*time.Millisecond,
		"no event for %q arrived", name)
}

func (a *app) listMonitors(t *testing.T) []monitorResponse {
	t.Helper()
	return decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
}

func TestMonitorsAPI_CreateFailsWholeWhenALinkFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		link string
	}{
		{"unknown tag", "tag_ids"},
		{"unknown notification channel", "notification_ids"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			created := a.watchMonitors(t, "created", events.MonitorCreated)

			body := with(pushMonitorBody("Doomed", "doomed-token"), tc.link, []string{unknownID})
			w := a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin)

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Empty(t, a.listMonitors(t), "no monitor is stored")

			// Delivery is asynchronous: a create that works proves events flow, and once its event
			// has arrived, the failed create's would have too.
			a.createMonitor(t, "Control", "control-token")
			created.waitForMonitor(t, "Control")
			assert.Equal(t, []string{"Control"}, created.names(), "no MonitorCreated for the failed create")
		})
	}
}

// readChannelsOnEvent subscribes a listener to topic that reads the notification channels of the
// monitor each event is about, as a listener in the app that sends to them would.
func (a *app) readChannelsOnEvent(t *testing.T, topic events.Topic[shared.Monitor]) *monitorLinks {
	t.Helper()
	var links monitorLinks
	var channels monitor_notification.Service
	require.NoError(t, a.container.Invoke(func(s monitor_notification.Service) { channels = s }))
	a.subscribeMonitors(t, "channels", topic, func(ctx context.Context, m shared.Monitor) {
		rels, err := channels.FindByMonitorID(ctx, m.ID)
		assert.NoError(t, err)
		ids := make([]string, 0, len(rels))
		for _, rel := range rels {
			ids = append(ids, rel.NotificationID)
		}
		links.set(m.Name, ids)
	})
	return &links
}

// manyNames returns n monitor names. A listener that wins its race against one request is luck,
// so a test that checks what a listener sees uses a few dozen requests.
func manyNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("Monitor %d", i+1)
	}
	return names
}

func TestMonitorsAPI_CreatedEventSeesTheMonitorsChannels(t *testing.T) {
	a := newAppWithAdmin(t)
	channelID := a.createChannel(t, "Ops webhook")
	links := a.readChannelsOnEvent(t, events.MonitorCreated)

	names := manyNames(30)
	for _, name := range names {
		body := with(pushMonitorBody(name, name+"-token"), "notification_ids", []string{channelID})
		decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)
	}

	require.Eventually(t, func() bool { return links.count() == len(names) }, 5*time.Second, 5*time.Millisecond)
	for _, name := range names {
		assert.Equal(t, []string{channelID}, links.get(name), "the channels a MonitorCreated listener reads for %s", name)
	}
}

// monitorLinks is what a listener found linked to each monitor, by monitor name.
type monitorLinks struct {
	mu    sync.Mutex
	found map[string][]string
}

func (l *monitorLinks) set(name string, ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.found == nil {
		l.found = map[string][]string{}
	}
	l.found[name] = ids
}

func (l *monitorLinks) get(name string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.found[name]
}

func (l *monitorLinks) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.found)
}

func (a *app) createChannel(t *testing.T, name string) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody(name), a.asAdmin), http.StatusCreated).ID
}

func (a *app) createTag(t *testing.T, name string) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody(name), a.asAdmin), http.StatusCreated).ID
}

// monitorWithLinks is a monitor as GET /monitors/{id} returns it.
type monitorWithLinks struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Interval        int      `json:"interval"`
	NotificationIDs []string `json:"notification_ids"`
	TagIDs          []string `json:"tag_ids"`
}

func (a *app) getMonitorWithLinks(t *testing.T, id string) monitorWithLinks {
	t.Helper()
	return decode[monitorWithLinks](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)
}

// monitorWithLinksBody is a push monitor linked to channelIDs and tagIDs.
func monitorWithLinksBody(name string, channelIDs, tagIDs []string) map[string]any {
	body := pushMonitorBody(name, name+"-token")
	body["notification_ids"] = channelIDs
	body["tag_ids"] = tagIDs
	return body
}

// linkedMonitors is an app with a monitor that has a channel and a tag, a monitor that has
// neither, and a second channel and tag to link to.
type linkedMonitors struct {
	channelA, channelB, tagA, tagB string
	linked, bare                   string
}

func (a *app) createLinkedMonitors(t *testing.T) linkedMonitors {
	t.Helper()
	f := linkedMonitors{
		channelA: a.createChannel(t, "Channel A"), channelB: a.createChannel(t, "Channel B"),
		tagA: a.createTag(t, "Tag A"), tagB: a.createTag(t, "Tag B"),
	}
	f.linked = decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Linked", []string{f.channelA}, []string{f.tagA}), a.asAdmin), http.StatusCreated).ID
	f.bare = a.createMonitor(t, "Bare", "bare-token")
	return f
}

// withoutIDs clears the IDs, which a test can't know for a monitor it is about to create.
func withoutIDs(monitors []monitorWithLinks) []monitorWithLinks {
	cleared := make([]monitorWithLinks, len(monitors))
	for i, m := range monitors {
		m.ID = ""
		cleared[i] = m
	}
	return cleared
}

func TestMonitorsAPI_EveryEndpointReturnsTheLinks(t *testing.T) {
	linked := func(f linkedMonitors) monitorWithLinks {
		return monitorWithLinks{Name: "Linked", Interval: 60, NotificationIDs: []string{f.channelA}, TagIDs: []string{f.tagA}}
	}
	bare := monitorWithLinks{Name: "Bare", Interval: 60, NotificationIDs: []string{}, TagIDs: []string{}}

	for _, tc := range []struct {
		name string
		// send makes the request and returns the monitors the response holds
		send func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks
		want func(f linkedMonitors) []monitorWithLinks
	}{
		{"list", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			return decode[[]monitorWithLinks](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
		}, func(f linkedMonitors) []monitorWithLinks { return []monitorWithLinks{linked(f), bare} }},

		{"batch", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			return decode[[]monitorWithLinks](t, a.request(t, http.MethodGet, "/api/v1/monitors/batch?ids="+f.linked+","+f.bare, nil, a.asAdmin), http.StatusOK)
		}, func(f linkedMonitors) []monitorWithLinks { return []monitorWithLinks{linked(f), bare} }},

		{"create with links", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			body := monitorWithLinksBody("Created", []string{f.channelB}, []string{f.tagB})
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Created", Interval: 60, NotificationIDs: []string{f.channelB}, TagIDs: []string{f.tagB}}}
		}},

		{"create without links", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			body := pushMonitorBody("Created", "created-token")
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Created", Interval: 60, NotificationIDs: []string{}, TagIDs: []string{}}}
		}},

		{"full update with other links", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			body := monitorWithLinksBody("Linked", []string{f.channelB}, []string{f.tagB})
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+f.linked, body, a.asAdmin), http.StatusOK)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Linked", Interval: 60, NotificationIDs: []string{f.channelB}, TagIDs: []string{f.tagB}}}
		}},

		{"full update with no links", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			body := monitorWithLinksBody("Linked", []string{}, []string{})
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+f.linked, body, a.asAdmin), http.StatusOK)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Linked", Interval: 60, NotificationIDs: []string{}, TagIDs: []string{}}}
		}},

		{"partial update with IDs", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			body := map[string]any{"notification_ids": []string{f.channelB}, "tag_ids": []string{f.tagB}}
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPatch, "/api/v1/monitors/"+f.linked, body, a.asAdmin), http.StatusOK)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Linked", Interval: 60, NotificationIDs: []string{f.channelB}, TagIDs: []string{f.tagB}}}
		}},

		{"partial update without IDs", func(t *testing.T, a *app, f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{decode[monitorWithLinks](t, a.request(t, http.MethodPatch, "/api/v1/monitors/"+f.linked, map[string]any{"interval": 90}, a.asAdmin), http.StatusOK)}
		}, func(f linkedMonitors) []monitorWithLinks {
			return []monitorWithLinks{{Name: "Linked", Interval: 90, NotificationIDs: []string{f.channelA}, TagIDs: []string{f.tagA}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			f := a.createLinkedMonitors(t)

			got := tc.send(t, a, f)

			assert.ElementsMatch(t, tc.want(f), withoutIDs(got))
		})
	}
}

// linkQueries counts the SELECTs on the monitor_notifications and monitor_tags tables that run for
// a request, as a bun query hook. Only queries run with the context of a request made by get
// count, so a listener still reading links for an earlier write doesn't add to them.
type linkQueries struct {
	mu                  sync.Mutex
	notifications, tags int
}

type countedRequest struct{}

func (q *linkQueries) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (q *linkQueries) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if ctx.Value(countedRequest{}) != q || !strings.HasPrefix(event.Query, "SELECT") {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if strings.Contains(event.Query, `"monitor_notifications"`) {
		q.notifications++
	}
	if strings.Contains(event.Query, `"monitor_tags"`) {
		q.tags++
	}
}

// get sends a GET as the admin and counts the link queries it runs.
func (q *linkQueries) get(t *testing.T, a *app, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(context.WithValue(req.Context(), countedRequest{}, q))
	a.asAdmin(req)
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, req)
	return w
}

func TestMonitorsAPI_ListsLoadTheLinksInOneQueryPerRelation(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(ids []string) string
	}{
		{"list", func([]string) string { return "/api/v1/monitors?limit=5" }},
		{"batch", func(ids []string) string { return "/api/v1/monitors/batch?ids=" + strings.Join(ids, ",") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			queries := &linkQueries{}
			require.NoError(t, a.container.Invoke(func(db *bun.DB) { db.AddQueryHook(queries) }))
			channelID, tagID := a.createChannel(t, "Ops webhook"), a.createTag(t, "Production")
			var ids []string
			for _, name := range manyNames(5) {
				body := monitorWithLinksBody(name, []string{channelID}, []string{tagID})
				ids = append(ids, decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated).ID)
			}

			monitors := decode[[]monitorWithLinks](t, queries.get(t, a, tc.path(ids)), http.StatusOK)

			require.Len(t, monitors, 5)
			for _, m := range monitors {
				assert.Equal(t, []string{channelID}, m.NotificationIDs, m.Name)
				assert.Equal(t, []string{tagID}, m.TagIDs, m.Name)
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			assert.Equal(t, 1, queries.notifications, "queries on monitor_notifications for 5 monitors")
			assert.Equal(t, 1, queries.tags, "queries on monitor_tags for 5 monitors")
		})
	}
}

func TestMonitorsAPI_GetByIDAnswersWithTheFieldsOfTheList(t *testing.T) {
	a := newAppWithAdmin(t)
	channelID, tagID := a.createChannel(t, "Channel A"), a.createTag(t, "Tag A")
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Linked", []string{channelID}, []string{tagID}), a.asAdmin), http.StatusCreated).ID

	byID := decode[map[string]any](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)
	list := decode[[]map[string]any](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)

	require.Len(t, list, 1)
	assert.Equal(t, list[0], byID, "one monitor, the same fields from both endpoints")
	assert.Equal(t, "Linked-token", byID["push_token"], "the push token sent on create")
	assert.Equal(t, []any{channelID}, byID["notification_ids"])
	assert.Equal(t, []any{tagID}, byID["tag_ids"])
}

func TestMonitorsAPI_FullUpdateReplacesTheLinks(t *testing.T) {
	a := newAppWithAdmin(t)
	channelA, channelB := a.createChannel(t, "Channel A"), a.createChannel(t, "Channel B")
	tagA, tagB := a.createTag(t, "Tag A"), a.createTag(t, "Tag B")
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Original", []string{channelA}, []string{tagA}), a.asAdmin), http.StatusCreated).ID
	require.Equal(t, monitorWithLinks{ID: id, Name: "Original", Interval: 60, NotificationIDs: []string{channelA}, TagIDs: []string{tagA}}, a.getMonitorWithLinks(t, id))

	decode[any](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+id, monitorWithLinksBody("Renamed", []string{channelB}, []string{tagB}), a.asAdmin), http.StatusOK)
	assert.Equal(t, monitorWithLinks{ID: id, Name: "Renamed", Interval: 60, NotificationIDs: []string{channelB}, TagIDs: []string{tagB}}, a.getMonitorWithLinks(t, id))

	decode[any](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+id, monitorWithLinksBody("Renamed", []string{}, []string{}), a.asAdmin), http.StatusOK)
	assert.Equal(t, monitorWithLinks{ID: id, Name: "Renamed", Interval: 60, NotificationIDs: []string{}, TagIDs: []string{}}, a.getMonitorWithLinks(t, id), "a full update with no IDs unlinks everything")
}

func TestMonitorsAPI_PartialUpdateChangesLinksOnlyWhenIDsAreSent(t *testing.T) {
	a := newAppWithAdmin(t)
	channelA, channelB := a.createChannel(t, "Channel A"), a.createChannel(t, "Channel B")
	tagA, tagB := a.createTag(t, "Tag A"), a.createTag(t, "Tag B")
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Original", []string{channelA}, []string{tagA}), a.asAdmin), http.StatusCreated).ID
	path := "/api/v1/monitors/" + id

	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"interval": 90}, a.asAdmin), http.StatusOK)
	assert.Equal(t, monitorWithLinks{ID: id, Name: "Original", Interval: 90, NotificationIDs: []string{channelA}, TagIDs: []string{tagA}}, a.getMonitorWithLinks(t, id), "no IDs sent: the links stay")

	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"notification_ids": []string{}, "tag_ids": []string{}}, a.asAdmin), http.StatusOK)
	assert.Equal(t, monitorWithLinks{ID: id, Name: "Original", Interval: 90, NotificationIDs: []string{channelA}, TagIDs: []string{tagA}}, a.getMonitorWithLinks(t, id), "empty lists count as not sent")

	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"notification_ids": []string{channelA, channelB}, "tag_ids": []string{tagB}}, a.asAdmin), http.StatusOK)
	got := a.getMonitorWithLinks(t, id)
	assert.ElementsMatch(t, []string{channelA, channelB}, got.NotificationIDs, "IDs sent: the links become exactly those")
	assert.Equal(t, []string{tagB}, got.TagIDs)
}

func TestMonitorsAPI_RepeatedLinkIDsAreLinkedOnce(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			a := newAppWithAdmin(t)
			channelA, channelB := a.createChannel(t, "Channel A"), a.createChannel(t, "Channel B")
			tagA, tagB := a.createTag(t, "Tag A"), a.createTag(t, "Tag B")
			channels := []string{channelA, channelB, channelA}
			tags := []string{tagA, tagA, tagB, tagA}

			var id string
			switch method {
			case http.MethodPost:
				id = decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Repeated", channels, tags), a.asAdmin), http.StatusCreated).ID
			case http.MethodPut:
				id = a.createMonitor(t, "Repeated", "Repeated-token")
				decode[any](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+id, monitorWithLinksBody("Repeated", channels, tags), a.asAdmin), http.StatusOK)
			case http.MethodPatch:
				id = a.createMonitor(t, "Repeated", "Repeated-token")
				decode[any](t, a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, map[string]any{"notification_ids": channels, "tag_ids": tags}, a.asAdmin), http.StatusOK)
			}

			got := a.getMonitorWithLinks(t, id)
			assert.ElementsMatch(t, []string{channelA, channelB}, got.NotificationIDs, "one link for each channel")
			assert.ElementsMatch(t, []string{tagA, tagB}, got.TagIDs, "one link for each tag")
		})
	}
}

func TestMonitorsAPI_UpdateFailsWholeWhenALinkFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		// body is the update, with unknownID as the last link in the list that fails
		body func(channelB, tagB string) map[string]any
	}{
		{"full update, unknown tag", http.MethodPut, func(channelB, tagB string) map[string]any {
			return monitorWithLinksBody("Changed", []string{channelB}, []string{tagB, unknownID})
		}},
		{"full update, unknown notification channel", http.MethodPut, func(channelB, tagB string) map[string]any {
			return monitorWithLinksBody("Changed", []string{channelB, unknownID}, []string{tagB})
		}},
		{"partial update, unknown tag", http.MethodPatch, func(channelB, tagB string) map[string]any {
			return map[string]any{"name": "Changed", "interval": 90, "tag_ids": []string{tagB, unknownID}}
		}},
		{"partial update, unknown notification channel", http.MethodPatch, func(channelB, tagB string) map[string]any {
			return map[string]any{"name": "Changed", "interval": 90, "notification_ids": []string{channelB, unknownID}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			channelA, channelB := a.createChannel(t, "Channel A"), a.createChannel(t, "Channel B")
			tagA, tagB := a.createTag(t, "Tag A"), a.createTag(t, "Tag B")
			id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody("Original", []string{channelA}, []string{tagA}), a.asAdmin), http.StatusCreated).ID
			controlID := a.createMonitor(t, "Control", "control-token")
			updated := a.watchMonitors(t, "updated", events.MonitorUpdated)

			w := a.request(t, tc.method, "/api/v1/monitors/"+id, tc.body(channelB, tagB), a.asAdmin)

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Equal(t, monitorWithLinks{ID: id, Name: "Original", Interval: 60, NotificationIDs: []string{channelA}, TagIDs: []string{tagA}}, a.getMonitorWithLinks(t, id),
				"the monitor and its links are as they were")

			// A change that works proves events flow, and once its event has arrived, the failed one's would have too.
			decode[any](t, a.request(t, http.MethodPatch, "/api/v1/monitors/"+controlID, map[string]any{"interval": 90}, a.asAdmin), http.StatusOK)
			updated.waitForMonitor(t, "Control")
			assert.Equal(t, []string{"Control"}, updated.names(), "no MonitorUpdated for the failed update")
		})
	}
}

func TestMonitorsAPI_UpdatedEventSeesTheMonitorsChannels(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			a := newAppWithAdmin(t)
			channelID := a.createChannel(t, "Ops webhook")
			names := manyNames(30)
			ids := make(map[string]string, len(names))
			for _, name := range names {
				ids[name] = a.createMonitor(t, name, name+"-token")
			}
			links := a.readChannelsOnEvent(t, events.MonitorUpdated)

			for _, name := range names {
				var body any = map[string]any{"notification_ids": []string{channelID}}
				if method == http.MethodPut {
					body = monitorWithLinksBody(name, []string{channelID}, []string{})
				}
				decode[any](t, a.request(t, method, "/api/v1/monitors/"+ids[name], body, a.asAdmin), http.StatusOK)
			}

			require.Eventually(t, func() bool { return links.count() == len(names) }, 5*time.Second, 5*time.Millisecond)
			for _, name := range names {
				assert.Equal(t, []string{channelID}, links.get(name), "the channels a MonitorUpdated listener reads for %s", name)
			}
		})
	}
}

// monitorRows counts the rows that belong to a monitor, by table.
type monitorRows struct {
	Monitors, Heartbeats, Notifications, Tags, Stats int
}

func (a *app) monitorRows(t *testing.T, monitorID string) monitorRows {
	t.Helper()
	count := func(table, column string) int {
		var n int
		require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
			return db.NewSelect().TableExpr(table).ColumnExpr("count(*)").Where(column+" = ?", monitorID).Scan(context.Background(), &n)
		}))
		return n
	}
	return monitorRows{
		Monitors:      count("monitors", "id"),
		Heartbeats:    count("heartbeats", "monitor_id"),
		Notifications: count("monitor_notifications", "monitor_id"),
		Tags:          count("monitor_tags", "monitor_id"),
		Stats:         count("stat_minutely", "monitor_id") + count("stat_hourly", "monitor_id") + count("stat_daily", "monitor_id"),
	}
}

// createMonitorWithHistory creates a monitor linked to a channel and a tag, with a heartbeat and
// the stats it adds up to.
func (a *app) createMonitorWithHistory(t *testing.T, name, channelID, tagID string) string {
	t.Helper()
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", monitorWithLinksBody(name, []string{channelID}, []string{tagID}), a.asAdmin), http.StatusCreated).ID
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service, statService stats.Service) error {
		ctx := context.Background()
		if _, err := heartbeats.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: id, Status: shared.MonitorStatusUp, Ping: 20, Time: time.Now()}); err != nil {
			return err
		}
		return statService.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{MonitorID: id, Status: int(shared.MonitorStatusUp), Ping: 20, Time: time.Now().Unix()})
	}))
	return id
}

func TestMonitorsAPI_DeleteRemovesTheMonitorWithItsLinksHeartbeatsAndStats(t *testing.T) {
	a := newAppWithAdmin(t)
	channelID, tagID := a.createChannel(t, "Ops webhook"), a.createTag(t, "Production")
	id := a.createMonitorWithHistory(t, "Checkout", channelID, tagID)
	kept := a.createMonitorWithHistory(t, "Billing", channelID, tagID)
	keptRows := a.monitorRows(t, kept)
	require.Equal(t, 1, a.monitorRows(t, id).Monitors)
	require.Equal(t, 1, a.monitorRows(t, id).Heartbeats)
	require.Equal(t, 1, a.monitorRows(t, id).Notifications)
	require.Equal(t, 1, a.monitorRows(t, id).Tags)
	require.Equal(t, 3, a.monitorRows(t, id).Stats, "one stat for each of the minute, hour and day")
	deleted := &deletedFeed{}
	require.NoError(t, a.container.Invoke(func(bus events.EventBus) {
		events.Subscribe(events.NewSubscriber(bus, "deleted"), events.MonitorDeleted, deleted.add)
	}))

	decode[any](t, a.request(t, http.MethodDelete, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, monitorRows{}, a.monitorRows(t, id), "the monitor, its links, heartbeats and stats are gone")
	assert.Equal(t, keptRows, a.monitorRows(t, kept), "another monitor keeps everything")
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin).Code)
	require.Eventually(t, func() bool { return slices.Contains(deleted.ids(), id) }, 5*time.Second, 5*time.Millisecond, "MonitorDeleted arrived")
}

// deletedFeed is what a bus subscriber has seen on the MonitorDeleted topic.
type deletedFeed struct {
	mu   sync.Mutex
	seen []string
}

func (f *deletedFeed) add(_ context.Context, monitorID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, monitorID)
}

func (f *deletedFeed) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// failingStats is the stats service with a delete that fails while fail is set.
type failingStats struct {
	stats.Service
	fail *atomic.Bool
}

func (s failingStats) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	if s.fail.Load() {
		return errors.New("stats are locked")
	}
	return s.Service.DeleteByMonitorID(ctx, monitorID)
}

func TestMonitorsAPI_DeleteRemovesNothingWhenACleanupFails(t *testing.T) {
	var fail atomic.Bool
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(s stats.Service) stats.Service { return failingStats{Service: s, fail: &fail} })
	})
	a.registerAdmin(t)
	channelID, tagID := a.createChannel(t, "Ops webhook"), a.createTag(t, "Production")
	id := a.createMonitorWithHistory(t, "Checkout", channelID, tagID)
	control := a.createMonitorWithHistory(t, "Control", channelID, tagID)
	before := a.monitorRows(t, id)
	require.Equal(t, monitorRows{Monitors: 1, Heartbeats: 1, Notifications: 1, Tags: 1, Stats: 3}, before)
	deleted := &deletedFeed{}
	require.NoError(t, a.container.Invoke(func(bus events.EventBus) {
		events.Subscribe(events.NewSubscriber(bus, "deleted"), events.MonitorDeleted, deleted.add)
	}))

	// The stats are the last thing deleted before the monitor, so the links and heartbeats were
	// already deleted when this fails
	fail.Store(true)
	w := a.request(t, http.MethodDelete, "/api/v1/monitors/"+id, nil, a.asAdmin)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Equal(t, before, a.monitorRows(t, id), "nothing was removed")
	assert.Equal(t, id, a.getMonitorWithLinks(t, id).ID)

	// A delete that works proves events flow, and once its event has arrived, the failed one's would have too.
	fail.Store(false)
	decode[any](t, a.request(t, http.MethodDelete, "/api/v1/monitors/"+control, nil, a.asAdmin), http.StatusOK)
	require.Eventually(t, func() bool { return slices.Contains(deleted.ids(), control) }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{control}, deleted.ids(), "no MonitorDeleted for the failed delete")
}
