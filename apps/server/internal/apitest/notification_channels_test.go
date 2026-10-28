package apitest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

func TestNotificationChannelsAPI_ManagesChannel(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("On-call webhook"), a.withAPIKey), http.StatusCreated).ID
	path := "/api/v1/notification-channels/" + id

	got := decode[channelResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, channelResponse{ID: id, Name: "On-call webhook", Type: "webhook", Active: true}, got)
	list := decode[[]channelResponse](t, a.request(t, http.MethodGet, "/api/v1/notification-channels", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)

	decode[any](t, a.request(t, http.MethodPut, path, webhookChannelBody("Ops webhook"), a.asAdmin), http.StatusOK)
	assert.Equal(t, "Ops webhook", decode[channelResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK).Name)
	decode[any](t, a.request(t, http.MethodPatch, path, webhookChannelBody("Pager webhook"), a.asAdmin), http.StatusOK)
	assert.Equal(t, "Pager webhook", decode[channelResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK).Name)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
}

func TestNotificationChannelsAPI_CreatesInactiveChannel(t *testing.T) {
	a := newAppWithAdmin(t)
	body := with(webhookChannelBody("Inactive webhook"), "active", false)

	created := decode[channelResponse](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", body, a.asAdmin), http.StatusCreated)
	assert.False(t, created.Active)

	got := decode[channelResponse](t, a.request(t, http.MethodGet, "/api/v1/notification-channels/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestNotificationChannelsAPI_DefaultsChannelToActiveWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	body := webhookChannelBody("Default webhook")
	delete(body, "active")

	created := decode[channelResponse](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", body, a.asAdmin), http.StatusCreated)
	assert.True(t, created.Active)

	got := decode[channelResponse](t, a.request(t, http.MethodGet, "/api/v1/notification-channels/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.True(t, got.Active)
}

func TestNotificationChannelsAPI_UpdateFullDoesNotEnableInactiveChannelWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	createBody := webhookChannelBody("Inactive webhook")
	createBody["active"] = false
	created := decode[channelResponse](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", createBody, a.asAdmin), http.StatusCreated)
	require.False(t, created.Active)

	updateBody := webhookChannelBody("Inactive webhook")
	delete(updateBody, "active")
	updated := decode[channelResponse](t, a.request(t, http.MethodPut, "/api/v1/notification-channels/"+created.ID, updateBody, a.asAdmin), http.StatusOK)
	assert.False(t, updated.Active)

	got := decode[channelResponse](t, a.request(t, http.MethodGet, "/api/v1/notification-channels/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestNotificationChannelsAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("On-call webhook"), a.asAdmin), http.StatusCreated).ID

	unsupported := with(webhookChannelBody("Carrier pigeon"), "type", "carrier-pigeon")
	badConfig := with(webhookChannelBody("No URL"), "config", `{"webhook_content_type":"json"}`)
	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/notification-channels", "{"},
		{"unsupported type", http.MethodPost, "/api/v1/notification-channels", unsupported},
		{"config fails its type", http.MethodPost, "/api/v1/notification-channels", badConfig},
		{"test with an unsupported type", http.MethodPost, "/api/v1/notification-channels/test", unsupported},
		{"full update with malformed JSON", http.MethodPut, "/api/v1/notification-channels/" + id, "{"},
		{"full update with an unsupported type", http.MethodPut, "/api/v1/notification-channels/" + id, unsupported},
		{"full update with a config that fails its type", http.MethodPut, "/api/v1/notification-channels/" + id, badConfig},
		{"negative page", http.MethodGet, "/api/v1/notification-channels?page=-1", nil},
	})

	list := decode[[]channelResponse](t, a.request(t, http.MethodGet, "/api/v1/notification-channels", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, "On-call webhook", list[0].Name)
}

func TestNotificationChannelsAPI_SendsTestNotification(t *testing.T) {
	// The webhook listens on this machine, which the cloud's run blocks
	a := newAppWithAdmin(t, withBlockPrivateTargets(false))
	received := make(chan string, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
	}))
	t.Cleanup(hook.Close)
	channel := with(webhookChannelBody("On-call webhook"), "config", fmt.Sprintf(`{"webhook_url":%q,"webhook_content_type":"json"}`, hook.URL))

	decode[any](t, a.request(t, http.MethodPost, "/api/v1/notification-channels/test", channel, a.asAdmin), http.StatusOK)

	select {
	case body := <-received:
		assert.Contains(t, body, "This is a test notification from Peekaping")
	default:
		t.Fatal("the webhook received no test notification")
	}
}
