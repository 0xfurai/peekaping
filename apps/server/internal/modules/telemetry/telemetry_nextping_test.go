package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedReads is the settings table whose reads the test can fail or answer in its own way: script
// gets the key and how many times it was read, and answers when it says it has
type scriptedReads struct {
	*fakeSettings
	mu     sync.Mutex
	reads  map[string]int
	script func(key string, n int) (model *shared.SettingModel, err error, answered bool)
}

func (s *scriptedReads) GetByKey(ctx context.Context, key string) (*shared.SettingModel, error) {
	s.mu.Lock()
	if s.reads == nil {
		s.reads = map[string]int{}
	}
	s.reads[key]++
	n := s.reads[key]
	s.mu.Unlock()
	if model, err, answered := s.script(key, n); answered {
		return model, err
	}
	return s.fakeSettings.GetByKey(ctx, key)
}

func (i *install) serviceOver(settings shared.SettingService) Service {
	return NewService(settings, i.monitors, i.channels, i.statusPages, i.maintenances, i.cfg, i.mode)
}

var pingTime = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)

func TestNextPing_WaitsADayFromTheLastPing(t *testing.T) {
	for name, tc := range map[string]struct {
		lastSent string
		due      bool
	}{
		"never sent":                {"", true},
		"a minute ago":              {pingTime.Add(-time.Minute).Format(time.RFC3339), false},
		"just now":                  {pingTime.Format(time.RFC3339), false},
		"23 hours ago":              {pingTime.Add(-23 * time.Hour).Format(time.RFC3339), false},
		"exactly a day ago":         {pingTime.Add(-24 * time.Hour).Format(time.RFC3339), false},
		"a day and a second ago":    {pingTime.Add(-24*time.Hour - time.Second).Format(time.RFC3339), true},
		"a week ago":                {pingTime.Add(-7 * 24 * time.Hour).Format(time.RFC3339), true},
		"a second from now":         {pingTime.Add(time.Second).Format(time.RFC3339), true},
		"a day from now":            {pingTime.Add(24 * time.Hour).Format(time.RFC3339), true},
		"a time that is not a time": {"yesterday", true},
		"a time of another format":  {"2026-10-22 07:59:00", true},
	} {
		t.Run(name, func(t *testing.T) {
			in := newInstall()
			in.optIn(testInstanceID)
			if tc.lastSent != "" {
				in.settings.put(LastSentKey, tc.lastSent, "string")
			}

			ping, err := in.service().NextPing(context.Background(), pingTime)

			require.NoError(t, err)
			if !tc.due {
				assert.Nil(t, ping)
				return
			}
			require.NotNil(t, ping)
			assert.Equal(t, testInstanceID, ping.InstanceID)
		})
	}
}

func TestNextPing_SendsNothingForAnInstallThatOptedOut(t *testing.T) {
	in := newInstall()
	in.settings.put(EnabledKey, "false", "bool")

	ping, err := in.service().NextPing(context.Background(), pingTime)

	require.NoError(t, err)
	assert.Nil(t, ping)
	_, stored := in.settings.values[InstanceIDKey]
	assert.False(t, stored, "and makes no identifier")
}

func TestNextPing_FailsWithTheReadThatFailed(t *testing.T) {
	for _, key := range []string{EnabledKey, InstanceIDKey, LastSentKey} {
		t.Run(key, func(t *testing.T) {
			in := newInstall()
			in.optIn(testInstanceID)
			failure := errors.New("database is locked")
			settings := &scriptedReads{fakeSettings: in.settings, script: func(k string, n int) (*shared.SettingModel, error, bool) {
				return nil, failure, k == key
			}}

			ping, err := in.serviceOver(settings).NextPing(context.Background(), pingTime)

			assert.Nil(t, ping)
			require.ErrorIs(t, err, failure)
			assert.ErrorContains(t, err, "read "+key)
		})
	}
}

func TestNextPing_ReadsTheChoiceBeforeAnythingElse(t *testing.T) {
	in := newInstall()
	failure := errors.New("database is locked")
	var order []string
	settings := &scriptedReads{fakeSettings: in.settings, script: func(k string, n int) (*shared.SettingModel, error, bool) {
		order = append(order, k)
		return nil, failure, k == EnabledKey
	}}

	_, err := in.serviceOver(settings).NextPing(context.Background(), pingTime)

	require.ErrorIs(t, err, failure)
	assert.Equal(t, []string{EnabledKey}, order, "nothing else is read, and no ID is made")
	assert.Empty(t, in.settings.values)
}

func TestNextPing_FailsWhenTheInstallCannotBeRead(t *testing.T) {
	in := newInstall()
	in.optIn(testInstanceID)
	in.monitors.err = errors.New("monitors are gone")

	ping, err := in.service().NextPing(context.Background(), pingTime)

	assert.Nil(t, ping)
	assert.ErrorContains(t, err, "list monitors: monitors are gone")
}

// An admin who opts out while the ping is being built leaves it no instance ID to send
func TestNextPing_SendsNothingWhenTheAdminOptsOutWhileThePayloadIsBuilt(t *testing.T) {
	in := newInstall()
	in.optIn(testInstanceID)
	settings := &scriptedReads{fakeSettings: in.settings, script: func(k string, n int) (*shared.SettingModel, error, bool) {
		switch {
		case k == InstanceIDKey && n >= 2: // the ID is gone when the payload is built
			return nil, nil, true
		case k == EnabledKey && n >= 2: // and the choice is off
			return &shared.SettingModel{Key: k, Value: "false", Type: "bool"}, nil, true
		}
		return nil, nil, false
	}}

	ping, err := in.serviceOver(settings).NextPing(context.Background(), pingTime)

	require.NoError(t, err)
	assert.Nil(t, ping)
}
