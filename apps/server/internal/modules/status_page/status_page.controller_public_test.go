package status_page

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// publicPageService is the status page service as publicMonitors reads it. Any other method panics.
type publicPageService struct {
	Service
	page        *Model
	pageErr     error
	links       []*monitor_status_page.Model
	linksErr    error
	askedSlug   string
	askedPageID string
}

func (s *publicPageService) FindPublishedBySlugAnyWorkspace(_ context.Context, slug string) (*Model, error) {
	s.askedSlug = slug
	return s.page, s.pageErr
}

func (s *publicPageService) GetMonitorsForStatusPage(_ context.Context, statusPageID string) ([]*monitor_status_page.Model, error) {
	s.askedPageID = statusPageID
	return s.links, s.linksErr
}

// publicMonitorService is locationsService that also finds the monitors
type publicMonitorService struct {
	locationsService
	monitors   map[string]*shared.Monitor
	monitorErr map[string]error
	found      []string
}

func (s *publicMonitorService) FindByIDAnyWorkspace(_ context.Context, id string) (*shared.Monitor, error) {
	s.found = append(s.found, id)
	if err := s.monitorErr[id]; err != nil {
		return nil, err
	}
	return s.monitors[id], nil
}

// publicHeartbeatService is the heartbeat service as publicMonitors reads it
type publicHeartbeatService struct {
	heartbeat.Service
	beats    map[string][]*heartbeat.Model
	beatsErr map[string]error
	uptimes  map[string]map[string]*float64
	// uptimesErr is the monitors whose uptime can't be read
	uptimesErr map[string]error
	limits     []int
	periods    []map[string]time.Duration
	at         []time.Time
}

func (s *publicHeartbeatService) FindByMonitorIDPaginated(_ context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	s.limits = append(s.limits, limit)
	if page != 0 || important != nil || !reverse {
		return nil, errors.New("heartbeats are read newest first, from the first page, important or not")
	}
	return s.beats[monitorID], s.beatsErr[monitorID]
}

func (s *publicHeartbeatService) FindUptimeStatsByMonitorID(_ context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]*float64, error) {
	s.periods = append(s.periods, periods)
	s.at = append(s.at, now)
	if err := s.uptimesErr[monitorID]; err != nil {
		return nil, err
	}
	return s.uptimes[monitorID], nil
}

type publicFixture struct {
	pages      *publicPageService
	monitors   *publicMonitorService
	heartbeats *publicHeartbeatService
	logs       *observer.ObservedLogs
	controller *Controller
}

func newPublicFixture() *publicFixture {
	f := &publicFixture{
		pages:      &publicPageService{page: &Model{ID: "page-1", Slug: "shop", Published: true}},
		monitors:   &publicMonitorService{monitors: map[string]*shared.Monitor{}, monitorErr: map[string]error{}},
		heartbeats: &publicHeartbeatService{beats: map[string][]*heartbeat.Model{}, beatsErr: map[string]error{}, uptimes: map[string]map[string]*float64{}, uptimesErr: map[string]error{}},
	}
	core, logs := observer.New(zap.ErrorLevel)
	f.logs = logs
	f.controller = &Controller{service: f.pages, monitorService: f.monitors, heartbeatService: f.heartbeats, logger: zap.New(core).Sugar()}
	return f
}

// link puts a monitor on the page, in the order the calls make them
func (f *publicFixture) link(monitor *shared.Monitor) {
	f.pages.links = append(f.pages.links, &monitor_status_page.Model{MonitorID: monitor.ID})
	f.monitors.monitors[monitor.ID] = monitor
}

func (f *publicFixture) errorsLogged() []string {
	var got []string
	for _, entry := range f.logs.All() {
		got = append(got, entry.Message)
	}
	return got
}

func floatPtr(v float64) *float64 { return &v }

func failure(t *testing.T, err error) *httpx.ResponseError {
	t.Helper()
	var response *httpx.ResponseError
	require.ErrorAs(t, err, &response)
	return response
}

func TestPublicMonitors_AnswersForAPageThatIsNotThere(t *testing.T) {
	t.Run("a workspace that is suspended", func(t *testing.T) {
		f := newPublicFixture()
		f.pages.page, f.pages.pageErr = nil, ErrUnavailable

		got, err := f.controller.publicMonitors(context.Background(), "shop", 100)

		assert.Nil(t, got)
		assert.Equal(t, Unavailable(), err)
		assert.Empty(t, f.errorsLogged())
	})

	t.Run("a page that can't be read", func(t *testing.T) {
		f := newPublicFixture()
		f.pages.page, f.pages.pageErr = nil, errors.New("db down")

		got, err := f.controller.publicMonitors(context.Background(), "shop", 100)

		assert.Nil(t, got)
		response := failure(t, err)
		assert.Equal(t, http.StatusInternalServerError, response.Status)
		assert.Equal(t, utils.NewFailResponse("Internal server error"), response.Body)
		assert.Equal(t, []string{"Failed to get status page by slug"}, f.errorsLogged())
	})

	t.Run("a page that isn't published or doesn't exist", func(t *testing.T) {
		f := newPublicFixture()
		f.pages.page = nil

		got, err := f.controller.publicMonitors(context.Background(), "shop", 100)

		assert.Nil(t, got)
		response := failure(t, err)
		assert.Equal(t, http.StatusNotFound, response.Status)
		assert.Equal(t, utils.NewFailResponse("Status page not found"), response.Body)
		assert.Empty(t, f.pages.askedPageID, "its monitors are not read")
	})

	t.Run("monitors that can't be read", func(t *testing.T) {
		f := newPublicFixture()
		f.pages.linksErr = errors.New("db down")

		got, err := f.controller.publicMonitors(context.Background(), "shop", 100)

		assert.Nil(t, got)
		response := failure(t, err)
		assert.Equal(t, http.StatusInternalServerError, response.Status)
		assert.Equal(t, utils.NewFailResponse("Internal server error"), response.Body)
		assert.Equal(t, []string{"Failed to get monitors for status page"}, f.errorsLogged())
		assert.Equal(t, "page-1", f.pages.askedPageID)
	})

	t.Run("no monitors is an empty list", func(t *testing.T) {
		f := newPublicFixture()

		got, err := f.controller.publicMonitors(context.Background(), "shop", 100)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, got.Status)
		assert.Equal(t, utils.NewSuccessResponse("success", []*MonitorWithHeartbeatsAndUptimeDTO{}), got.Body)
		assert.Equal(t, "shop", f.pages.askedSlug)
	})
}

func TestPublicMonitors_ListsEachMonitorWithItsHeartbeatsAndUptime(t *testing.T) {
	f := newPublicFixture()
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f.link(&shared.Monitor{ID: "a", Type: "http", Name: "Checkout", Active: true, Status: shared.MonitorStatusUp})
	f.link(&shared.Monitor{ID: "b", Type: "tcp", Name: "Database", Active: true, TypeDisabled: true, Status: shared.MonitorStatusDown})
	f.link(&shared.Monitor{ID: "c", Type: "ping", Name: "Paused", Active: false, Status: shared.MonitorStatusPending})
	f.heartbeats.beats["a"] = []*heartbeat.Model{{ID: "hb1", Status: shared.MonitorStatusUp, Time: start, EndTime: start.Add(time.Second), Ping: 12, Msg: "private"}}
	f.heartbeats.uptimes["a"] = map[string]*float64{"24h": floatPtr(99.5)}
	f.heartbeats.uptimes["b"] = nil
	f.heartbeats.uptimes["c"] = map[string]*float64{"30d": floatPtr(1)}

	got, err := f.controller.publicMonitors(context.Background(), "shop", 7)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, got.Status)
	assert.Equal(t, utils.NewSuccessResponse("success", []*MonitorWithHeartbeatsAndUptimeDTO{
		{
			PublicMonitorDTO: &PublicMonitorDTO{ID: "a", Type: "http", Name: "Checkout", Active: true, Status: shared.MonitorStatusUp},
			Heartbeats:       []*PublicHeartbeatDTO{{ID: "hb1", Status: shared.MonitorStatusUp, Time: start, EndTime: start.Add(time.Second), Ping: 12}},
			Uptime24h:        floatPtr(99.5),
		},
		{
			PublicMonitorDTO: &PublicMonitorDTO{ID: "b", Type: "tcp", Name: "Database", Active: false, Status: shared.MonitorStatusDown},
			Heartbeats:       []*PublicHeartbeatDTO{},
		},
		{
			PublicMonitorDTO: &PublicMonitorDTO{ID: "c", Type: "ping", Name: "Paused", Active: false, Status: shared.MonitorStatusPending},
			Heartbeats:       []*PublicHeartbeatDTO{},
		},
	}), got.Body)
	assert.Equal(t, []string{"a", "b", "c"}, f.monitors.found)
	assert.Equal(t, []int{7, 7, 7}, f.heartbeats.limits, "as many heartbeats as asked for")
	assert.Equal(t, []map[string]time.Duration{{"24h": 24 * time.Hour}, {"24h": 24 * time.Hour}, {"24h": 24 * time.Hour}}, f.heartbeats.periods)
	for _, at := range f.heartbeats.at {
		assert.WithinDuration(t, time.Now(), at, time.Minute)
		assert.Equal(t, time.UTC, at.Location())
	}
	assert.Empty(t, f.errorsLogged())
}

func TestPublicMonitors_SkipsAMonitorItCannotFind(t *testing.T) {
	f := newPublicFixture()
	f.link(&shared.Monitor{ID: "unreadable", Name: "Unreadable", Active: true})
	f.link(&shared.Monitor{ID: "deleted", Name: "Deleted", Active: true})
	f.link(&shared.Monitor{ID: "fine", Name: "Fine", Active: true})
	f.monitors.monitorErr["unreadable"] = errors.New("db down")
	delete(f.monitors.monitors, "deleted")

	got, err := f.controller.publicMonitors(context.Background(), "shop", 1)

	require.NoError(t, err)
	require.Len(t, got.Body.Data, 1)
	assert.Equal(t, "fine", got.Body.Data[0].ID)
	assert.Equal(t, []string{"Failed to get monitor by ID"}, f.errorsLogged())
	assert.Equal(t, "unreadable", f.logs.All()[0].ContextMap()["monitorID"])
	assert.Equal(t, []int{1}, f.heartbeats.limits, "nothing is read of a monitor that isn't found")
}

func TestPublicMonitors_ListsAMonitorWhoseHeartbeatsCantBeReadWithNone(t *testing.T) {
	f := newPublicFixture()
	f.link(&shared.Monitor{ID: "a", Name: "A", Active: true})
	f.link(&shared.Monitor{ID: "b", Name: "B", Active: true})
	f.heartbeats.beatsErr["a"] = errors.New("db down")
	f.heartbeats.beats["a"] = []*heartbeat.Model{{ID: "ignored"}}
	f.heartbeats.beats["b"] = []*heartbeat.Model{{ID: "hb"}}
	f.heartbeats.uptimes["a"] = map[string]*float64{"24h": floatPtr(50)}

	got, err := f.controller.publicMonitors(context.Background(), "shop", 1)

	require.NoError(t, err)
	require.Len(t, got.Body.Data, 2)
	assert.NotNil(t, got.Body.Data[0].Heartbeats)
	assert.Empty(t, got.Body.Data[0].Heartbeats)
	assert.Equal(t, floatPtr(50), got.Body.Data[0].Uptime24h, "its uptime is still read")
	assert.Len(t, got.Body.Data[1].Heartbeats, 1)
	assert.Equal(t, []string{"Failed to get heartbeats for monitor"}, f.errorsLogged())
}

func TestPublicMonitors_FailsWhenAMonitorsUptimeCantBeRead(t *testing.T) {
	f := newPublicFixture()
	f.link(&shared.Monitor{ID: "a", Name: "A", Active: true})
	f.link(&shared.Monitor{ID: "b", Name: "B", Active: true})
	f.heartbeats.uptimesErr["a"] = errors.New("db down")

	got, err := f.controller.publicMonitors(context.Background(), "shop", 1)

	assert.Nil(t, got)
	response := failure(t, err)
	assert.Equal(t, http.StatusInternalServerError, response.Status)
	assert.Equal(t, utils.NewFailResponse("failed to get uptime stats for monitor"), response.Body)
	assert.Equal(t, []string{"Failed to get uptime stats for monitor"}, f.errorsLogged())
	assert.Equal(t, []string{"a"}, f.monitors.found, "the rest are not read")
}

func TestPublicMonitors_ListsLocationsOnlyWhenThePageShowsThemAndThereAreSeveral(t *testing.T) {
	up := shared.MonitorStatusUp
	two := []shared.MonitorLocation{
		{ProbeID: shared.LocalProbeID, ProbeName: "Local", Status: &up},
		{ProbeID: "fra", ProbeName: "Frankfurt"},
	}
	for _, tt := range []struct {
		name          string
		showLocations bool
		locations     []shared.MonitorLocation
		want          []string
	}{
		{"shown, several", true, two, []string{"Local", "Frankfurt"}},
		{"hidden, several", false, two, nil},
		{"shown, one", true, two[:1], nil},
		{"shown, none", true, nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicFixture()
			f.pages.page.ShowLocations = tt.showLocations
			f.link(&shared.Monitor{ID: "a", Name: "A", Active: true, Locations: tt.locations})
			f.monitors.uptimes = map[string]float64{shared.LocalProbeID: 100}

			got, err := f.controller.publicMonitors(context.Background(), "shop", 5)

			require.NoError(t, err)
			require.Len(t, got.Body.Data, 1)
			var names []string
			for _, location := range got.Body.Data[0].Locations {
				names = append(names, location.Name)
			}
			assert.Equal(t, tt.want, names)
		})
	}
}
