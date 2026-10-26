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
	"peekaping/internal/reporting/reportingtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

func (f *fakeSettings) CreateIfAbsent(_ context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.values[key]; !ok {
		f.values[key] = &shared.SettingModel{Key: key, Value: entity.Value, Type: entity.Type}
	}
	copied := *f.values[key]
	return &copied, nil
}

func (f *fakeSettings) DeleteByKey(context.Context, string) error { return nil }
func (f *fakeSettings) InitializeSettings(context.Context) error  { return nil }

func (f *fakeSettings) put(key, value, typ string) {
	_, _ = f.SetByKey(context.Background(), key, &shared.SettingModel{Value: value, Type: typ})
}

func (f *fakeSettings) value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.values[key]; ok {
		return m.Value
	}
	return ""
}

// recordingSwitch keeps what the service tells the switch
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
	return NewService(settings, cfg, sw, zap.NewNop().Sugar()), settings, sw
}

func TestState_IsOnByDefault(t *testing.T) {
	service, settings, sw := newService(&config.Config{})

	state, err := service.State(context.Background())

	require.NoError(t, err)
	assert.Equal(t, State{Enabled: true, ForcedOff: false}, state)
	assert.Empty(t, settings.values, "reading the state stores nothing")
	assert.Empty(t, sw.set, "and tells the switch nothing")
}

func TestSetEnabled_StoresTheChoiceAndTellsTheSwitch(t *testing.T) {
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

// Making a choice answers the notice that tells an upgraded install that reports are on
func TestSetEnabled_MarksTheUpgradeNoticeSeen(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		service, settings, _ := newService(&config.Config{})

		_, err := service.SetEnabled(context.Background(), enabled)

		require.NoError(t, err)
		assert.Equal(t, "true", settings.value("TELEMETRY_NOTICE_SEEN"))
		assert.Equal(t, shared.TelemetryNoticeSeenKey, "TELEMETRY_NOTICE_SEEN")
	}
}

func TestState_DoesNotMarkTheNoticeSeen(t *testing.T) {
	service, settings, _ := newService(&config.Config{})

	_, err := service.State(context.Background())
	require.NoError(t, err)
	require.NoError(t, service.Apply(context.Background()))

	assert.Empty(t, settings.values)
}

func TestSetEnabled_KeepsTheChoiceWhileTheEnvironmentForcesReportingOff(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"ERROR_REPORTING=off": {ErrorReporting: "off"},
	} {
		t.Run(name, func(t *testing.T) {
			service, settings, sw := newService(cfg)

			state, err := service.SetEnabled(context.Background(), true)

			require.NoError(t, err)
			assert.Equal(t, State{Enabled: true, ForcedOff: true}, state, "the choice is stored, and the environment still wins")
			assert.Equal(t, "true", settings.value(EnabledKey))
			assert.Equal(t, []bool{false}, sw.set, "the switch is told it is off")
		})
	}
}

func TestSetEnabled_TheOtherSwitchesDoNotForceItOff(t *testing.T) {
	service, _, _ := newService(&config.Config{Telemetry: "off"})

	state, err := service.State(context.Background())

	require.NoError(t, err)
	assert.False(t, state.ForcedOff, "TELEMETRY=off is the usage ping's switch")
}

// The stored setting is the source of truth. A switch that fails must not fail the change: the api
// applies the stored choice again every minute (Run).
func TestSetEnabled_StoresTheChoiceAndSucceedsWhenTheSwitchFails(t *testing.T) {
	service, settings, sw := newService(&config.Config{})
	sw.err = errors.New("the switch is stuck")

	state, err := service.SetEnabled(context.Background(), true)

	require.NoError(t, err)
	assert.Equal(t, State{Enabled: true}, state)
	assert.Equal(t, "true", settings.value(EnabledKey))
	assert.Equal(t, []bool{true}, sw.set, "the switch was tried")
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

func TestApply_TellsTheSwitchTheStoredChoiceAtStart(t *testing.T) {
	for name, tc := range map[string]struct {
		stored string
		cfg    *config.Config
		want   bool
	}{
		"nothing stored":                  {"", &config.Config{}, true},
		"nothing stored, forced off":      {"", &config.Config{ErrorReporting: "off"}, false},
		"nothing stored, the ping is off": {"", &config.Config{Telemetry: "off"}, true},
		"opted in":                        {"true", &config.Config{}, true},
		"opted out":                       {"false", &config.Config{}, false},
		"opted in, forced off":            {"true", &config.Config{ErrorReporting: "off"}, false},
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

// run starts service.Run and returns how to stop it and wait for it
func run(t *testing.T, service Service, every time.Duration) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx, every)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("Run did not stop with its context")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// A change made on another api replica is in the shared database, so this replica follows it
func TestRun_FollowsTheStoredChoiceWhoeverChangedIt(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	settings := newFakeSettings()
	service := NewService(settings, &config.Config{}, LocalSwitch{}, zap.NewNop().Sugar())
	run(t, service, 10*time.Millisecond)

	settings.put(EnabledKey, "true", "bool") // the other replica's change

	require.Eventually(t, reporting.Enabled, 2*time.Second, 5*time.Millisecond)

	settings.put(EnabledKey, "false", "bool")

	require.Eventually(t, func() bool { return !reporting.Enabled() }, 2*time.Second, 5*time.Millisecond)
}

func TestRun_ReportsNothingWhileTheEnvironmentForcesReportingOff(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(true)
	settings := newFakeSettings()
	settings.put(EnabledKey, "true", "bool")
	service := NewService(settings, &config.Config{ErrorReporting: "off"}, LocalSwitch{}, zap.NewNop().Sugar())

	run(t, service, 10*time.Millisecond)

	require.Eventually(t, func() bool { return !reporting.Enabled() }, 2*time.Second, 5*time.Millisecond)
}

func TestRun_KeepsGoingWhenTheSettingCannotBeRead(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	settings := newFakeSettings()
	settings.readErr = errors.New("database is locked")
	service := NewService(settings, &config.Config{}, LocalSwitch{}, zap.NewNop().Sugar())
	run(t, service, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.False(t, reporting.Enabled(), "nothing is read, so nothing is applied")

	settings.mu.Lock()
	settings.readErr = nil
	settings.mu.Unlock()
	settings.put(EnabledKey, "true", "bool")

	require.Eventually(t, reporting.Enabled, 2*time.Second, 5*time.Millisecond)
}
