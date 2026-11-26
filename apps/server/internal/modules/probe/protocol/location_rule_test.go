package protocol_test

import (
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A remote probe never tells the server a monitor's location rule: the server builds the result it
// ingests from the monitor it holds, as it does for a push
func TestResults_TheResultCarriesTheMonitorsLocationRuleFromTheServer(t *testing.T) {
	s := newResultsServer(t)
	threshold := 2
	s.monitors.byID[monCheckout].LocationRule = shared.LocationRuleAtLeast
	s.monitors.byID[monCheckout].LocationThreshold = &threshold
	start := time.Now().Add(-time.Minute)

	got := s.results(t, probeproto.Result{ID: "result-1", MonitorID: monCheckout, Status: probeproto.StatusDown, StartTime: start, EndTime: start})

	require.Equal(t, []probeproto.ResultOutcome{accepted("result-1")}, got)
	require.Len(t, s.ingest.stored, 1)
	assert.Equal(t, shared.LocationRuleAtLeast, s.ingest.stored[0].MonitorLocationRule)
	assert.Equal(t, 2, s.ingest.stored[0].MonitorLocationThreshold)
}
