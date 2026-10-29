package apitest

import (
	"encoding/json"
	"net/http"
	"testing"

	"peekaping/internal/config"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-156: with BLOCK_PRIVATE_TARGETS on, saving a monitor whose target is a literal blocked address,
// or a proxy whose host is one, is refused with 400 and TARGET_NOT_ALLOWED. A name is saved: what it
// resolves to is checked when the check dials.

// withBlockPrivateTargets sets BLOCK_PRIVATE_TARGETS to the value a test needs, whatever the run's
// default is. A test whose servers listen on 127.0.0.1 and that saves monitors or proxies pointing
// at them sets it to false, since the cloud's run blocks that address; none needed it so far.
func withBlockPrivateTargets(on bool) func(*config.Config) {
	return func(cfg *config.Config) { cfg.BlockPrivateTargets = on }
}

type targetNotAllowedError struct {
	Message string `json:"message"`
	Error   struct {
		Code string `json:"code"`
		Host string `json:"host"`
	} `json:"error"`
}

func assertTargetNotAllowed(t *testing.T, w *httpRecorder, host string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var got targetNotAllowedError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	assert.Equal(t, "TARGET_NOT_ALLOWED", got.Error.Code)
	assert.Equal(t, host, got.Error.Host)
	assert.Equal(t, "target address is not allowed: "+host, got.Message)
}

// monitorWithConfig is a monitor body of the given type and config
func monitorWithConfig(name, monitorType, monitorConfig string) map[string]any {
	return with(with(httpMonitorBody(name, ""), "type", monitorType), "config", monitorConfig)
}

func httpConfigFor(url string) string {
	return `{"url":"` + url + `","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`
}

func TestBlockPrivateTargets_AMonitorAtABlockedAddressIsRefusedOnCreate(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	tests := []struct {
		name string
		body map[string]any
		host string
	}{
		{"http on loopback", monitorWithConfig("Local", "http", httpConfigFor("http://127.0.0.1:8080/health")), "127.0.0.1"},
		{"http on the metadata address", monitorWithConfig("Metadata", "http", httpConfigFor("http://169.254.169.254/latest/meta-data")), "169.254.169.254"},
		{"http on a private IPv6 address", monitorWithConfig("IPv6 monitor", "http-keyword", httpConfigFor("http://[fd12::1]/")), "fd12::1"},
		{"tcp on a private network", monitorWithConfig("Database", "tcp", `{"host":"10.0.0.5","port":5432}`), "10.0.0.5"},
		{"ping on a private network", monitorWithConfig("Router", "ping", `{"host":"192.168.1.1"}`), "192.168.1.1"},
		{"dns through a local resolver", monitorWithConfig("Dns", "dns", `{"host":"example.com","resolver_server":"127.0.0.53","port":53,"resolve_type":"A"}`), "127.0.0.53"},
		{"smtp on carrier-grade NAT", monitorWithConfig("Mail", "smtp", `{"host":"100.64.0.1","port":25}`), "100.64.0.1"},
		{"postgres on a private network", monitorWithConfig("Postgres", "postgres", `{"database_connection_string":"postgres://app:secret@10.1.2.3:5432/app"}`), "10.1.2.3"},
		{"redis on loopback", monitorWithConfig("Cache", "redis", `{"databaseConnectionString":"redis://127.0.0.1:6379"}`), "127.0.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTargetNotAllowed(t, a.request(t, http.MethodPost, monitorsPath, tc.body, a.asAdmin), tc.host)
		})
	}
	listed := decode[[]typedMonitor](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	assert.Empty(t, listed, "nothing was stored")
}

func TestBlockPrivateTargets_AHostnameTargetIsSaved(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))

	for _, body := range []map[string]any{
		monitorWithConfig("Site", "http", httpConfigFor("https://internal.corp.example/health")),
		monitorWithConfig("Localhost", "http", httpConfigFor("http://localhost:8080/")),
		monitorWithConfig("Public", "http", httpConfigFor("http://8.8.8.8/")),
		monitorWithConfig("Database", "tcp", `{"host":"db.corp.example","port":5432}`),
		monitorWithConfig("Postgres", "postgres", `{"database_connection_string":"postgres://app:secret@db.corp.example:5432/app"}`),
		monitorWithConfig("Cache", "redis", `{"databaseConnectionString":"redis://localhost:6379"}`),
		monitorWithConfig("Push", "push", `{"pushToken":"abcdefghijklmnopqrstuvwxyz012345"}`),
	} {
		decode[typedMonitor](t, a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin), http.StatusCreated)
	}
}

func TestBlockPrivateTargets_AMonitorCannotBeMovedToABlockedAddress(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	id := a.createHTTPMonitor(t, "Site", "")
	path := monitorsPath + "/" + id
	blocked := httpConfigFor("http://10.1.2.3/")

	put := a.request(t, http.MethodPut, path, with(httpMonitorBody("Site", ""), "config", blocked), a.asAdmin)
	patchConfig := a.request(t, http.MethodPatch, path, map[string]any{"config": blocked}, a.asAdmin)
	patchType := a.request(t, http.MethodPatch, path, map[string]any{"type": "tcp", "config": `{"host":"169.254.169.254","port":80}`}, a.asAdmin)

	assertTargetNotAllowed(t, put, "10.1.2.3")
	assertTargetNotAllowed(t, patchConfig, "10.1.2.3")
	assertTargetNotAllowed(t, patchType, "169.254.169.254")
	got := a.typedMonitorOf(t, id)
	assert.Equal(t, "http", got.Type, "it stays what it was")

	// a name, and an edit that leaves the target alone, still save
	decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"config": httpConfigFor("https://other.example.com/")}, a.asAdmin), http.StatusOK)
	decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed"}, a.asAdmin), http.StatusOK)
}

func TestBlockPrivateTargets_AMonitorStoredBeforeCanStillBePausedAndRenamed(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	id := a.storeMonitor(t, "Local", "http", httpConfigFor("http://127.0.0.1:8080/"), "")
	path := monitorsPath + "/" + id

	renamed := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Local v2", "active": false}, a.asAdmin), http.StatusOK)

	assert.Equal(t, "Local v2", renamed.Name)
	assert.False(t, renamed.Active)
}

func TestBlockPrivateTargets_AProxyAtABlockedAddressIsRefused(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))

	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"} {
		assertTargetNotAllowed(t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody(host), a.asAdmin), host)
	}
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("proxy.corp.example"), a.asAdmin), http.StatusCreated).ID
	path := "/api/v1/proxies/" + id

	assertTargetNotAllowed(t, a.request(t, http.MethodPut, path, proxyBody("192.168.0.9"), a.asAdmin), "192.168.0.9")
	assertTargetNotAllowed(t, a.request(t, http.MethodPatch, path, map[string]any{"host": "172.16.0.2"}, a.asAdmin), "172.16.0.2")
	// a name, a public address and an edit that leaves the host alone save
	decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"port": 3128}, a.asAdmin), http.StatusOK)
	decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"host": "8.8.8.8"}, a.asAdmin), http.StatusOK)
	decode[created](t, a.request(t, http.MethodPut, path, proxyBody("other.corp.example"), a.asAdmin), http.StatusOK)
}

func TestBlockPrivateTargets_OffEverythingSaves(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(false))

	for _, body := range []map[string]any{
		monitorWithConfig("Local", "http", httpConfigFor("http://127.0.0.1:8080/health")),
		monitorWithConfig("Metadata", "http", httpConfigFor("http://169.254.169.254/")),
		monitorWithConfig("Database", "tcp", `{"host":"10.0.0.5","port":5432}`),
		monitorWithConfig("Router", "ping", `{"host":"192.168.1.1"}`),
	} {
		id := decode[created](t, a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin), http.StatusCreated).ID
		decode[typedMonitor](t, a.request(t, http.MethodPut, monitorsPath+"/"+id, body, a.asAdmin), http.StatusOK)
		decode[typedMonitor](t, a.request(t, http.MethodPatch, monitorsPath+"/"+id, map[string]any{"config": body["config"]}, a.asAdmin), http.StatusOK)
	}
	proxyID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("127.0.0.1"), a.asAdmin), http.StatusCreated).ID
	decode[created](t, a.request(t, http.MethodPut, "/api/v1/proxies/"+proxyID, proxyBody("10.0.0.1"), a.asAdmin), http.StatusOK)
	decode[created](t, a.request(t, http.MethodPatch, "/api/v1/proxies/"+proxyID, map[string]any{"host": "::1"}, a.asAdmin), http.StatusOK)
}

// AC5, the server's side: hello tells a probe whether the server it connects to blocks private
// targets, so that the probe blocks them too. A server that doesn't leaves the field out, as every
// server did before.
func TestBlockPrivateTargets_HelloStatesTheServersSetting(t *testing.T) {
	for _, on := range []bool{true, false} {
		a := newAppWithAdmin(t, withBlockPrivateTargets(on))
		remote := a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "eu-central"})

		w := a.request(t, http.MethodPost, helloPath, helloFrom("instance-1"), asProbe(remote.Token, "instance-1"))

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var raw map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		if on {
			assert.Equal(t, true, raw["block_private_targets"])
		} else {
			assert.NotContains(t, raw, "block_private_targets", "a server that doesn't block says nothing, as before")
		}
	}
}

// helloSaying is the hello of an instance as JSON, with enforces_private_target_block when the
// probe enforces the block, and without it as a probe from before says hello
func helloSaying(t *testing.T, instanceID string, enforces bool) map[string]any {
	t.Helper()
	raw, err := json.Marshal(helloFrom(instanceID))
	require.NoError(t, err)
	var hello map[string]any
	require.NoError(t, json.Unmarshal(raw, &hello))
	delete(hello, "enforces_private_target_block")
	if enforces {
		hello["enforces_private_target_block"] = true
	}
	return hello
}

// A probe from before the setting ignores block_private_targets, so a server that blocks private
// targets deals no monitor to an instance whose hello doesn't say that it enforces the block. The
// probe's other instances get its monitors, and the probe API says which instances don't enforce it.
func TestBlockPrivateTargets_AnInstanceThatDoesNotEnforceTheBlockIsDealtNoMonitors(t *testing.T) {
	a := localProbeApp(t, nil, withBlockPrivateTargets(true))
	checkout := a.createHTTPMonitor(t, "Checkout", "")
	old, current := uuid.NewString(), uuid.NewString()

	w := a.request(t, http.MethodPost, helloPath, helloSaying(t, old, false), asProbe(localToken, old))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, decodeAssignments(t, a.assignments(t, localToken, old, "")).Monitors, "an old probe would check private targets")

	w = a.request(t, http.MethodPost, helloPath, helloSaying(t, current, true), asProbe(localToken, current))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{checkout}, monitorIDs(decodeAssignments(t, a.assignments(t, localToken, current, "")).Monitors))
	assert.Empty(t, decodeAssignments(t, a.assignments(t, localToken, old, "")).Monitors, "nothing is dealt to it beside the other")

	enforces := map[string]bool{}
	for _, instance := range a.getProbe(t, "local").Instances {
		enforces[instance.ID] = instance.EnforcesPrivateTargetBlock
	}
	assert.Equal(t, map[string]bool{old: false, current: true}, enforces)
}

// The server says why an instance gets nothing, once for each instance and not at each hello
func TestBlockPrivateTargets_AnInstanceThatDoesNotEnforceTheBlockIsWarnedOfOnce(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	a := localProbeApp(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, withBlockPrivateTargets(true))
	old, current := uuid.NewString(), uuid.NewString()

	for range 2 {
		require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, helloSaying(t, old, false), asProbe(localToken, old)).Code)
	}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, helloSaying(t, current, true), asProbe(localToken, current)).Code)

	warned := logs.FilterMessageSnippet("too old to block private targets").All()
	require.Len(t, warned, 1)
	assert.Equal(t, old, warned[0].ContextMap()["instanceId"])
}

// With the setting off, an instance that doesn't say it enforces the block gets its monitors as
// before
func TestBlockPrivateTargets_OffAnInstanceThatDoesNotEnforceTheBlockGetsItsMonitors(t *testing.T) {
	a := localProbeApp(t, nil, withBlockPrivateTargets(false))
	checkout := a.createHTTPMonitor(t, "Checkout", "")
	old := uuid.NewString()

	w := a.request(t, http.MethodPost, helloPath, helloSaying(t, old, false), asProbe(localToken, old))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, []string{checkout}, monitorIDs(decodeAssignments(t, a.assignments(t, localToken, old, "")).Monitors))
}
