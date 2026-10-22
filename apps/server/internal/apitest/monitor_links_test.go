package apitest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMonitorsAPI_CreatedEventSeesTheMonitorsChannels(t *testing.T) {
	a := newAppWithAdmin(t)
	channelID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("Ops webhook"), a.asAdmin), http.StatusCreated).ID

	// What a listener that reads the new monitor's channels finds, by monitor name.
	var links monitorLinks
	var channels monitor_notification.Service
	require.NoError(t, a.container.Invoke(func(s monitor_notification.Service) { channels = s }))
	a.subscribeMonitors(t, "channels", events.MonitorCreated, func(ctx context.Context, m shared.Monitor) {
		rels, err := channels.FindByMonitorID(ctx, m.ID)
		assert.NoError(t, err)
		ids := make([]string, 0, len(rels))
		for _, rel := range rels {
			ids = append(ids, rel.NotificationID)
		}
		links.set(m.Name, ids)
	})

	// A listener that wins the race against one create is luck, so create a few dozen.
	names := make([]string, 30)
	for i := range names {
		names[i] = fmt.Sprintf("Monitor %d", i+1)
	}
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
