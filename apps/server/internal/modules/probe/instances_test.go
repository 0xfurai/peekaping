package probe_test

import (
	"testing"
	"time"

	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlineInstances_WindowOf60s(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	instances := []*probe.Instance{
		{ID: "c-just-now", LastSeenAt: now},
		{ID: "a-59s", LastSeenAt: now.Add(-59 * time.Second)},
		{ID: "d-60s", LastSeenAt: now.Add(-60 * time.Second)},
		{ID: "e-61s", LastSeenAt: now.Add(-61 * time.Second)},
		{ID: "b-an-hour", LastSeenAt: now.Add(-time.Hour)},
		{ID: "f-ahead", LastSeenAt: now.Add(3 * time.Second)},
	}

	got := probe.OnlineInstances(instances, now, probe.InstanceOnlineWindow)

	assert.Equal(t, []string{"a-59s", "c-just-now", "d-60s", "f-ahead"}, got, "seen within the last 60 s, sorted; a clock a little ahead of ours is online too")
	assert.Equal(t, 60*time.Second, probe.InstanceOnlineWindow)
}

func TestOnlineInstances_NoneIsAnEmptyList(t *testing.T) {
	require.NotNil(t, probe.OnlineInstances(nil, time.Now(), time.Minute))
}
