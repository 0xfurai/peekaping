package producer

import (
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
)

func TestRunProducer_QueuesTheMonitorsLocationRule(t *testing.T) {
	threshold := 2
	mon := &monitor.Model{
		ID: "mon-http", Name: "API", Type: "http", Active: true, Interval: 60, Timeout: 5,
		LocationRule: shared.LocationRuleAtLeast, LocationThreshold: &threshold,
		Config: `{"url":"https://example.com"}`,
	}
	e := newEngine(t, mon)
	e.addDue(t, "mon-http", t0)

	e.startJobProcessing(t)

	e.eventuallyDueAt(t, "mon-http", time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC))
	_, payload := e.queuedCheck(t, "mon-http")
	assert.Equal(t, shared.LocationRuleAtLeast, payload.LocationRule)
	assert.Equal(t, 2, payload.LocationThreshold)
}
