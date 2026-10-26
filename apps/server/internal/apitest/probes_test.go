package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/probe"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type probeInstanceResponse struct {
	ID              string    `json:"id"`
	Hostname        string    `json:"hostname"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	Version         string    `json:"version"`
	ProtocolVersion int       `json:"protocol_version"`
	SupportedTypes  []string  `json:"supported_types"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

type probeResponse struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Location        string                  `json:"location"`
	TokenPrefix     string                  `json:"token_prefix"`
	Disabled        bool                    `json:"disabled"`
	Status          string                  `json:"status"`
	SupportedTypes  []string                `json:"supported_types"`
	NotificationIDs []string                `json:"notification_ids"`
	Instances       []probeInstanceResponse `json:"instances"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type probeWithToken struct {
	probeResponse
	Token string `json:"token"`
}

const probesPath = "/api/v1/probes"

func (a *app) createProbe(t *testing.T, body map[string]any) probeWithToken {
	t.Helper()
	return decode[probeWithToken](t, a.request(t, http.MethodPost, probesPath, body, a.asAdmin), http.StatusCreated)
}

// probeByToken is the probe a token finds, which is how the probe protocol will authenticate a probe
func (a *app) probeByToken(t *testing.T, token string) *probe.Model {
	t.Helper()
	var found *probe.Model
	require.NoError(t, a.container.Invoke(func(svc probe.Service) error {
		var err error
		found, err = svc.FindByToken(context.Background(), token)
		return err
	}))
	return found
}

func errorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	return envelope.Message
}

func TestProbesAPI_ManagesProbe(t *testing.T) {
	a := newAppWithAdmin(t)

	probeBody := map[string]any{"name": "Frankfurt", "location": "eu-central"}
	made := a.createProbe(t, probeBody)
	_, err := uuid.Parse(made.ID)
	assert.NoError(t, err, "a probe's id is a UUID")
	assert.Equal(t, "Frankfurt", made.Name)
	assert.Equal(t, "eu-central", made.Location)
	assert.False(t, made.Disabled)
	assert.Equal(t, "offline", made.Status)
	assert.Equal(t, []string{}, made.SupportedTypes)
	assert.Equal(t, []string{}, made.NotificationIDs)
	assert.Equal(t, []probeInstanceResponse{}, made.Instances)
	assert.WithinDuration(t, time.Now(), made.CreatedAt, time.Minute)
	path := probesPath + "/" + made.ID

	// An API key reaches the routes too
	list := decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.withAPIKey), http.StatusOK)
	require.Len(t, list, 2)
	assert.Equal(t, "local", list[0].ID, "the local probe comes first")
	assert.Equal(t, made.probeResponse, list[1])

	assert.Equal(t, made.probeResponse, decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK))

	updated := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"name": "Paris", "location": "eu-west", "disabled": true}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Paris", updated.Name)
	assert.Equal(t, "eu-west", updated.Location)
	assert.True(t, updated.Disabled)
	assert.Equal(t, made.TokenPrefix, updated.TokenPrefix)
	assert.Equal(t, made.CreatedAt, updated.CreatedAt)
	assert.Equal(t, updated, decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK))

	onlyName := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"name": "Lyon"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Lyon", onlyName.Name)
	assert.Equal(t, "eu-west", onlyName.Location, "a field the update leaves out stays")
	assert.True(t, onlyName.Disabled)

	w := a.request(t, http.MethodDelete, path, nil, a.asAdmin)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
	assert.Len(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK), 1)
}

func TestProbesAPI_ShowsTheTokenOnce(t *testing.T) {
	a := newAppWithAdmin(t)

	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})

	assert.Regexp(t, `^pkp_[A-Za-z0-9_-]{43}$`, made.Token)
	assert.Equal(t, made.Token[:8], made.TokenPrefix)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"get":    a.request(t, http.MethodGet, probesPath+"/"+made.ID, nil, a.asAdmin),
		"list":   a.request(t, http.MethodGet, probesPath, nil, a.asAdmin),
		"update": a.request(t, http.MethodPut, probesPath+"/"+made.ID, map[string]any{"name": "Paris"}, a.asAdmin),
	} {
		assert.NotContains(t, w.Body.String(), made.Token, name)
		assert.NotContains(t, w.Body.String(), `"token"`, name)
		assert.NotContains(t, w.Body.String(), "token_hash", name)
		assert.Contains(t, w.Body.String(), `"token_prefix":"`+made.TokenPrefix+`"`, name)
	}

	var hash string
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("probes").Column("token_hash").Where("id = ?", made.ID).Scan(context.Background(), &hash)
	}))
	assert.Regexp(t, `^sha256\$[0-9a-f]{64}$`, hash, "the database holds the SHA-256 hash")
	assert.NotContains(t, hash, made.Token)
	found := a.probeByToken(t, made.Token)
	require.NotNil(t, found)
	assert.Equal(t, made.ID, found.ID)
}

func TestProbesAPI_RotateInvalidatesTheOldToken(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})

	rotated := decode[probeWithToken](t, a.request(t, http.MethodPost, probesPath+"/"+made.ID+"/rotate-token", nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, made.ID, rotated.ID)
	assert.Regexp(t, `^pkp_[A-Za-z0-9_-]{43}$`, rotated.Token)
	assert.NotEqual(t, made.Token, rotated.Token)
	assert.Equal(t, rotated.Token[:8], rotated.TokenPrefix)
	assert.Nil(t, a.probeByToken(t, made.Token), "the old token no longer finds the probe")
	found := a.probeByToken(t, rotated.Token)
	require.NotNil(t, found, "the new token finds it at once")
	assert.Equal(t, made.ID, found.ID)
	got := decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, rotated.TokenPrefix, got.TokenPrefix)
}

func TestProbesAPI_DisabledProbeTokenFindsNothing(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})

	decode[probeResponse](t, a.request(t, http.MethodPut, probesPath+"/"+made.ID, map[string]any{"disabled": true}, a.asAdmin), http.StatusOK)

	assert.Nil(t, a.probeByToken(t, made.Token))
}

func TestProbesAPI_LocalCannotBeDeletedRotatedOrDisabled(t *testing.T) {
	a := newAppWithAdmin(t)
	local := probesPath + "/local"
	channel := a.createChannel(t, "Ops")

	got := decode[probeResponse](t, a.request(t, http.MethodGet, local, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "local", got.ID)
	assert.Equal(t, "Local", got.Name)
	assert.Equal(t, "offline", got.Status)
	assert.Equal(t, "", got.TokenPrefix)
	assert.False(t, got.Disabled)

	w := a.request(t, http.MethodDelete, local, nil, a.asAdmin)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, errorMessage(t, w.Body.Bytes()), "local probe can't be deleted")

	w = a.request(t, http.MethodPost, local+"/rotate-token", nil, a.asAdmin)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "The local probe's token can't be rotated. In cluster mode it is set with PROBE_BOOTSTRAP_TOKEN: set or change it and restart Peekaping. In single mode the local probe takes no token: the peekaping process checks its monitors", errorMessage(t, w.Body.Bytes()),
		"the message says where the local token comes from in either mode, and holds when the variable is unset")

	w = a.request(t, http.MethodPut, local, map[string]any{"disabled": true}, a.asAdmin)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, errorMessage(t, w.Body.Bytes()), "local probe can't be disabled")

	renamed := decode[probeResponse](t, a.request(t, http.MethodPut, local, map[string]any{
		"name": "Headquarters", "location": "rack 4", "notification_ids": []string{channel},
	}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Headquarters", renamed.Name)
	assert.Equal(t, "rack 4", renamed.Location)
	assert.Equal(t, []string{channel}, renamed.NotificationIDs)
	assert.False(t, renamed.Disabled)
	assert.Equal(t, renamed, decode[probeResponse](t, a.request(t, http.MethodGet, local, nil, a.asAdmin), http.StatusOK))
}

func TestProbesAPI_BootstrapTokenAuthenticatesLocal(t *testing.T) {
	const token = "bootstrap-token-0123456789-abcdefghijklmnop"
	a := newAppWithAdmin(t, func(cfg *config.Config) { cfg.ProbeBootstrapToken = token })

	found := a.probeByToken(t, token)

	require.NotNil(t, found, "the bootstrap token finds a probe")
	assert.Equal(t, "local", found.ID)
	assert.Nil(t, a.probeByToken(t, token+"x"))
	local := decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/local", nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "bootstra", local.TokenPrefix)
	assert.NotContains(t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin).Body.String(), token)
}

func TestProbesAPI_WithoutABootstrapTokenLocalHasNone(t *testing.T) {
	a := newAppWithAdmin(t)

	local := decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/local", nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, "", local.TokenPrefix)
	assert.Nil(t, a.probeByToken(t, ""))
}

func TestProbesAPI_NotificationChannels(t *testing.T) {
	a := newAppWithAdmin(t)
	slack, email := a.createChannel(t, "Slack"), a.createChannel(t, "Email")

	made := a.createProbe(t, map[string]any{"name": "Frankfurt", "notification_ids": []string{slack, email}})
	assert.ElementsMatch(t, []string{slack, email}, made.NotificationIDs)
	path := probesPath + "/" + made.ID
	got := decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.ElementsMatch(t, []string{slack, email}, got.NotificationIDs)

	kept := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"name": "Paris"}, a.asAdmin), http.StatusOK)
	assert.ElementsMatch(t, []string{slack, email}, kept.NotificationIDs, "an update that names no channels keeps them")

	replaced := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{email}}, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{email}, replaced.NotificationIDs)

	a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{slack, email}}, a.asAdmin)
	w := a.request(t, http.MethodDelete, "/api/v1/notification-channels/"+email, nil, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	afterDelete := decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{slack}, afterDelete.NotificationIDs, "a deleted channel is cleared from its probes")

	cleared := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{}}, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{}, cleared.NotificationIDs)
}

func TestProbesAPI_RefusesAnUnknownNotificationChannel(t *testing.T) {
	a := newAppWithAdmin(t)
	slack := a.createChannel(t, "Slack")
	made := a.createProbe(t, map[string]any{"name": "Frankfurt", "notification_ids": []string{slack}})
	unknown := uuid.NewString()

	w := a.request(t, http.MethodPost, probesPath, map[string]any{"name": "Paris", "notification_ids": []string{unknown}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, errorMessage(t, w.Body.Bytes()), unknown)
	assert.Len(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK), 2, "the failed create left no probe")

	w = a.request(t, http.MethodPut, probesPath+"/"+made.ID, map[string]any{"name": "Renamed", "notification_ids": []string{"not-an-id"}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	unchanged := decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Frankfurt", unchanged.Name)
	assert.Equal(t, []string{slack}, unchanged.NotificationIDs)
}

func TestProbesAPI_ShowsTheInstancesOfAProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	lastSeen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES ('instance-1', ?, 'host-1', 'linux', 'arm64', '1.2.3', 1, ?)`, made.ID, lastSeen)
		return err
	}))

	got := decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, []probeInstanceResponse{{ID: "instance-1", Hostname: "host-1", OS: "linux", Arch: "arm64", Version: "1.2.3", ProtocolVersion: 1, SupportedTypes: []string{}, LastSeenAt: lastSeen}}, got.Instances)
	assert.Equal(t, "offline", got.Status, "nothing speaks the probe protocol yet")
	list := decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK)
	assert.Len(t, list[1].Instances, 1)
	assert.Empty(t, list[0].Instances)
}

func TestProbesAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	path := probesPath + "/" + made.ID
	tooLong := strings.Repeat("x", 256)

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"create: malformed JSON", http.MethodPost, probesPath, "{"},
		{"create: no name", http.MethodPost, probesPath, map[string]any{"location": "eu"}},
		{"create: empty name", http.MethodPost, probesPath, map[string]any{"name": ""}},
		{"create: name too long", http.MethodPost, probesPath, map[string]any{"name": tooLong}},
		{"create: location too long", http.MethodPost, probesPath, map[string]any{"name": "Paris", "location": tooLong}},
		{"update: malformed JSON", http.MethodPut, path, "{"},
		{"update: empty name", http.MethodPut, path, map[string]any{"name": ""}},
		{"update: name too long", http.MethodPut, path, map[string]any{"name": tooLong}},
		{"update: location too long", http.MethodPut, path, map[string]any{"location": tooLong}},
	})
	assert.Equal(t, "Frankfurt", decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK).Name)
}

func TestProbesAPI_AnUnknownProbeIsNotFound(t *testing.T) {
	a := newAppWithAdmin(t)
	missing := probesPath + "/" + uuid.NewString()

	a.expectStatus(t, http.StatusNotFound, []call{
		{"get", http.MethodGet, missing, nil},
		{"update", http.MethodPut, missing, map[string]any{"name": "Paris"}},
		{"delete", http.MethodDelete, missing, nil},
		{"rotate", http.MethodPost, missing + "/rotate-token", nil},
	})
}

func TestProbesAPI_NeedsCredentials(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, c := range []call{
		{"list", http.MethodGet, probesPath, nil},
		{"create", http.MethodPost, probesPath, map[string]any{"name": "Paris"}},
		{"get", http.MethodGet, probesPath + "/local", nil},
		{"update", http.MethodPut, probesPath + "/local", map[string]any{"name": "Paris"}},
		{"delete", http.MethodDelete, probesPath + "/local", nil},
		{"rotate", http.MethodPost, probesPath + "/local/rotate-token", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, a.request(t, c.method, c.path, c.body).Code)
		})
	}
	assert.Len(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK), 1)
}

func TestProbes_DeleteWithMonitorsAnswers409(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	path := probesPath + "/" + frankfurt.ID
	first := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	second := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Search", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	a.pauseMonitor(t, second)

	w := a.request(t, http.MethodDelete, path, nil, a.asAdmin)

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "The probe can't be deleted: 2 monitors run on it. Move them to another probe or delete them first", errorMessage(t, w.Body.Bytes()),
		"a paused monitor counts, too")
	assert.Equal(t, frankfurt.probeResponse, a.getProbe(t, frankfurt.ID), "the probe stays as it was")
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, first), "and so do its monitors")
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, second))

	a.moveMonitor(t, first, "local")
	w = a.request(t, http.MethodDelete, path, nil, a.asAdmin)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "The probe can't be deleted: 1 monitor runs on it. Move them to another probe or delete them first", errorMessage(t, w.Body.Bytes()))

	decode[any](t, a.request(t, http.MethodDelete, monitorsPath+"/"+second, nil, a.asAdmin), http.StatusOK)
	w = a.request(t, http.MethodDelete, path, nil, a.asAdmin)
	assert.Equal(t, http.StatusNoContent, w.Code, "the probe goes once its last monitor is gone: "+w.Body.String())
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, first))
}
