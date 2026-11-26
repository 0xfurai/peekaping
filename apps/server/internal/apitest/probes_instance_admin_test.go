package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Only an instance admin changes a probe; a member and an API key see it without its instances'
// hosts and its channels (docs/adr/0005 section 10, PP-150 AC5)

// probeWrites are the routes that change a probe, for the probe with the id
func probeWrites(id string) []call {
	path := probesPath + "/" + id
	return []call{
		{"create", http.MethodPost, probesPath, map[string]any{"name": "Paris"}},
		{"update", http.MethodPut, path, map[string]any{"name": "Renamed"}},
		{"link channels", http.MethodPut, path, map[string]any{"notification_ids": []string{}}},
		{"rotate the token", http.MethodPost, path + "/rotate-token", nil},
		{"assign all monitors", http.MethodPost, path + "/assign-all-monitors", nil},
		{"delete", http.MethodDelete, path, nil},
	}
}

func TestProbesAPI_OnlyAnInstanceAdminChangesAProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	// An owner of a workspace is not an instance admin
	owner := a.userIn(t, "owner@example.com", a.firstWorkspace(t), workspace.RoleOwner)

	for _, who := range []struct {
		name  string
		creds credentials
	}{{"a workspace owner", owner}, {"an API key", a.withAPIKey}} {
		for _, route := range probeWrites(made.ID) {
			t.Run(who.name+" can not "+route.name, func(t *testing.T) {
				w := a.request(t, route.method, route.path, route.body, who.creds)
				assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			})
		}
	}
	// and nothing changed
	assert.Equal(t, made.probeResponse, decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/"+made.ID, nil, a.asAdmin), http.StatusOK))
	assert.Len(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK), 2)

	t.Run("no credentials", func(t *testing.T) {
		for _, route := range probeWrites(made.ID) {
			assert.Equal(t, http.StatusUnauthorized, a.request(t, route.method, route.path, route.body).Code, route.name)
		}
	})
}

func TestProbesAPI_AnInstanceAdminChangesAProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	path := probesPath + "/" + made.ID
	// A user in no workspace at all can be an instance admin too
	a.setInstanceAdmin(t, a.createUser(t, "ops@example.com"), true)
	ops := a.loginAs(t, "ops@example.com")

	for _, who := range []struct {
		name  string
		creds credentials
	}{{"the first user", a.asAdmin}, {"a user given the flag", ops}} {
		t.Run(who.name, func(t *testing.T) {
			created := decode[probeWithToken](t, a.request(t, http.MethodPost, probesPath, map[string]any{"name": "Paris"}, who.creds), http.StatusCreated)
			createdPath := probesPath + "/" + created.ID
			decode[probeResponse](t, a.request(t, http.MethodPut, createdPath, map[string]any{"name": "Lyon", "location": "eu-west"}, who.creds), http.StatusOK)
			rotated := decode[probeWithToken](t, a.request(t, http.MethodPost, createdPath+"/rotate-token", nil, who.creds), http.StatusOK)
			assert.NotEqual(t, created.Token, rotated.Token)
			decode[struct {
				Pending bool `json:"pending"`
			}](t, a.request(t, http.MethodPost, createdPath+"/assign-all-monitors", nil, who.creds), http.StatusOK)
			assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, createdPath, nil, who.creds).Code)
		})
	}

	// The flag is read on every request
	a.setInstanceAdmin(t, a.userID(t, adminEmail), false)
	assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodPut, path, map[string]any{"name": "Renamed"}, a.asAdmin).Code)
	a.setInstanceAdmin(t, a.userID(t, adminEmail), true)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPut, path, map[string]any{"name": "Renamed"}, a.asAdmin).Code)
}

// probeWithInstance is a probe with a channel and an online instance that runs http and tcp
func (a *app) probeWithInstance(t *testing.T) (probeWithToken, string) {
	t.Helper()
	channel := a.createChannel(t, "Ops")
	made := a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "eu-central", "notification_ids": []string{channel}})
	require.NoError(t, a.container.Invoke(func(svc probe.Service) error {
		ctx := context.Background()
		if err := svc.RegisterInstance(ctx, made.ID, &probe.Instance{
			ID: "instance-1", Hostname: "fra-1", OS: "linux", Arch: "arm64", Version: "1.4.0", ProtocolVersion: 2,
			SupportedTypes: []string{"http", "tcp"}, LastSeenAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		_, err := svc.MarkOnline(ctx, made.ID, time.Now().UTC())
		return err
	}))
	return made, channel
}

func TestProbesAPI_AMemberSeesAProbeWithoutItsHostsAndChannels(t *testing.T) {
	a := newAppWithAdmin(t)
	made, channel := a.probeWithInstance(t)
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)
	path := probesPath + "/" + made.ID

	// shows is what every reader sees; hidden is what only an instance admin does
	shows := func(t *testing.T, got probeResponse) {
		t.Helper()
		assert.Equal(t, made.ID, got.ID)
		assert.Equal(t, "Frankfurt", got.Name)
		assert.Equal(t, "eu-central", got.Location)
		assert.Equal(t, "online", got.Status)
		assert.False(t, got.Disabled)
		assert.ElementsMatch(t, []string{"http", "tcp"}, got.SupportedTypes)
		require.Len(t, got.Instances, 1, "the instance is counted")
		assert.Equal(t, []string{"http", "tcp"}, got.Instances[0].SupportedTypes)
	}
	hidden := func(t *testing.T, got probeResponse) {
		t.Helper()
		assert.Equal(t, []string{}, got.NotificationIDs, "no channel IDs")
		assert.Equal(t, probeInstanceResponse{ID: "instance-1", SupportedTypes: []string{"http", "tcp"}, LastSeenAt: got.Instances[0].LastSeenAt}, got.Instances[0], "no hostname, OS, arch, version or protocol version")
	}
	visible := func(t *testing.T, got probeResponse) {
		t.Helper()
		assert.Equal(t, []string{channel}, got.NotificationIDs)
		assert.Equal(t, "fra-1", got.Instances[0].Hostname)
		assert.Equal(t, "linux", got.Instances[0].OS)
		assert.Equal(t, "arm64", got.Instances[0].Arch)
		assert.Equal(t, "1.4.0", got.Instances[0].Version)
		assert.Equal(t, 2, got.Instances[0].ProtocolVersion)
	}
	find := func(t *testing.T, list []probeResponse) probeResponse {
		t.Helper()
		for _, p := range list {
			if p.ID == made.ID {
				return p
			}
		}
		require.Fail(t, "the probe is not listed")
		return probeResponse{}
	}

	for _, who := range []struct {
		name  string
		creds credentials
		check func(*testing.T, probeResponse)
	}{{"a member", member, hidden}, {"an API key", a.withAPIKey, hidden}, {"the instance admin", a.asAdmin, visible}} {
		t.Run(who.name+" gets the probe", func(t *testing.T) {
			got := decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, who.creds), http.StatusOK)
			shows(t, got)
			who.check(t, got)
		})
		t.Run(who.name+" lists the probes", func(t *testing.T) {
			got := find(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, who.creds), http.StatusOK))
			shows(t, got)
			who.check(t, got)
		})
		t.Run(who.name+" gets the availability", func(t *testing.T) {
			got := decode[probeAvailability](t, a.request(t, http.MethodGet, path+"/availability", nil, who.creds), http.StatusOK)
			require.Len(t, got.Instances, 1)
			assert.Equal(t, "instance-1", got.Instances[0].ID)
			if who.name == "the instance admin" {
				assert.Equal(t, "fra-1", got.Instances[0].Hostname)
				assert.Equal(t, "1.4.0", got.Instances[0].Version)
			} else {
				assert.Empty(t, got.Instances[0].Hostname)
				assert.Empty(t, got.Instances[0].Version)
			}
		})
	}
}
