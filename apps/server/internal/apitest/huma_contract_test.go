package apitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"peekaping/internal"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// These cases pin behavior that automatic binding and validation can change.
func TestHTTPContract_DecodingAndErrorOrder(t *testing.T) {
	a := newAppWithAdmin(t)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		creds                    credentials
	}{
		{"auth before body", "POST", "/api/v1/tags", "{", 401, nil},
		{"auth before query", "GET", "/api/v1/tags?page=invalid", "", 401, nil},
		{"hidden setting before body", "PUT", "/api/v1/settings/key/ACCESS_TOKEN_SECRET_KEY", "{", 404, a.asAdmin},
		{"malformed body", "POST", "/api/v1/tags", "{", 400, a.asAdmin},
		{"unknown fields and trailing JSON", "POST", "/api/v1/tags", `{"name":"Contract","color":"#3B82F6","unknown":true} {"ignored":true}`, 201, a.asAdmin},
		{"empty query defaults", "GET", "/api/v1/tags?page=&limit=", "", 200, a.asAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creds []credentials
			if tc.creds != nil {
				creds = append(creds, tc.creds)
			}
			w := a.request(t, tc.method, tc.path, tc.body, creds...)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Empty(t, w.Header().Get("Link"))
			assert.NotContains(t, w.Body.String(), `"$schema"`)
			assert.NotContains(t, w.Body.String(), "\n")
			if tc.status >= 400 {
				var body map[string]any
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Len(t, body, 2)
				assert.IsType(t, "", body["message"])
				assert.Contains(t, body, "data")
				assert.Nil(t, body["data"])
			}
		})
	}
}

func TestHTTPContract_FormatsAndBodySize(t *testing.T) {
	a := newAppWithAdmin(t)
	for n, contentType := range []string{"", "text/plain", "application/json"} {
		// Nearly 1 MiB, the most a request may send
		body := fmt.Sprintf(`{"name":"Formats %d","color":"#3B82F6","description":%q}`, n, strings.Repeat("x", 1024*1024-1024))
		req := httptest.NewRequest("POST", "/api/v1/tags", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "text/csv")
		a.asAdmin(req)
		w := httptest.NewRecorder()
		a.router.ServeHTTP(w, req)
		require.Equal(t, 201, w.Code)
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	}
}

func TestHTTPContract_PatchOmissionNullAndEmpty(t *testing.T) {
	a := newAppWithAdmin(t)
	monitor := a.createMonitor(t, "Patch monitor", "patch-token")
	id := a.createStatusPage(t, statusPage{slug: "patch", published: true, monitors: []string{monitor}, domains: []string{"patch.example"}})
	path := "/api/v1/status-pages/" + id
	decode[any](t, a.request(t, "PATCH", path, `{"published":false,"monitor_ids":null}`, a.asAdmin), 200)
	got := decode[statusPageResponse](t, a.request(t, "GET", path, nil, a.asAdmin), 200)
	assert.False(t, got.Published)
	assert.Equal(t, []string{monitor}, got.MonitorIDs)
	assert.Equal(t, []string{"patch.example"}, got.Domains)
	decode[any](t, a.request(t, "PATCH", path, `{"monitor_ids":[],"domains":[]}`, a.asAdmin), 200)
	got = decode[statusPageResponse](t, a.request(t, "GET", path, nil, a.asAdmin), 200)
	assert.Empty(t, got.MonitorIDs)
	assert.Empty(t, got.Domains)
}

func TestOpenAPI_UsesCurrentHost(t *testing.T) {
	w := newApp(t).request(t, http.MethodGet, "/openapi.json", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, "3.1.0", doc["openapi"])
	assert.NotContains(t, w.Body.String(), "localhost")
	assert.NotEmpty(t, doc["paths"])
}

func TestOpenAPI_CompleteAndAccurateContract(t *testing.T) {
	a := newApp(t)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `json:"operationId"`
			Security    []map[string][]string `json:"security"`
			Responses   map[string]struct {
				Content map[string]json.RawMessage `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Required             []string `json:"required"`
				AdditionalProperties any      `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(a.request(t, "GET", "/openapi.json", nil).Body.Bytes(), &spec))
	count := 0
	ids := map[string]bool{}
	for _, route := range a.routes() {
		if route.path != "/health" && !strings.HasPrefix(route.path, "/api/v1/") && !strings.HasPrefix(route.path, "/probe/v1/") {
			continue
		}
		count++
		op, ok := spec.Paths[route.path][strings.ToLower(route.method)]
		require.True(t, ok, "missing %s", route.key())
		assert.NotEmpty(t, op.OperationID)
		assert.False(t, ids[op.OperationID], "duplicate operation ID %s", op.OperationID)
		ids[op.OperationID] = true
		assert.NotContains(t, op.Responses, "422", route.key())
		if _, public := publicRoutes[route.key()]; public {
			assert.Empty(t, op.Security, route.key())
		} else if jwtOnlyRoutes[route.key()] {
			assert.Equal(t, []map[string][]string{{"JwtAuth": {}}}, op.Security, route.key())
		} else if probeRoutes[route.key()] {
			assert.Equal(t, []map[string][]string{{"ProbeAuth": {}}}, op.Security, route.key())
			assert.True(t, strings.HasPrefix(op.OperationID, "probe"), "%s: probe operations are prefixed so they can't collide", route.key())
		} else {
			assert.Equal(t, []map[string][]string{{"JwtAuth": {}}, {"ApiKeyAuth": {}}}, op.Security, route.key())
		}
		if strings.Contains(route.path, "/badge/") {
			assert.Contains(t, op.Responses["200"].Content, "image/svg+xml")
			assert.Contains(t, op.Responses["404"].Content, "application/json")
		}
	}
	assert.Equal(t, 92, count)
	specCount := 0
	for _, methods := range spec.Paths {
		specCount += len(methods)
	}
	assert.Equal(t, count, specCount)
	assert.ElementsMatch(t, []string{"email", "password"}, spec.Components.Schemas["auth.LoginDto"].Required)
	assert.Equal(t, true, spec.Components.Schemas["auth.LoginDto"].AdditionalProperties)
	assert.ElementsMatch(t, []string{"name", "color"}, spec.Components.Schemas["tag.CreateUpdateDto"].Required)
	assert.Empty(t, spec.Components.Schemas["monitor.PartialUpdateDto"].Required)
	assert.Empty(t, spec.Paths["/api/v1/api-keys/{id}"]["delete"].Responses["204"].Content)
}

func TestOpenAPI_ExportMatchesRuntime(t *testing.T) {
	exported, err := json.Marshal(internal.OpenAPI())
	require.NoError(t, err)
	assert.JSONEq(t, string(exported), newApp(t).request(t, "GET", "/openapi.json", nil).Body.String())
}

func TestOpenAPI_ResponsesMatchSchemas(t *testing.T) {
	a := localProbeApp(t, nil)
	id := a.createMonitor(t, "Contract monitor", "contract-push")
	contractProxy := decode[created](t, a.request(t, "POST", "/api/v1/proxies", map[string]any{
		"protocol": "http", "host": "contract.proxy.example", "port": 3128, "auth": true, "username": "u", "password": "p",
	}, a.asAdmin), 201).ID
	contractHTTPID := a.createHTTPMonitor(t, "Contract http monitor", contractProxy)
	checkedAt := time.Now().UTC().Add(-time.Minute)
	contractResults := map[string]any{"results": []map[string]any{
		{"id": "contract-result-1", "monitor_id": contractHTTPID, "status": "up", "message": "200 - OK", "ping_ms": 12,
			"start_time": checkedAt.Format(time.RFC3339Nano), "end_time": checkedAt.Add(12 * time.Millisecond).Format(time.RFC3339Nano)},
		{"id": "contract-result-2", "monitor_id": "no-such-monitor", "status": "down", "message": "", "ping_ms": 0,
			"start_time": checkedAt.Format(time.RFC3339Nano), "end_time": checkedAt.Format(time.RFC3339Nano)},
	}}
	a.createStatusPage(t, statusPage{slug: "contract", published: true, monitors: []string{id}, domains: []string{"contract.example"}})
	decode[created](t, a.request(t, "POST", "/api/v1/tags", tagBody("Contract"), a.asAdmin), 201)
	protocolProbe := a.createProbe(t, map[string]any{"name": "Contract protocol probe"})
	oldHello := helloFrom("contract-old")
	oldHello.ProtocolVersion = 0
	doc, err := jsonschema.UnmarshalJSON(a.request(t, "GET", "/openapi.json", nil).Body)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	check := func(t *testing.T, method, path, media string, w *httptest.ResponseRecorder) {
		t.Helper()
		escape := strings.NewReplacer("~", "~0", "/", "~1")
		pointer := fmt.Sprintf("#/paths/%s/%s/responses/%d/content/%s/schema", escape.Replace(path), strings.ToLower(method), w.Code, escape.Replace(media))
		schema, err := compiler.Compile("https://peekaping.test/openapi.json" + pointer)
		require.NoError(t, err)
		var value any
		if media == "application/json" {
			value, err = jsonschema.UnmarshalJSON(w.Body)
			require.NoError(t, err)
		} else {
			value = w.Body.String()
		}
		require.NoError(t, schema.Validate(value), "%s %s status %d", method, path, w.Code)
	}
	for _, tc := range []struct {
		name, method, path, url string
		body                    any
		status                  int
		creds                   credentials
	}{
		{"health nulls", "GET", "/health", "/health", nil, 200, nil},
		{"list", "GET", "/api/v1/monitors", "/api/v1/monitors", nil, 200, a.asAdmin},
		{"monitor detail", "GET", "/api/v1/monitors/{id}", "/api/v1/monitors/" + id, nil, 200, a.asAdmin},
		{"missing TLS", "GET", "/api/v1/monitors/{id}/tls", "/api/v1/monitors/" + id + "/tls", nil, 200, a.asAdmin},
		{"unauthenticated", "GET", "/api/v1/tags", "/api/v1/tags", nil, 401, nil},
		{"malformed JSON", "POST", "/api/v1/tags", "/api/v1/tags", "{", 400, a.asAdmin},
		{"not found", "GET", "/api/v1/tags/{id}", "/api/v1/tags/missing", nil, 404, a.asAdmin},
		{"conflict", "POST", "/api/v1/tags", "/api/v1/tags", tagBody("Contract"), 409, a.asAdmin},
		{"domain error", "POST", "/api/v1/status-pages", "/api/v1/status-pages", map[string]any{"slug": "another", "title": "Another", "domains": []string{"contract.example"}}, 400, a.asAdmin},
		{"bare 2FA error", "POST", "/api/v1/auth/2fa/verify", "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail, "code": "123456"}, 400, a.asAdmin},
		{"push", "GET", "/api/v1/push/{token}", "/api/v1/push/contract-push", nil, 200, nil},
		{"probe with its token", "POST", "/api/v1/probes", "/api/v1/probes", map[string]any{"name": "Contract probe", "location": "eu"}, 201, a.asAdmin},
		{"probe list", "GET", "/api/v1/probes", "/api/v1/probes", nil, 200, a.asAdmin},
		{"probe detail", "GET", "/api/v1/probes/{id}", "/api/v1/probes/local", nil, 200, a.asAdmin},
		{"local probe delete conflict", "DELETE", "/api/v1/probes/{id}", "/api/v1/probes/local", nil, 409, a.asAdmin},
		{"local probe rotate conflict", "POST", "/api/v1/probes/{id}/rotate-token", "/api/v1/probes/local/rotate-token", nil, 409, a.asAdmin},
		{"probe not found", "GET", "/api/v1/probes/{id}", "/api/v1/probes/missing", nil, 404, a.asAdmin},
		{"probe availability list", "GET", "/api/v1/probes/availability", "/api/v1/probes/availability?range=7d", nil, 200, a.asAdmin},
		{"probe availability list, bad range", "GET", "/api/v1/probes/availability", "/api/v1/probes/availability?range=1y", nil, 400, a.asAdmin},
		{"probe availability", "GET", "/api/v1/probes/{id}/availability", "/api/v1/probes/local/availability", nil, 200, a.asAdmin},
		{"probe availability, bad range", "GET", "/api/v1/probes/{id}/availability", "/api/v1/probes/local/availability?range=1y", nil, 400, a.asAdmin},
		{"probe availability not found", "GET", "/api/v1/probes/{id}/availability", "/api/v1/probes/missing/availability", nil, 404, a.asAdmin},
		{"probe protocol hello", "POST", "/probe/v1/hello", "/probe/v1/hello", helloFrom("contract-instance"), 200, asProbe(protocolProbe.Token, "contract-instance")},
		{"probe protocol assignments", "GET", "/probe/v1/assignments", "/probe/v1/assignments?version=", nil, 200, asProbe(protocolProbe.Token, "contract-instance")},
		{"probe protocol hello of the local probe", "POST", "/probe/v1/hello", "/probe/v1/hello", helloFrom("contract-local"), 200, asProbe(localToken, "contract-local")},
		{"probe protocol assignments with a monitor and its proxy", "GET", "/probe/v1/assignments", "/probe/v1/assignments?version=", nil, 200, asProbe(localToken, "contract-local")},
		{"probe protocol results with an accepted and a rejected result", "POST", "/probe/v1/results", "/probe/v1/results", contractResults, 200, asProbe(localToken, "contract-local")},
		{"probe protocol results, bad body", "POST", "/probe/v1/results", "/probe/v1/results", "{", 400, asProbe(localToken, "contract-local")},
		{"probe protocol results, too many", "POST", "/probe/v1/results", "/probe/v1/results", map[string]any{"results": make([]map[string]any, 501)}, 400, asProbe(localToken, "contract-local")},
		{"probe protocol results, unknown instance", "POST", "/probe/v1/results", "/probe/v1/results", contractResults, 409, asProbe(protocolProbe.Token, "contract-unknown")},
		{"probe protocol assignments, unknown instance", "GET", "/probe/v1/assignments", "/probe/v1/assignments?version=", nil, 409, asProbe(protocolProbe.Token, "contract-unknown")},
		{"probe protocol hello, old version", "POST", "/probe/v1/hello", "/probe/v1/hello", oldHello, 426, asProbe(protocolProbe.Token, "contract-old")},
		{"probe protocol hello, bad body", "POST", "/probe/v1/hello", "/probe/v1/hello", "{", 400, asProbe(protocolProbe.Token, "contract-bad")},
		{"probe protocol, unknown token", "POST", "/probe/v1/hello", "/probe/v1/hello", helloFrom("contract-instance"), 401, asProbe("pkp_no-probe-has-it", "contract-instance")},
		{"probe protocol, no instance id", "POST", "/probe/v1/bye", "/probe/v1/bye", nil, 400, withHeader("Authorization", "Bearer "+protocolProbe.Token)},
		{"probe protocol bye, unknown instance", "POST", "/probe/v1/bye", "/probe/v1/bye", nil, 409, asProbe(protocolProbe.Token, "contract-unknown")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creds []credentials
			if tc.creds != nil {
				creds = append(creds, tc.creds)
			}
			w := a.request(t, tc.method, tc.url, tc.body, creds...)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			check(t, tc.method, tc.path, "application/json", w)
		})
	}
	w := a.request(t, "GET", "/api/v1/badge/"+id+"/status", nil)
	require.Equal(t, 200, w.Code)
	check(t, "GET", "/api/v1/badge/{monitorId}/status", "image/svg+xml", w)
	assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
	// Lockout has its own body and Retry-After header.
	for n := 0; n < maxFailedLogins; n++ {
		a.request(t, "POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": "wrong"})
	}
	w = a.request(t, "POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": "wrong"})
	require.Equal(t, 429, w.Code)
	_, err = strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	check(t, "POST", "/api/v1/auth/login", "application/json", w)
}

func TestOpenAPI_WeekdayItemsMatchValidation(t *testing.T) {
	a := newAppWithAdmin(t)
	doc, err := jsonschema.UnmarshalJSON(a.request(t, "GET", "/openapi.json", nil).Body)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	for _, dto := range []string{"CreateUpdateDto", "PartialUpdateDto"} {
		schema, err := compiler.Compile("https://peekaping.test/openapi.json#/components/schemas/maintenance." + dto)
		require.NoError(t, err)
		body := weeklyMaintenanceBody("Every day", "09:00", "17:00")
		body["weekdays"] = []int{0, 1, 2, 3, 4, 5, 6}
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		value, err := jsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
		require.NoError(t, err)
		assert.NoError(t, schema.Validate(value), dto+" accepts all seven weekdays")
		body["weekdays"] = []int{7}
		encoded, err = json.Marshal(body)
		require.NoError(t, err)
		value, err = jsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
		require.NoError(t, err)
		assert.Error(t, schema.Validate(value), dto+" rejects weekday 7")
	}
	body := weeklyMaintenanceBody("Every day", "09:00", "17:00")
	body["weekdays"] = []int{0, 1, 2, 3, 4, 5, 6}
	w := a.request(t, "POST", "/api/v1/maintenances", body, a.asAdmin)
	require.Equal(t, 201, w.Code, w.Body.String())
	body["weekdays"] = []int{7}
	assert.Equal(t, 400, a.request(t, "POST", "/api/v1/maintenances", body, a.asAdmin).Code)
}

func TestOpenAPI_RequiredValuesMatchValidation(t *testing.T) {
	a := newAppWithAdmin(t)
	doc, err := jsonschema.UnmarshalJSON(a.request(t, "GET", "/openapi.json", nil).Body)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	for _, tc := range []struct {
		name, dto, path, field string
		body                   map[string]any
		invalid                any
	}{
		{"required list", "monitor.CreateUpdateDto", "/api/v1/monitors", "notification_ids", pushMonitorBody("Required list", "required-list"), nil},
		{"required string", "proxy.CreateUpdateDto", "/api/v1/proxies", "host", proxyBody("required.example"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema, err := compiler.Compile("https://peekaping.test/openapi.json#/components/schemas/" + tc.dto)
			require.NoError(t, err)
			check := func(valid bool) {
				t.Helper()
				encoded, err := json.Marshal(tc.body)
				require.NoError(t, err)
				value, err := jsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
				require.NoError(t, err)
				w := a.request(t, "POST", tc.path, tc.body, a.asAdmin)
				if valid {
					assert.NoError(t, schema.Validate(value))
					assert.Equal(t, 201, w.Code, w.Body.String())
				} else {
					assert.Error(t, schema.Validate(value))
					assert.Equal(t, 400, w.Code, w.Body.String())
				}
			}
			check(true)
			tc.body[tc.field] = tc.invalid
			check(false)
		})
	}
}

func TestOpenAPI_TelemetryEnabledIsNotNullable(t *testing.T) {
	a := newAppWithAdmin(t)
	doc, err := jsonschema.UnmarshalJSON(a.request(t, "GET", "/openapi.json", nil).Body)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	schema, err := compiler.Compile("https://peekaping.test/openapi.json#/components/schemas/telemetry.UpdateDto")
	require.NoError(t, err)
	for _, tc := range []struct {
		body   string
		valid  bool
		status int
	}{
		{`{"enabled":true}`, true, 200},
		{`{"enabled":false}`, true, 200},
		{`{"enabled":null}`, false, 400},
		{`{}`, false, 400},
	} {
		t.Run(tc.body, func(t *testing.T) {
			value, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.body))
			require.NoError(t, err)
			if tc.valid {
				assert.NoError(t, schema.Validate(value))
			} else {
				assert.Error(t, schema.Validate(value))
			}
			w := a.request(t, "PUT", "/api/v1/telemetry", tc.body, a.asAdmin)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
}

func TestOpenAPI_StatPointsDatabaseErrorMatchesSchema(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Database failure", "database-failure")
	doc, err := jsonschema.UnmarshalJSON(a.request(t, "GET", "/openapi.json", nil).Body)
	require.NoError(t, err)
	// Make the record lookup fail while leaving authentication available.
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("DROP TABLE monitors")
		return err
	}))
	w := a.request(t, "GET", "/api/v1/monitors/"+id+"/stats/points?since=2026-01-01T00:00:00Z&until=2026-01-01T01:00:00Z", nil, a.asAdmin)
	require.Equal(t, 500, w.Code, w.Body.String())
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	schema, err := compiler.Compile("https://peekaping.test/openapi.json#/paths/~1api~1v1~1monitors~1{id}~1stats~1points/get/responses/500/content/application~1json/schema")
	require.NoError(t, err)
	value, err := jsonschema.UnmarshalJSON(w.Body)
	require.NoError(t, err)
	assert.NoError(t, schema.Validate(value))
}
