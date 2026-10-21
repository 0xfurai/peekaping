package error_reporting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSettings is the settings table: the service's one door to the stored settings.
type fakeSettings struct {
	mu      sync.Mutex
	values  map[string]*shared.SettingModel
	readErr error
}

func newFakeSettings() *fakeSettings {
	return &fakeSettings{values: map[string]*shared.SettingModel{}}
}

func (f *fakeSettings) GetByKey(_ context.Context, key string) (*shared.SettingModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
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

func (f *fakeSettings) DeleteByKey(context.Context, string) error { return nil }
func (f *fakeSettings) InitializeSettings(context.Context) error  { return nil }

func (f *fakeSettings) value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.values[key]; ok {
		return m.Value
	}
	return ""
}

// recordingSwitch keeps what the service tells the processes
type recordingSwitch struct {
	set []bool
	err error
}

func (s *recordingSwitch) Set(_ context.Context, on bool) error {
	s.set = append(s.set, on)
	return s.err
}

func newService(cfg *config.Config) (Service, *fakeSettings, *recordingSwitch) {
	settings := newFakeSettings()
	sw := &recordingSwitch{}
	return NewService(settings, cfg, sw), settings, sw
}

func TestState_IsOffByDefault(t *testing.T) {
	service, settings, sw := newService(&config.Config{})

	state, err := service.State(context.Background())

	require.NoError(t, err)
	assert.Equal(t, State{Enabled: false, ForcedOff: false}, state)
	assert.Empty(t, settings.values, "reading the state stores nothing")
	assert.Empty(t, sw.set, "and tells no process anything")
}

func TestSetEnabled_StoresTheChoiceAndTellsTheProcesses(t *testing.T) {
	service, settings, sw := newService(&config.Config{})

	state, err := service.SetEnabled(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, State{Enabled: true}, state)
	assert.Equal(t, "true", settings.value(EnabledKey))
	assert.Equal(t, "ERROR_REPORTING_ENABLED", EnabledKey)
	assert.Equal(t, "bool", settings.values[EnabledKey].Type)
	assert.Equal(t, []bool{true}, sw.set)

	state, err = service.State(context.Background())
	require.NoError(t, err)
	assert.True(t, state.Enabled, "the choice is kept")

	state, err = service.SetEnabled(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, State{Enabled: false}, state)
	assert.Equal(t, "false", settings.value(EnabledKey))
	assert.Equal(t, []bool{true, false}, sw.set)
}

func TestSetEnabled_KeepsTheChoiceWhileTheEnvironmentForcesReportingOff(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"ERROR_REPORTING=off": {ErrorReporting: "off"},
		"DO_NOT_TRACK=1":      {DoNotTrack: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			service, settings, sw := newService(cfg)

			state, err := service.SetEnabled(context.Background(), true)

			require.NoError(t, err)
			assert.Equal(t, State{Enabled: true, ForcedOff: true}, state, "the choice is stored, and the environment still wins")
			assert.Equal(t, "true", settings.value(EnabledKey))
			assert.Equal(t, []bool{false}, sw.set, "the processes are told it is off")
		})
	}
}

func TestSetEnabled_TheOtherSwitchesDoNotForceItOff(t *testing.T) {
	service, _, _ := newService(&config.Config{Telemetry: "off"})

	state, err := service.State(context.Background())

	require.NoError(t, err)
	assert.False(t, state.ForcedOff, "TELEMETRY=off is the usage ping's switch")
}

func TestSetEnabled_StoresTheChoiceEvenWhenTheProcessesCannotBeTold(t *testing.T) {
	service, settings, sw := newService(&config.Config{})
	sw.err = errors.New("redis: connection refused")

	_, err := service.SetEnabled(context.Background(), true)

	require.ErrorContains(t, err, "connection refused")
	assert.Equal(t, "true", settings.value(EnabledKey), "the next start or the next change tells them")
}

func TestState_FailsWhenTheSettingCannotBeRead(t *testing.T) {
	service, settings, _ := newService(&config.Config{})
	settings.readErr = errors.New("database is locked")

	_, err := service.State(context.Background())
	assert.ErrorContains(t, err, "database is locked")
	_, err = service.SetEnabled(context.Background(), true)
	assert.NoError(t, err, "a change doesn't read the old choice")
	assert.Error(t, service.Apply(context.Background()))
}

func TestApply_TellsTheProcessesTheStoredChoiceAtStart(t *testing.T) {
	for name, tc := range map[string]struct {
		stored string
		cfg    *config.Config
		want   bool
	}{
		"nothing stored":                  {"", &config.Config{}, false},
		"opted in":                        {"true", &config.Config{}, true},
		"opted out":                       {"false", &config.Config{}, false},
		"opted in, forced off":            {"true", &config.Config{ErrorReporting: "off"}, false},
		"opted in, DO_NOT_TRACK":          {"true", &config.Config{DoNotTrack: "1"}, false},
		"opted in, the usage ping is off": {"true", &config.Config{Telemetry: "off"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			service, settings, sw := newService(tc.cfg)
			if tc.stored != "" {
				settings.values[EnabledKey] = &shared.SettingModel{Key: EnabledKey, Value: tc.stored, Type: "bool"}
			}

			require.NoError(t, service.Apply(context.Background()))

			assert.Equal(t, []bool{tc.want}, sw.set)
		})
	}
}

func TestLocalSwitch_TurnsReportingOnAndOffInThisProcess(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	sw := LocalSwitch{}

	require.NoError(t, sw.Set(context.Background(), true))
	assert.True(t, reporting.Enabled())
	require.NoError(t, sw.Set(context.Background(), false))
	assert.False(t, reporting.Enabled())
}

func newRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

// AC6: a change in Settings reaches every process of the cluster without a restart
func TestRedisSwitch_AClusterProcessFollowsTheApisChoiceWithinOneRefresh(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	server, client := newRedis(t)
	service := NewService(newFakeSettings(), &config.Config{}, NewRedisSwitch(client))
	// The producer, the worker and the ingester are other processes with a refresher of their own
	otherProcess := redissync.NewRefresher(client, time.Minute)

	_, err := service.SetEnabled(context.Background(), true)
	require.NoError(t, err)
	value, err := server.Get(redissync.Key)
	require.NoError(t, err)
	assert.Equal(t, "true", value, "the api writes the choice on a change")
	assert.True(t, reporting.Enabled(), "and follows it itself at once")

	reporting.SetEnabled(false) // the other process, which has not read the key yet
	otherProcess.Refresh(context.Background())
	assert.True(t, reporting.Enabled(), "the other process turns reporting on by reading the key")

	_, err = service.SetEnabled(context.Background(), false)
	require.NoError(t, err)
	reporting.SetEnabled(true)
	otherProcess.Refresh(context.Background())
	assert.False(t, reporting.Enabled(), "and off again")
}

func TestRedisSwitch_APeerThatStartsLaterReadsTheChoiceTheApiWroteAtItsStart(t *testing.T) {
	reportingtest.Install(t)
	server, client := newRedis(t)
	settings := newFakeSettings()
	settings.values[EnabledKey] = &shared.SettingModel{Key: EnabledKey, Value: "true", Type: "bool"}
	service := NewService(settings, &config.Config{}, NewRedisSwitch(client))

	require.NoError(t, service.Apply(context.Background()))

	value, err := server.Get(redissync.Key)
	require.NoError(t, err)
	assert.Equal(t, "true", value)
}

func TestRedisSwitch_ForcedOffWritesFalseSoNoProcessSendsWhatTheApiMustNot(t *testing.T) {
	reportingtest.Install(t)
	server, client := newRedis(t)
	service := NewService(newFakeSettings(), &config.Config{DoNotTrack: "1"}, NewRedisSwitch(client))

	_, err := service.SetEnabled(context.Background(), true)
	require.NoError(t, err)

	value, err := server.Get(redissync.Key)
	require.NoError(t, err)
	assert.Equal(t, "false", value)
}

func TestRedisSwitch_ChangesThisProcessEvenWhenRedisIsDown(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	server, client := newRedis(t)
	server.Close()

	err := NewRedisSwitch(client).Set(context.Background(), true)

	assert.Error(t, err)
	assert.True(t, reporting.Enabled())
}
