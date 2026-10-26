package apitest

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-331 AC1: a check a probe instance sends is stored with that instance's id, so with two
// instances of one probe each heartbeat names the instance that sent it.
func TestProbeProtocol_AHeartbeatNamesTheInstanceThatSentIt(t *testing.T) {
	a := localProbeApp(t, nil)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	monitorID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	first, second := uuid.NewString(), uuid.NewString()
	a.sayHello(t, frankfurt.Token, first)
	a.sayHello(t, frankfurt.Token, second)

	a.outcomesOf(t, frankfurt.Token, first, checkResult("from-first", monitorID, checkedAt(0)))
	a.outcomesOf(t, frankfurt.Token, second, checkResult("from-second", monitorID, checkedAt(10)))

	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 2)
	require.NotNil(t, beats[0].InstanceID)
	require.NotNil(t, beats[1].InstanceID)
	assert.Equal(t, first, *beats[0].InstanceID)
	assert.Equal(t, second, *beats[1].InstanceID)
	assert.Equal(t, []string{frankfurt.ID, frankfurt.ID}, []string{beats[0].ProbeID, beats[1].ProbeID}, "both are the one probe's")
}

// In cluster mode the local probe's checks come in through the probe protocol too, so they record
// their instance.
func TestProbeProtocol_TheLocalProbesHeartbeatNamesItsInstance(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)

	a.outcomesOf(t, localToken, instanceID, checkResult("local-check", monitorID, checkedAt(0)))

	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 1)
	assert.Equal(t, "local", beats[0].ProbeID)
	require.NotNil(t, beats[0].InstanceID)
	assert.Equal(t, instanceID, *beats[0].InstanceID)
}
