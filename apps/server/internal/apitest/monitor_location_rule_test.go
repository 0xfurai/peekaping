package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type monitorWithRule struct {
	ID                string   `json:"id"`
	ProbeIDs          []string `json:"probe_ids"`
	LocationRule      string   `json:"location_rule"`
	LocationThreshold *int     `json:"location_threshold"`
}

func ptr(n int) *int { return &n }

// ruleOf reads the monitor back
func (a *app) ruleOf(t *testing.T, id string) monitorWithRule {
	t.Helper()
	return decode[monitorWithRule](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK)
}

// withRule is a monitor body with a location rule
func withRule(body map[string]any, rule string, threshold *int) map[string]any {
	body = with(body, "location_rule", rule)
	if threshold != nil {
		body = with(body, "location_threshold", *threshold)
	}
	return body
}

func TestMonitorLocationRule_DefaultsToAny(t *testing.T) {
	a := newAppWithAdmin(t)

	made := decode[monitorWithRule](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Checkout", ""), a.asAdmin), http.StatusCreated)

	assert.Equal(t, "any", made.LocationRule, "the response to the create")
	assert.Nil(t, made.LocationThreshold)
	stored := a.ruleOf(t, made.ID)
	assert.Equal(t, "any", stored.LocationRule, "and what is stored")
	assert.Nil(t, stored.LocationThreshold)
}

func TestMonitorLocationRule_RoundTrips(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	tokyo := a.createProbe(t, map[string]any{"name": "Tokyo"})
	body := onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID, tokyo.ID)

	made := decode[monitorWithRule](t, a.request(t, http.MethodPost, monitorsPath, withRule(body, "at_least", ptr(2)), a.asAdmin), http.StatusCreated)
	assert.Equal(t, "at_least", made.LocationRule, "the response to the create")
	assert.Equal(t, ptr(2), made.LocationThreshold)
	stored := a.ruleOf(t, made.ID)
	assert.Equal(t, "at_least", stored.LocationRule, "and what is stored")
	assert.Equal(t, ptr(2), stored.LocationThreshold)
	path := monitorsPath + "/" + made.ID

	kept := decode[monitorWithRule](t, a.request(t, http.MethodPut, path, body, a.asAdmin), http.StatusOK)
	assert.Equal(t, "at_least", kept.LocationRule, "a full update that doesn't send a rule keeps it")
	assert.Equal(t, ptr(2), kept.LocationThreshold)

	all := decode[monitorWithRule](t, a.request(t, http.MethodPut, path, withRule(body, "all", nil), a.asAdmin), http.StatusOK)
	assert.Equal(t, "all", all.LocationRule, "a full update to a rule without a threshold")
	assert.Nil(t, all.LocationThreshold)

	patched := decode[monitorWithRule](t, a.request(t, http.MethodPatch, path, map[string]any{"location_rule": "at_least", "location_threshold": 3}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "at_least", patched.LocationRule, "a partial update")
	assert.Equal(t, ptr(3), patched.LocationThreshold)

	renamed := decode[monitorWithRule](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Checkout, renamed"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "at_least", renamed.LocationRule, "a partial update without a rule keeps it")
	assert.Equal(t, ptr(3), renamed.LocationThreshold)

	lowered := decode[monitorWithRule](t, a.request(t, http.MethodPatch, path, map[string]any{"location_threshold": 1}, a.asAdmin), http.StatusOK)
	assert.Equal(t, ptr(1), lowered.LocationThreshold, "and a threshold alone changes the one the rule has")

	any := decode[monitorWithRule](t, a.request(t, http.MethodPatch, path, map[string]any{"location_rule": "any"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "any", any.LocationRule)
	assert.Nil(t, any.LocationThreshold, "a partial update to a rule without a threshold clears it")
	assert.Equal(t, a.ruleOf(t, made.ID), any, "and the list reads the same")
}

func TestMonitorLocationRule_RejectsAThresholdThatDoesNotFitTheProbes(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	tokyo := a.createProbe(t, map[string]any{"name": "Tokyo"})
	three := onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID, tokyo.ID)
	id := decode[monitorWithRule](t, a.request(t, http.MethodPost, monitorsPath, withRule(three, "at_least", ptr(2)), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	for name, tc := range map[string]struct {
		rule      string
		threshold *int
		message   string
	}{
		"zero":                 {"at_least", ptr(0), "Invalid location_rule: at_least 0 is out of range: the monitor runs on 3 probes, so it takes 1 to 3"},
		"negative":             {"at_least", ptr(-1), "Invalid location_rule: at_least -1 is out of range: the monitor runs on 3 probes, so it takes 1 to 3"},
		"more than the probes": {"at_least", ptr(4), "Invalid location_rule: at_least 4 is out of range: the monitor runs on 3 probes, so it takes 1 to 3"},
		"none":                 {"at_least", nil, "Invalid location_rule: at_least needs a location_threshold"},
		"with all":             {"all", ptr(2), "Invalid location_rule: location_threshold is only for the at_least rule"},
		"with any":             {"any", ptr(1), "Invalid location_rule: location_threshold is only for the at_least rule"},
		"an unknown rule":      {"most", nil, `Invalid location_rule: "most" is not a rule: use any, all or at_least`},
	} {
		t.Run(name, func(t *testing.T) {
			patch := map[string]any{"location_rule": tc.rule}
			if tc.threshold != nil {
				patch["location_threshold"] = *tc.threshold
			}
			for _, c := range []call{
				{"create", http.MethodPost, monitorsPath, withRule(three, tc.rule, tc.threshold)},
				{"full update", http.MethodPut, path, withRule(three, tc.rule, tc.threshold)},
				{"partial update", http.MethodPatch, path, patch},
			} {
				w := a.request(t, c.method, c.path, c.body, a.asAdmin)

				assert.Equal(t, http.StatusBadRequest, w.Code, c.name+": "+w.Body.String())
				assert.Equal(t, tc.message, errorMessage(t, w.Body.Bytes()), c.name)
			}
		})
	}

	stored := a.ruleOf(t, id)
	assert.Equal(t, "at_least", stored.LocationRule, "the monitor stays as it was")
	assert.Equal(t, ptr(2), stored.LocationThreshold)
	assert.Len(t, decode[[]monitorWithRule](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK), 1, "and no monitor is created")
}

func TestMonitorLocationRule_AThresholdOnAMonitorWithOneProbe(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodPost, monitorsPath, withRule(httpMonitorBody("Checkout", ""), "at_least", ptr(2)), a.asAdmin)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "Invalid location_rule: at_least 2 is out of range: the monitor runs on 1 probe, so it takes only 1", errorMessage(t, w.Body.Bytes()))
	ok := a.request(t, http.MethodPost, monitorsPath, withRule(httpMonitorBody("Checkout", ""), "at_least", ptr(1)), a.asAdmin)
	assert.Equal(t, http.StatusCreated, ok.Code, ok.Body.String())
}

func TestMonitorLocationRule_RemovingProbesBelowTheThresholdIsRejected(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	tokyo := a.createProbe(t, map[string]any{"name": "Tokyo"})
	three := onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID, tokyo.ID)
	id := decode[monitorWithRule](t, a.request(t, http.MethodPost, monitorsPath, withRule(three, "at_least", ptr(3)), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id
	const message = "Invalid location_rule: at_least 3 is out of range: the monitor runs on 2 probes, so it takes 1 to 2"

	patch := a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{"local", frankfurt.ID}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, patch.Code, patch.Body.String())
	assert.Equal(t, message, errorMessage(t, patch.Body.Bytes()), "a partial update that only removes a probe")

	put := a.request(t, http.MethodPut, path, onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID), a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, put.Code, put.Body.String())
	assert.Equal(t, message, errorMessage(t, put.Body.Bytes()), "a full update that doesn't send the rule, which stays")
	assert.Len(t, a.ruleOf(t, id).ProbeIDs, 3, "the monitor keeps its probes")

	both := a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{"local", frankfurt.ID}, "location_rule": "all"}, a.asAdmin)
	require.Equal(t, http.StatusOK, both.Code, both.Body.String())
	moved := a.ruleOf(t, id)
	assert.Equal(t, "all", moved.LocationRule, "removing a probe and changing the rule in one request is fine")
	assert.Len(t, moved.ProbeIDs, 2)

	toTwo := a.request(t, http.MethodPatch, path, map[string]any{"location_rule": "at_least", "location_threshold": 2}, a.asAdmin)
	require.Equal(t, http.StatusOK, toTwo.Code, toTwo.Body.String())
	shrink := a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, shrink.Code, shrink.Body.String())
	assert.Equal(t, "Invalid location_rule: at_least 2 is out of range: the monitor runs on 1 probe, so it takes only 1", errorMessage(t, shrink.Body.Bytes()))
}

func TestMonitorLocationRule_AMonitorWithoutAThresholdCanLoseProbes(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	id := decode[monitorWithRule](t, a.request(t, http.MethodPost, monitorsPath, withRule(onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID), "all", nil), a.asAdmin), http.StatusCreated).ID

	w := a.request(t, http.MethodPatch, monitorsPath+"/"+id, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "all", a.ruleOf(t, id).LocationRule)
}
