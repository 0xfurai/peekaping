package apitest

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
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
	// The cluster api's local probe is online while its instances are connected, like a remote
	// probe: none has said hello here
	a := newAppWithAdmin(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	paris := a.createProbe(t, map[string]any{"name": "Paris"})

	made := decode[monitorWithLocations](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID, paris.ID), a.asAdmin), http.StatusCreated)

	want := []monitorLocation{
		{ProbeID: frankfurt.ID, ProbeName: "Frankfurt", Online: true},
		{ProbeID: paris.ID, ProbeName: "Paris", Online: false},
		{ProbeID: "local", ProbeName: "Local", Online: false},
	}
	sort.Slice(want, func(i, j int) bool { return want[i].ProbeID < want[j].ProbeID })
	assert.Equal(t, want, made.Locations, "a new monitor's locations have no status yet, in probe_ids order")
	assert.Equal(t, made.ProbeIDs, probeIDsOf(made.Locations))
	assert.Equal(t, want, decode[monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK).Locations, "get")
	list := decode[[]monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, want, list[0].Locations, "list")
}

// AC4: the local probe's location is online once an instance started with the local probe's token
// has said hello
func TestMonitorProbes_TheLocalLocationFollowsTheLocalProbesInstances(t *testing.T) {
	a := localProbeApp(t, nil)
	made := decode[monitorWithLocations](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Checkout", ""), a.asAdmin), http.StatusCreated)
	require.Len(t, made.Locations, 1)
	assert.False(t, made.Locations[0].Online, "no instance of the local probe is connected")

	a.sayHello(t, localToken, uuid.NewString())

	found := decode[monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK)
	assert.True(t, found.Locations[0].Online, "an instance said hello")
}

// A push monitor runs no checks: its pushes arrive at the api, so its location on the local probe is
// online without any instance
func TestMonitorProbes_APushMonitorsLocalLocationIsOnlineWithoutInstances(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Nightly backup", "backup-push-token")

	found := decode[monitorWithLocations](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK)

	require.Len(t, found.Locations, 1)
	assert.True(t, found.Locations[0].Online)
}
