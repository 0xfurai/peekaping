package apitest

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// monitorsOnProbe are the ids of the monitors that have a row for the probe, read from the table
// and not through the API, which shows a monitor without rows on the local probe
func (a *app) monitorsOnProbe(t *testing.T, probeID string) []string {
	t.Helper()
	ids := []string{}
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("monitor_probes").Column("monitor_id").Where("probe_id = ?", probeID).Scan(context.Background(), &ids)
	}))
	return ids
}

// AC1: a probe made with assign_all_monitors waits to be assigned and has no monitors yet. One made
// without the field is as it was.
func TestProbeAssignAll_CreateMarksTheProbeAsWaiting(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createHTTPMonitor(t, "Checkout", "")

	waiting := a.createProbe(t, map[string]any{"name": "Frankfurt", "assign_all_monitors": true})
	assert.True(t, waiting.AssignAllPending)
	assert.True(t, a.getProbe(t, waiting.ID).AssignAllPending, "a read of the probe shows it too")
	assert.Empty(t, a.monitorsOnProbe(t, waiting.ID), "the probe gets its monitors on its first hello, not at its creation")

	for name, body := range map[string]map[string]any{
		"without the field": {"name": "Paris"},
		"set to false":      {"name": "Lyon", "assign_all_monitors": false},
	} {
		t.Run(name, func(t *testing.T) {
			plain := a.createProbe(t, body)
			assert.False(t, plain.AssignAllPending)
			assert.Empty(t, a.monitorsOnProbe(t, plain.ID))
		})
	}
	assert.False(t, a.getProbe(t, "local").AssignAllPending)
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin).Code)
}
