package healthcheck

import (
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func twoOfThreeMonitor() *monitor.Model {
	threshold := 2
	return &monitor.Model{
		ID: "mon-1", Type: "http",
		LocationRule: shared.LocationRuleAtLeast, LocationThreshold: &threshold,
	}
}

func TestResultFromMonitor_CarriesTheLocationRule(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

	got := ResultFromMonitor(twoOfThreeMonitor(), false, shared.MonitorStatusDown, "refused", 5, now, now, nil, zap.NewNop().Sugar())

	assert.Equal(t, shared.LocationRuleAtLeast, got.MonitorLocationRule)
	assert.Equal(t, 2, got.MonitorLocationThreshold)
}

func TestResultFromMonitor_AMonitorWithoutARuleHasNoThreshold(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

	got := ResultFromMonitor(&monitor.Model{ID: "mon-1", Type: "http"}, false, shared.MonitorStatusUp, "", 5, now, now, nil, zap.NewNop().Sugar())

	assert.Empty(t, got.MonitorLocationRule, "empty means any")
	assert.Zero(t, got.MonitorLocationThreshold)
}
