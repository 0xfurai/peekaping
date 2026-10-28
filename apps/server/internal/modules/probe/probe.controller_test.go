package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// controllerFixture is a probe controller over a migrated SQLite database, with the local probe
// stored offline, a remote probe stored offline and another stored online
type controllerFixture struct {
	controller      *probe.Controller
	remoteOffline   string
	remoteOnline    string
	markLocalOnline func()
}

func newControllerFixture(t *testing.T, liveness shared.LocalProbeLiveness) controllerFixture {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	offline, online := f.create(t, "Frankfurt"), f.create(t, "Paris")
	_, err := f.svc.MarkOnline(ctx, online.ID, at(12, 0, 0))
	require.NoError(t, err)
	availability := probe.NewAvailabilityService(f.svc, liveness, func() time.Time { return at(12, 0, 0) })
	return controllerFixture{
		controller:    probe.NewController(f.svc, availability, nil, liveness, nil, zap.NewNop().Sugar()),
		remoteOffline: offline.ID,
		remoteOnline:  online.ID,
		markLocalOnline: func() {
			_, err := f.svc.MarkOnline(ctx, probe.LocalID, at(12, 1, 0))
			require.NoError(t, err)
		},
	}
}

func probeRequest(method, id, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/probes/"+id, strings.NewReader(body))
	request.SetPathValue("id", id)
	return request
}

// statuses reads the status of every probe the controller answers about, three ways: the list,
// the probe, and the update, which is given a name only and so changes no status
func (f controllerFixture) statuses(t *testing.T) map[string]map[string]string {
	t.Helper()
	ctx := context.Background()
	listed, err := f.controller.GetProbes(ctx, &probe.GetProbesInput{})
	require.NoError(t, err)
	inList := map[string]string{}
	for _, p := range listed.Body.Data {
		inList[p.ID] = p.Status
	}
	got := map[string]string{}
	updated := map[string]string{}
	for _, id := range []string{probe.LocalID, f.remoteOffline, f.remoteOnline} {
		one, err := f.controller.GetProbe(ctx, &probe.GetProbeInput{Request: probeRequest(http.MethodGet, id, "")})
		require.NoError(t, err)
		got[id] = one.Body.Data.Status
		put, err := f.controller.UpdateProbe(ctx, &probe.UpdateProbeInput{Request: probeRequest(http.MethodPut, id, `{"location":"rack 4"}`)})
		require.NoError(t, err)
		updated[id] = put.Body.Data.Status
	}
	return map[string]map[string]string{"list": inList, "get": got, "update": updated}
}

// AC1, AC4: in single mode the local probe is online whatever status is stored; a remote probe
// reports the status that is stored
func TestController_InSingleModeTheLocalProbeIsOnlineAndRemoteProbesReportTheirStatus(t *testing.T) {
	f := newControllerFixture(t, shared.LocalProbeIsTheProcess)

	for way, statuses := range f.statuses(t) {
		assert.Equal(t, "online", statuses[probe.LocalID], way+": the local probe, stored offline")
		assert.Equal(t, "offline", statuses[f.remoteOffline], way+": a remote probe stored offline")
		assert.Equal(t, "online", statuses[f.remoteOnline], way+": a remote probe stored online")
	}
}

// AC3, AC4: in cluster mode the local probe reports its stored status like a remote probe: offline
// until an instance connects, online while one is connected
func TestController_InClusterModeTheLocalProbeReportsItsStoredStatus(t *testing.T) {
	f := newControllerFixture(t, shared.LocalProbeIsItsInstances)

	for way, statuses := range f.statuses(t) {
		assert.Equal(t, "offline", statuses[probe.LocalID], way+": no instance is connected")
		assert.Equal(t, "offline", statuses[f.remoteOffline], way+": a remote probe stored offline")
		assert.Equal(t, "online", statuses[f.remoteOnline], way+": a remote probe stored online")
	}

	f.markLocalOnline()

	for way, statuses := range f.statuses(t) {
		assert.Equal(t, "online", statuses[probe.LocalID], way+": an instance is connected")
	}
}

// racingProbes is a probe service that reads the probe once with no instance and then with one: a
// first hello registered its instance between the two reads
type racingProbes struct {
	probe.Service
	reads   int
	pending int
}

func (s *racingProbes) FindByID(_ context.Context, id string) (*probe.Model, error) {
	s.reads++
	found := &probe.Model{ID: id, Name: "Frankfurt"}
	if s.reads > 1 {
		found.Instances = []*probe.Instance{{ID: "instance-1"}}
		found.SupportedTypes = []string{"http", "tcp"}
	}
	return found, nil
}

func (s *racingProbes) SetPendingAssignment(context.Context, string) error {
	s.pending++
	return nil
}

type recordingAssigner struct {
	calls [][]string
}

func (a *recordingAssigner) AssignProbeToAll(_ context.Context, _ string, supportedTypes []string) (int, error) {
	a.calls = append(a.calls, supportedTypes)
	return 2, nil
}

// AC5: a first hello that registers its instance after the action read the probe, and so didn't
// see the flag, would leave the probe waiting. The action reads the probe again once the flag is
// set, and assigns the instance it finds.
func TestController_TheActionAssignsAnInstanceThatRegisteredWhileItSetTheFlag(t *testing.T) {
	probes := &racingProbes{}
	assigner := &recordingAssigner{}
	controller := probe.NewController(probes, nil, assigner, shared.LocalProbeIsTheProcess, nil, zap.NewNop().Sugar())

	out, err := controller.AssignAllMonitors(context.Background(), &probe.AssignAllMonitorsInput{Request: probeRequest(http.MethodPost, "remote-1", "")})

	require.NoError(t, err)
	assert.Equal(t, 1, probes.pending, "the flag is set")
	assert.Equal(t, [][]string{{"http", "tcp"}}, assigner.calls, "assigned with the types of the second read")
	assert.Equal(t, 2, out.Body.Data.Added)
	assert.False(t, out.Body.Data.Pending)
}
