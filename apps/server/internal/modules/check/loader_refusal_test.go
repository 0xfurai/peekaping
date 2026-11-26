package check

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"peekaping/internal/modules/monitor"
)

// A monitor that has several reasons not to be checked is refused for the first of them, in the
// order paused, workspace suspended, type turned off, over the workspace's limit.
func TestLoader_RefusesAMonitorForTheFirstOfItsReasons(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape func(*monitor.Model)
		want  error
	}{
		{"over its workspace's limit", func(m *monitor.Model) { m.OverLimit = true }, ErrMonitorOverLimit},
		{"over the limit and of a type turned off", func(m *monitor.Model) { m.OverLimit, m.TypeDisabled = true, true }, ErrMonitorTypeDisabled},
		{"of a type turned off and a suspended workspace", func(m *monitor.Model) { m.TypeDisabled, m.WorkspaceSuspended = true, true }, ErrWorkspaceSuspended},
		{"suspended and paused", func(m *monitor.Model) { m.WorkspaceSuspended, m.Active = true, false }, ErrMonitorPaused},
		{"every reason", func(m *monitor.Model) {
			m.Active, m.WorkspaceSuspended, m.TypeDisabled, m.OverLimit = false, true, true, true
		}, ErrMonitorPaused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mon := activeMonitor()
			tc.shape(mon)
			f := newFixture(t, mon)
			f.maintenance.active[monitorID] = true

			job, err := f.loader.Load(context.Background(), monitorID)

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, job)
			assert.Zero(t, f.heartbeats.reads)
		})
	}
}
