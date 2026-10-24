package providers

import (
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
)

func bindingLocation(probeID, name string, online bool, status shared.MonitorStatus) shared.MonitorLocation {
	return shared.MonitorLocation{ProbeID: probeID, ProbeName: name, Online: online, Status: &status}
}

func TestPrepareTemplateBindings_TheLocationsThatAreDownAndHowManyThereAre(t *testing.T) {
	m := &monitor.Model{ID: "mon-1", Name: "Public API", Locations: []shared.MonitorLocation{
		bindingLocation("0a", "Tokyo", true, shared.MonitorStatusDown),
		bindingLocation("1b", "Frankfurt", true, shared.MonitorStatusDown),
		bindingLocation("2c", "Oslo", false, shared.MonitorStatusDown),
		bindingLocation("3d", "Paris", true, shared.MonitorStatusUp),
		bindingLocation("local", "Local", true, shared.MonitorStatusUp),
	}}

	bindings := PrepareTemplateBindings(m, nil, "connection refused")

	assert.Equal(t, []string{"Frankfurt", "Tokyo"}, bindings["locations_down"], "online and Down, local first and then by name")
	assert.Equal(t, 5, bindings["locations_total"])
}

func TestPrepareTemplateBindings_AMonitorOnOneProbe(t *testing.T) {
	m := &monitor.Model{ID: "mon-1", Name: "Public API", Locations: []shared.MonitorLocation{bindingLocation("local", "Local", true, shared.MonitorStatusDown)}}

	bindings := PrepareTemplateBindings(m, nil, "connection refused")

	assert.Equal(t, []string{"Local"}, bindings["locations_down"])
	assert.Equal(t, 1, bindings["locations_total"])
}

func TestPrepareTemplateBindings_AMonitorWithoutLocationsIsOnTheLocalProbe(t *testing.T) {
	bindings := PrepareTemplateBindings(&monitor.Model{ID: "mon-1", Name: "Public API"}, nil, "msg")

	assert.Equal(t, []string{}, bindings["locations_down"], "a list, so that a template can range over it")
	assert.Equal(t, 1, bindings["locations_total"])
}

func TestPrepareTemplateBindings_NoMonitorNoLocationBindings(t *testing.T) {
	bindings := PrepareTemplateBindings(nil, nil, "msg")

	assert.NotContains(t, bindings, "locations_down")
	assert.NotContains(t, bindings, "locations_total")
}

func TestPrepareTemplateBindings_TheMonitorCarriesItsLocationRule(t *testing.T) {
	threshold := 2
	m := &monitor.Model{ID: "mon-1", LocationRule: shared.LocationRuleAtLeast, LocationThreshold: &threshold}

	monitorBinding := PrepareTemplateBindings(m, nil, "msg")["monitor"].(map[string]any)

	assert.Equal(t, "at_least", monitorBinding["location_rule"])
	assert.EqualValues(t, 2, monitorBinding["location_threshold"])
}
