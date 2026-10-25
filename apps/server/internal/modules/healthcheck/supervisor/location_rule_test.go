package supervisor

import (
	"testing"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
)

func twoOfThreeMonitor() *shared.Monitor {
	threshold := 2
	return &shared.Monitor{
		ID: "mon-1", Type: "http",
		LocationRule: shared.LocationRuleAtLeast, LocationThreshold: &threshold,
	}
}

func TestTickResult_IngestResultCarriesTheLocationRule(t *testing.T) {
	tick := &TickResult{Monitor: twoOfThreeMonitor(), ExecutionResult: &executor.Result{Status: shared.MonitorStatusUp}}

	got := tick.IngestResult(false)

	assert.Equal(t, shared.LocationRuleAtLeast, got.MonitorLocationRule)
	assert.Equal(t, 2, got.MonitorLocationThreshold)
}
