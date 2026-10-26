package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testInstanceID = "6c1b4f0e-5d3a-4c8e-9f21-0a7b8c9d1e2f"

// lowerUUIDv4 is the instance ID the Worker accepts
var lowerUUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// fakeSettings is the settings table: the service's one door to the stored settings.
type fakeSettings struct {
	mu         sync.Mutex
	values     map[string]*shared.SettingModel
	failDelete error
}

func newFakeSettings() *fakeSettings {
	return &fakeSettings{values: map[string]*shared.SettingModel{}}
}

func (f *fakeSettings) GetByKey(_ context.Context, key string) (*shared.SettingModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.values[key]; ok {
		copied := *m
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeSettings) SetByKey(_ context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = &shared.SettingModel{Key: key, Value: entity.Value, Type: entity.Type}
	copied := *f.values[key]
	return &copied, nil
}

func (f *fakeSettings) CreateIfAbsent(_ context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.values[key]; !ok {
		f.values[key] = &shared.SettingModel{Key: key, Value: entity.Value, Type: entity.Type}
	}
	copied := *f.values[key]
	return &copied, nil
}

func (f *fakeSettings) DeleteByKey(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete != nil {
		return f.failDelete
	}
	delete(f.values, key)
	return nil
}

func (f *fakeSettings) InitializeSettings(context.Context) error { return nil }

func (f *fakeSettings) put(key, value, typ string) {
	_, _ = f.SetByKey(context.Background(), key, &shared.SettingModel{Value: value, Type: typ})
}

// page returns the page-th slice of items, as the repositories' FindAll do.
func page[T any](items []T, pageNo, limit int) []T {
	start := pageNo * limit
	if limit <= 0 || start >= len(items) {
		return nil
	}
	return items[start:min(start+limit, len(items))]
}

type fakeMonitors struct {
	items []*monitor.Model
	err   error
}

func (f *fakeMonitors) FindAll(_ context.Context, pageNo, limit int, _ string, _ *bool, _ *int, _ []string) ([]*monitor.Model, error) {
	return page(f.items, pageNo, limit), f.err
}

type fakeChannels struct{ items []*notification_channel.Model }

func (f *fakeChannels) FindAll(_ context.Context, pageNo, limit int, _ string) ([]*notification_channel.Model, error) {
	return page(f.items, pageNo, limit), nil
}

type fakeStatusPages struct{ items []*status_page.Model }

func (f *fakeStatusPages) FindAll(_ context.Context, pageNo, limit int, _ string) ([]*status_page.Model, error) {
	return page(f.items, pageNo, limit), nil
}

type fakeMaintenances struct{ items []*maintenance.Model }

func (f *fakeMaintenances) FindAll(_ context.Context, pageNo, limit int, _ string, _ string) ([]*maintenance.Model, error) {
	return page(f.items, pageNo, limit), nil
}

// install is the install the service reads: its settings and what its modules hold.
type install struct {
	settings     *fakeSettings
	monitors     *fakeMonitors
	channels     *fakeChannels
	statusPages  *fakeStatusPages
	maintenances *fakeMaintenances
	cfg          *config.Config
	mode         Mode
}

func newInstall() *install {
	return &install{
		settings:     newFakeSettings(),
		monitors:     &fakeMonitors{},
		channels:     &fakeChannels{},
		statusPages:  &fakeStatusPages{},
		maintenances: &fakeMaintenances{},
		cfg:          &config.Config{DBType: "sqlite"},
		mode:         ModeSingle,
	}
}

func (i *install) service() Service {
	return NewService(i.settings, i.monitors, i.channels, i.statusPages, i.maintenances, i.cfg, i.mode)
}

// optIn stores the opt-in the way the service does.
func (i *install) optIn(id string) {
	i.settings.put(EnabledKey, "true", "bool")
	i.settings.put(InstanceIDKey, id, "string")
}

func monitorsOfType(types ...string) []*monitor.Model {
	var monitors []*monitor.Model
	for n, typ := range types {
		monitors = append(monitors, &monitor.Model{ID: fmt.Sprint(n), Type: typ, Name: fmt.Sprintf("monitor %d", n)})
	}
	return monitors
}

func channelsOfType(types ...string) []*notification_channel.Model {
	var channels []*notification_channel.Model
	for n, typ := range types {
		channels = append(channels, &notification_channel.Model{ID: fmt.Sprint(n), Type: typ, Name: fmt.Sprintf("channel %d", n)})
	}
	return channels
}

func TestBuildPayload_BucketsAndTypesOnly(t *testing.T) {
	in := newInstall()
	in.optIn(testInstanceID)
	in.cfg = &config.Config{DBType: "postgresql", MetricsEnabled: true}
	in.mode = ModeCluster
	webhookURL := `{"webhook_url":"https://hooks.example.com/services/T0SECRET/B0TOKEN","name":"Ops on-call"}`
	in.monitors.items = []*monitor.Model{
		{ID: "1", Type: "http", Name: "Prod API", Active: true, Config: `{"url":"https://internal.example.com/health","method":"GET"}`},
		{ID: "2", Type: "postgres", Name: "Billing DB host", Active: true, Config: `{"connection_string":"postgres://admin:hunter2@db.internal:5432/billing"}`},
		{ID: "3", Type: "HTTP", Name: "Paused API", Active: false, Config: `{"url":"https://paused.example.com"}`},
		{ID: "4", Type: "Not A Type!", Name: "Odd one", Active: true},
	}
	in.channels.items = []*notification_channel.Model{
		{ID: "1", Type: "webhook", Name: "Ops on-call", Config: &webhookURL},
		{ID: "2", Type: "slack", Name: "Team Slack", Config: &webhookURL},
		{ID: "3", Type: "Webhook", Name: "Second webhook", Config: &webhookURL},
	}
	in.statusPages.items = []*status_page.Model{{ID: "1", Slug: "acme-status", Title: "Acme Status", Description: "Acme's public page"}}
	in.maintenances.items = []*maintenance.Model{{ID: "1", Title: "Nightly DB upgrade", Description: "Billing is read only"}}

	payload, err := in.service().BuildPayload(context.Background())

	require.NoError(t, err)
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{
		"instance_id": "6c1b4f0e-5d3a-4c8e-9f21-0a7b8c9d1e2f",
		"version": %q,
		"mode": "cluster",
		"db": "postgres",
		"os": %q,
		"monitors": "1-10",
		"monitor_types": ["http", "postgres"],
		"notification_providers": ["slack", "webhook"],
		"features": {"status_pages": true, "maintenance": true, "metrics": true}
	}`, version.Version, runtime.GOOS+"/"+runtime.GOARCH), string(encoded))
	for _, private := range []string{
		"internal.example.com", "paused.example.com", "hooks.example.com", "T0SECRET", "hunter2", "db.internal",
		"Prod API", "Billing DB host", "Paused API", "Odd one", "Ops on-call", "Team Slack", "Second webhook",
		"acme-status", "Acme Status", "Acme's public page", "Nightly DB upgrade", "Billing is read only",
	} {
		assert.NotContains(t, string(encoded), private)
	}
}

func TestBuildPayload_MonitorBuckets(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  string
	}{
		{0, "0"},
		{1, "1-10"},
		{10, "1-10"},
		{11, "11-50"},
		{50, "11-50"},
		{51, "51-200"},
		{200, "51-200"},
		{201, "201-1000"},
		{1000, "201-1000"},
		{1001, "1000+"},
	} {
		t.Run(fmt.Sprintf("%d monitors", tc.count), func(t *testing.T) {
			in := newInstall()
			for n := 0; n < tc.count; n++ {
				in.monitors.items = append(in.monitors.items, &monitor.Model{ID: fmt.Sprint(n), Type: "http", Active: n%2 == 0})
			}

			payload, err := in.service().BuildPayload(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tc.want, payload.Monitors)
		})
	}
}

func TestBuildPayload_NothingConfiguredSendsEmptyListsAndFalseFeatures(t *testing.T) {
	in := newInstall()
	// Opted out, so that the payload has no random instance ID to compare
	in.settings.put(EnabledKey, "false", "bool")

	payload, err := in.service().BuildPayload(context.Background())

	require.NoError(t, err)
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{
		"instance_id": "",
		"version": %q,
		"mode": "single",
		"db": "sqlite",
		"os": %q,
		"monitors": "0",
		"monitor_types": [],
		"notification_providers": [],
		"features": {"status_pages": false, "maintenance": false, "metrics": false}
	}`, version.Version, runtime.GOOS+"/"+runtime.GOARCH), string(encoded))
}

func TestBuildPayload_InstanceIDIsSentUnlessTheAdminOptedOut(t *testing.T) {
	in := newInstall()
	in.settings.put(InstanceIDKey, testInstanceID, "string")

	payload, err := in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, testInstanceID, payload.InstanceID, "the ping is on by default, so the ID is sent without a stored choice")

	in.settings.put(EnabledKey, "false", "bool")
	payload, err = in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", payload.InstanceID, "an ID left behind is not sent after an opt-out")

	in.settings.put(EnabledKey, "true", "bool")
	payload, err = in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, testInstanceID, payload.InstanceID)
}

func TestBuildPayload_StorageBackendIsNormalized(t *testing.T) {
	for dbType, want := range map[string]string{
		"sqlite":     "sqlite",
		"SQLite":     "sqlite",
		"postgres":   "postgres",
		"postgresql": "postgres",
		"mysql":      "mysql",
		"MySQL":      "mysql",
		"mariadb":    "",
		"":           "",
	} {
		t.Run(dbType, func(t *testing.T) {
			in := newInstall()
			in.cfg = &config.Config{DBType: dbType}

			payload, err := in.service().BuildPayload(context.Background())

			require.NoError(t, err)
			assert.Equal(t, want, payload.DB)
		})
	}
}

func TestBuildPayload_ModeIsTheOneRegistrationGave(t *testing.T) {
	for _, mode := range []Mode{ModeSingle, ModeCluster} {
		t.Run(string(mode), func(t *testing.T) {
			in := newInstall()
			in.mode = mode
			in.cfg = &config.Config{DBType: "sqlite", Mode: "prod"}

			payload, err := in.service().BuildPayload(context.Background())

			require.NoError(t, err)
			assert.Equal(t, string(mode), payload.Mode, "MODE is dev/prod/test and has nothing to do with it")
		})
	}
}

func TestBuildPayload_TypeListsAreDistinctSortedNamedAndCapped(t *testing.T) {
	in := newInstall()
	in.monitors.items = monitorsOfType("tcp", "http", "http", "TCP", "grpc-keyword", "under_score", "9lives",
		"", "has space", "slash/ed", "dot.ted", "unicodé", "thirty-three-characters-long-name1")
	in.channels.items = channelsOfType("telegram", "Slack", "slack", "x", "no way", "thirty-two-characters-long-name1")

	payload, err := in.service().BuildPayload(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"9lives", "grpc-keyword", "http", "tcp", "under_score"}, payload.MonitorTypes)
	assert.Equal(t, []string{"slack", "telegram", "thirty-two-characters-long-name1", "x"}, payload.NotificationChannelTypes)
}

func TestBuildPayload_TypeListsStopAtFifty(t *testing.T) {
	in := newInstall()
	var types []string
	for n := 0; n < 60; n++ {
		types = append(types, fmt.Sprintf("type-%02d", n))
	}
	in.monitors.items = monitorsOfType(types...)
	in.channels.items = channelsOfType(types...)

	payload, err := in.service().BuildPayload(context.Background())

	require.NoError(t, err)
	require.Len(t, payload.MonitorTypes, 50)
	assert.Equal(t, "type-00", payload.MonitorTypes[0])
	assert.Equal(t, "type-49", payload.MonitorTypes[49])
	require.Len(t, payload.NotificationChannelTypes, 50)
	assert.Equal(t, "type-49", payload.NotificationChannelTypes[49])
}

func TestBuildPayload_ReadsEveryMonitorAcrossPages(t *testing.T) {
	in := newInstall()
	// The last monitor, far past any single page, is the only one of its type.
	in.monitors.items = monitorsOfType(make([]string, 2500)...)
	for _, m := range in.monitors.items {
		m.Type = "http"
	}
	in.monitors.items[2499].Type = "tcp"
	in.channels.items = channelsOfType(make([]string, 1300)...)
	for _, c := range in.channels.items {
		c.Type = "slack"
	}
	in.channels.items[1299].Type = "ntfy"

	payload, err := in.service().BuildPayload(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "1000+", payload.Monitors)
	assert.Equal(t, []string{"http", "tcp"}, payload.MonitorTypes)
	assert.Equal(t, []string{"ntfy", "slack"}, payload.NotificationChannelTypes)
}

func TestBuildPayload_FeaturesFollowWhatExists(t *testing.T) {
	in := newInstall()
	in.statusPages.items = []*status_page.Model{{ID: "1"}}

	payload, err := in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Features{StatusPages: true}, payload.Features)

	in = newInstall()
	in.maintenances.items = []*maintenance.Model{{ID: "1", Active: false}}
	payload, err = in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Features{Maintenance: true}, payload.Features, "a maintenance window counts whether or not it is active")

	in = newInstall()
	in.cfg = &config.Config{DBType: "sqlite", MetricsEnabled: true}
	payload, err = in.service().BuildPayload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Features{Metrics: true}, payload.Features)
}

func TestBuildPayload_FailsWhenAnInstallsDataCannotBeRead(t *testing.T) {
	in := newInstall()
	in.monitors.err = errors.New("database is locked")

	_, err := in.service().BuildPayload(context.Background())

	assert.ErrorContains(t, err, "database is locked")
}

func TestState_IsOnByDefaultAndHasAnInstanceID(t *testing.T) {
	in := newInstall()

	state, err := in.service().State(context.Background())

	require.NoError(t, err)
	assert.True(t, state.Enabled, "no stored choice means on")
	assert.False(t, state.ForcedOff)
	assert.Regexp(t, lowerUUIDv4, state.Payload.InstanceID)
	stored, err := in.settings.GetByKey(context.Background(), InstanceIDKey)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, state.Payload.InstanceID, stored.Value)
	assert.Equal(t, "string", stored.Type)
	choice, err := in.settings.GetByKey(context.Background(), EnabledKey)
	require.NoError(t, err)
	assert.Nil(t, choice, "the default is not stored as the admin's choice")
}

func TestInstanceID_IsCreatedOnceForAnInstallWithNoChoice(t *testing.T) {
	in := newInstall()
	ctx := context.Background()

	state, err := in.service().State(ctx)
	require.NoError(t, err)
	first := state.Payload.InstanceID
	require.Regexp(t, lowerUUIDv4, first)

	// Each reader below is a fresh service over the same settings, as after a restart
	built, err := in.service().BuildPayload(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, built.InstanceID)
	due, err := in.service().NextPing(ctx, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, due, "the first ping of an install that never chose is due at once")
	assert.Equal(t, first, due.InstanceID)
	state, err = in.service().State(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, state.Payload.InstanceID)
}

// Several api replicas of a cluster start together on an install with no stored choice and no ID.
// They must not send different IDs.
func TestInstanceID_ReplicasStartingTogetherEndWithTheSameID(t *testing.T) {
	in := newInstall()
	const replicas = 16
	ids := make([]string, replicas)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for n := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			state, err := in.service().State(context.Background())
			if assert.NoError(t, err) {
				ids[n] = state.Payload.InstanceID
			}
		}()
	}
	close(start)
	wg.Wait()

	stored, err := in.settings.GetByKey(context.Background(), InstanceIDKey)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Regexp(t, lowerUUIDv4, stored.Value)
	for n, id := range ids {
		assert.Equal(t, stored.Value, id, "replica %d sends another ID than the stored one", n)
	}
}

func TestSetEnabled_AnExplicitOptOutIsStoredAndSurvivesARestart(t *testing.T) {
	in := newInstall()
	ctx := context.Background()
	state, err := in.service().State(ctx)
	require.NoError(t, err)
	firstID := state.Payload.InstanceID
	require.NotEmpty(t, firstID)

	state, err = in.service().SetEnabled(ctx, false)

	require.NoError(t, err)
	assert.False(t, state.Enabled)
	assert.Equal(t, "", state.Payload.InstanceID)
	choice, err := in.settings.GetByKey(ctx, EnabledKey)
	require.NoError(t, err)
	require.NotNil(t, choice)
	assert.Equal(t, "false", choice.Value)
	id, err := in.settings.GetByKey(ctx, InstanceIDKey)
	require.NoError(t, err)
	assert.Nil(t, id, "opting out deletes the ID")

	// The restart: a new service over the same settings reads the choice, and creates nothing
	restarted := in.service()
	state, err = restarted.State(ctx)
	require.NoError(t, err)
	assert.False(t, state.Enabled)
	assert.Equal(t, "", state.Payload.InstanceID)
	due, err := restarted.NextPing(ctx, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Nil(t, due, "no ping after an opt-out")
	id, err = in.settings.GetByKey(ctx, InstanceIDKey)
	require.NoError(t, err)
	assert.Nil(t, id, "and no ID is made again")

	// Opting back in makes a new ID
	state, err = restarted.SetEnabled(ctx, true)
	require.NoError(t, err)
	assert.True(t, state.Enabled)
	assert.Regexp(t, lowerUUIDv4, state.Payload.InstanceID)
	assert.NotEqual(t, firstID, state.Payload.InstanceID)
}

func TestKillSwitchesWinWhenNothingIsStored(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"TELEMETRY=off": {DBType: "sqlite", Telemetry: "off"},
	} {
		t.Run(name, func(t *testing.T) {
			in := newInstall()
			in.cfg = cfg
			ctx := context.Background()

			state, err := in.service().State(ctx)
			require.NoError(t, err)
			assert.True(t, state.Enabled, "the stored choice is the default")
			assert.True(t, state.ForcedOff)
			assert.Equal(t, "", state.Payload.InstanceID, "no identifier is made while the environment forbids the ping")
			due, err := in.service().NextPing(ctx, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			assert.Nil(t, due)
			assert.Empty(t, in.settings.values, "and nothing is stored")
		})
	}
}

// A choice that failed half way must not turn the ping back on: once the admin has opted out the
// choice is stored first, so an ID that is made later for the default can't send the ping the
// admin refused.
func TestSetEnabled_AFailedOptOutNeverLeavesThePingOn(t *testing.T) {
	in := newInstall()
	ctx := context.Background()
	_, err := in.service().State(ctx)
	require.NoError(t, err)
	in.settings.failDelete = errors.New("database is locked")

	_, err = in.service().SetEnabled(ctx, false)

	require.Error(t, err)
	state, err := in.service().State(ctx)
	require.NoError(t, err)
	assert.False(t, state.Enabled, "the choice was stored before the ID was deleted")
	assert.Equal(t, "", state.Payload.InstanceID)
	due, err := in.service().NextPing(ctx, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Nil(t, due)
}

func TestState_TheNoticeIsPendingUntilItIsMarkedSeen(t *testing.T) {
	in := newInstall()
	ctx := context.Background()

	state, err := in.service().State(ctx)
	require.NoError(t, err)
	assert.True(t, state.NoticePending, "an install that upgrades starts sending and has not been told")

	state, err = in.service().MarkNoticeSeen(ctx)
	require.NoError(t, err)
	assert.False(t, state.NoticePending)
	assert.True(t, state.Enabled, "dismissing the notice is not a choice")
	seen, err := in.settings.GetByKey(ctx, shared.TelemetryNoticeSeenKey)
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.Equal(t, "TELEMETRY_NOTICE_SEEN", shared.TelemetryNoticeSeenKey)
	assert.Equal(t, "true", seen.Value)
	choice, err := in.settings.GetByKey(ctx, EnabledKey)
	require.NoError(t, err)
	assert.Nil(t, choice)

	state, err = in.service().State(ctx)
	require.NoError(t, err)
	assert.False(t, state.NoticePending, "and a restart does not bring it back")
}

func TestSetEnabled_MarksTheNoticeSeen(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			in := newInstall()

			state, err := in.service().SetEnabled(context.Background(), enabled)

			require.NoError(t, err)
			assert.False(t, state.NoticePending)
			seen, err := in.settings.GetByKey(context.Background(), shared.TelemetryNoticeSeenKey)
			require.NoError(t, err)
			require.NotNil(t, seen)
		})
	}
}

func TestState_NoNoticeWhenTheEnvironmentForcesBothFeaturesOff(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg     *config.Config
		pending bool
	}{
		"nothing forced off":     {&config.Config{}, true},
		"only the ping":          {&config.Config{Telemetry: "off"}, true},
		"only the error reports": {&config.Config{ErrorReporting: "off"}, true},
		"both":                   {&config.Config{Telemetry: "off", ErrorReporting: "off"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			in := newInstall()
			in.cfg = tc.cfg

			state, err := in.service().State(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tc.pending, state.NoticePending)
		})
	}
}
