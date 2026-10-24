package apitest

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// monitorLocation is one place a monitor runs, as the API returns it
type monitorLocation struct {
	ProbeID         string     `json:"probe_id"`
	ProbeName       string     `json:"probe_name"`
	Online          bool       `json:"online"`
	Status          *int       `json:"status"`
	LastPing        *int       `json:"last_ping"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
}

type monitorWithLocations struct {
	monitorWithProbes
	Locations []monitorLocation `json:"locations"`
}

func probeIDsOf(locations []monitorLocation) []string {
	ids := make([]string, len(locations))
	for i, l := range locations {
		ids[i] = l.ProbeID
	}
	return ids
}

func TestMonitorProbes_ReturnsLocations(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	paris := a.createProbe(t, map[string]any{"name": "Paris"})

	made := decode[monitorWithLocations](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID, paris.ID), a.asAdmin), http.StatusCreated)

	want := []monitorLocation{
		{ProbeID: frankfurt.ID, ProbeName: "Frankfurt", Online: true},
		{ProbeID: paris.ID, ProbeName: "Paris", Online: false},
		{ProbeID: "local", ProbeName: "Local", Online: true},
	}
	sort.Slice(want, func(i, j int) bool { return want[i].ProbeID < want[j].ProbeID })
	assert.Equal(t, want, made.Locations, "a new monitor's locations have no status yet, in probe_ids order")
	assert.Equal(t, made.ProbeIDs, probeIDsOf(made.Locations))
	assert.Equal(t, want, decode[monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK).Locations, "get")
	list := decode[[]monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, want, list[0].Locations, "list")
}
