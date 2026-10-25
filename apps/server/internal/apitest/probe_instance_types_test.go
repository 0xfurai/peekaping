package apitest

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The monitor types of two builds of one probe, as the full image and the slim one say them in
// their hellos: the slim build leaves out the Docker, Kafka, SQL Server, MongoDB and gRPC executors
var (
	fullBuildTypes = []string{"dns", "docker", "http", "tcp"}
	slimBuildTypes = []string{"dns", "http", "tcp"}
)

const (
	fullInstance = "instance-full"
	slimInstance = "instance-slim"
)

func dockerMonitorBody(name string) map[string]any {
	return with(with(httpMonitorBody(name, ""), "type", "docker"), "config",
		`{"container_id":"web","connection_type":"socket","docker_daemon":"/var/run/docker.sock"}`)
}

// sayHelloWith has the instance say hello with the monitor types of its build
func (a *app) sayHelloWith(t *testing.T, token, instanceID string, types []string) {
	t.Helper()
	hello := helloFrom(instanceID)
	hello.SupportedTypes = types
	w := a.request(t, http.MethodPost, helloPath, hello, asProbe(token, instanceID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// AC4: instances of one probe on different builds don't flip the probe's supported types. Each
// instance's are stored on the instance, a monitor is dealt only to the online instances that run
// its type, and the probe's types are the union over its online instances.
func TestProbeInstanceTypes_InstancesOnDifferentBuildsOfOneProbe(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) (a *app, probeID, token string){
		"a remote probe": func(t *testing.T) (*app, string, string) {
			a := newAppWithAdmin(t)
			made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
			return a, made.ID, made.Token
		},
		"the local probe of a cluster": func(t *testing.T) (*app, string, string) {
			return localProbeApp(t, nil), "local", localToken
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, probeID, token := setup(t)
			a.sayHelloWith(t, token, fullInstance, fullBuildTypes)
			a.sayHelloWith(t, token, slimInstance, slimBuildTypes)

			assert.ElementsMatch(t, fullBuildTypes, a.getProbe(t, probeID).SupportedTypes,
				"the slim build's hello, which came last, didn't take docker from the probe")

			dockerID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(dockerMonitorBody("Containers"), probeID), a.asAdmin), http.StatusCreated).ID
			httpIDs := make([]string, 40)
			for i := range httpIDs {
				httpIDs[i] = decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody(fmt.Sprintf("Site %02d", i), ""), probeID), a.asAdmin), http.StatusCreated).ID
			}
			all := append([]string{dockerID}, httpIDs...)

			fullShare := decodeAssignments(t, a.assignments(t, token, fullInstance, ""))
			slimShare := decodeAssignments(t, a.assignments(t, token, slimInstance, ""))

			assert.Contains(t, monitorIDs(fullShare.Monitors), dockerID, "the docker monitor goes to the full build")
			assert.NotContains(t, monitorIDs(slimShare.Monitors), dockerID, "and not to the slim one")
			assert.NotEmpty(t, slimShare.Monitors, "the slim build is dealt its part of the monitors it runs")
			assert.ElementsMatch(t, all, append(monitorIDs(fullShare.Monitors), monitorIDs(slimShare.Monitors)...), "each monitor goes to exactly one instance")
			assert.Equal(t, fullShare.Version, slimShare.Version, "the instances of a probe hold one version")

			fullResults := a.outcomesOf(t, token, fullInstance, checkResult("full-docker", dockerID, checkedAt(0)))
			slimResults := a.outcomesOf(t, token, slimInstance, checkResult("slim-docker", dockerID, checkedAt(1)))
			assert.Equal(t, []probeproto.ResultOutcome{{ID: "full-docker", Accepted: true}}, fullResults)
			assert.Equal(t, []probeproto.ResultOutcome{{ID: "slim-docker", Reason: "not_assigned"}}, slimResults,
				"the slim build can't post results for a type it doesn't run")
			require.Len(t, a.storedHeartbeats(t, dockerID), 1)

			a.sayHelloWith(t, token, slimInstance, slimBuildTypes)
			assert.ElementsMatch(t, fullBuildTypes, a.getProbe(t, probeID).SupportedTypes, "the slim build saying hello again changes nothing")

			require.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(token, fullInstance)).Code)

			assert.ElementsMatch(t, slimBuildTypes, a.getProbe(t, probeID).SupportedTypes, "docker leaves the probe's types with the full build")
			alone := decodeAssignments(t, a.assignments(t, token, slimInstance, slimShare.Version))
			assert.ElementsMatch(t, httpIDs, monitorIDs(alone.Monitors), "the slim build has every monitor it runs, and the docker monitor is dealt to nobody")
			again := a.outcomesOf(t, token, slimInstance, checkResult("slim-docker-again", dockerID, checkedAt(2)))
			assert.Equal(t, []probeproto.ResultOutcome{{ID: "slim-docker-again", Reason: "not_assigned"}}, again)
			refused := a.request(t, http.MethodPost, monitorsPath, onProbes(dockerMonitorBody("More containers"), probeID), a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, refused.Code, "the probe doesn't run docker monitors now: "+refused.Body.String())

			a.sayHelloWith(t, token, fullInstance, fullBuildTypes)

			back := decodeAssignments(t, a.assignments(t, token, fullInstance, ""))
			assert.Contains(t, monitorIDs(back.Monitors), dockerID, "the docker monitor is dealt again once a full build is back")
			assert.ElementsMatch(t, fullBuildTypes, a.getProbe(t, probeID).SupportedTypes)
		})
	}
}

// What an instance is dealt depends on the types of the others, so a hello that changes only
// this instance's own types ends its siblings' polls, though the types the probe runs between
// its instances are the same as before
func TestProbeInstanceTypes_HelloWakesTheSiblingsWhenOnlyThisInstancesTypesChange(t *testing.T) {
	a := localProbeApp(t, nil)
	a.sayHelloWith(t, localToken, fullInstance, fullBuildTypes)
	a.sayHelloWith(t, localToken, slimInstance, slimBuildTypes)
	const count = 12
	for i := range count {
		decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(dockerMonitorBody(fmt.Sprintf("Containers %02d", i)), "local"), a.asAdmin), http.StatusCreated)
	}
	alone := decodeAssignments(t, a.assignments(t, localToken, fullInstance, ""))
	require.Len(t, alone.Monitors, count, "only the full build runs docker monitors")
	answered := a.startPoll(localToken, fullInstance, alone.Version)
	stillWaiting(t, answered)

	a.sayHelloWith(t, localToken, slimInstance, fullBuildTypes)

	got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
	assert.NotEqual(t, alone.Version, got.Version)
	assert.Less(t, len(got.Monitors), count, "the instance that can run docker monitors now takes some of them")
}
