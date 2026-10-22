package notification_channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// channelStore is a service that keeps channels in memory. It hands out the channels it holds,
// not copies, so that a handler that changes one changes what is stored.
type channelStore struct {
	Service
	channels map[string]*Model
}

func (s *channelStore) Create(_ context.Context, entity *CreateUpdateDto) (*Model, error) {
	created := &Model{ID: "created", Name: entity.Name, Type: entity.Type, Active: entity.Active, IsDefault: entity.IsDefault, Config: &entity.Config}
	s.channels[created.ID] = created
	return created, nil
}

func (s *channelStore) FindByID(_ context.Context, id string) (*Model, error) {
	return s.channels[id], nil
}

func (s *channelStore) UpdateFull(_ context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	s.channels[id] = &Model{ID: id, Name: entity.Name, Type: entity.Type, Active: entity.Active, IsDefault: entity.IsDefault, Config: &entity.Config}
	return s.channels[id], nil
}

func (s *channelStore) UpdatePartial(_ context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	channel := *s.channels[id]
	if entity.Name != nil {
		channel.Name = *entity.Name
	}
	if entity.Type != nil {
		channel.Type = *entity.Type
	}
	if entity.Config != nil {
		channel.Config = entity.Config
	}
	s.channels[id] = &channel
	return &channel, nil
}

type secretsFixture struct {
	t      *testing.T
	store  *channelStore
	router *httpx.Router
	// provider is the one of test-secrets and test-other-secrets
	provider *secretTestProvider
}

// newSecretsFixture serves the channel routes over a store with one channel, channel-1, of type
// test-secrets, saved with config
func newSecretsFixture(t *testing.T, config string) *secretsFixture {
	t.Helper()
	providers, provider := newSecretTestRegistry()
	store := &channelStore{channels: map[string]*Model{
		"channel-1": {ID: "channel-1", Name: "Alerts", Type: "test-secrets", Active: true, Config: &config},
	}}
	controller := NewController(store, providers, zap.NewNop().Sugar())
	router := httpx.NewRouter()
	ops := huma.Operation{Errors: []int{400, 404, 500}}
	httpx.Register(router, "POST", "/", ops, controller.Create)
	httpx.Register(router, "POST", "/test", ops, controller.Test)
	httpx.Register(router, "GET", "/{id}", ops, controller.FindByID)
	httpx.Register(router, "PUT", "/{id}", ops, controller.UpdateFull)
	httpx.Register(router, "PATCH", "/{id}", ops, controller.UpdatePartial)
	return &secretsFixture{t: t, store: store, router: router, provider: provider}
}

func (f *secretsFixture) do(method, path, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// stored returns the config the store holds for a channel
func (f *secretsFixture) stored(id string) string {
	f.t.Helper()
	channel := f.store.channels[id]
	require.NotNil(f.t, channel, id)
	require.NotNil(f.t, channel.Config, id)
	return *channel.Config
}

// putBody is the body of a full update of a channel of the given type
func putBody(channelType, config string) string {
	return `{"name":"Alerts","type":"` + channelType + `","active":true,"config":` + quote(config) + `}`
}

// quote returns s as a JSON string
func quote(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(quoted)
}

const storedChannelConfig = `{"url":"https://old.example","server":"https://server.example","token":"stored-token","password":"stored-password"}`

// storedChannelConfigWithHook is the same channel with a hook URL, which is a secret and an endpoint
const storedChannelConfigWithHook = `{"url":"https://old.example","server":"https://server.example","token":"stored-token","password":"stored-password","hook_url":"https://hook.example/stored"}`

// cmpOr returns value, or fallback when value is empty
func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// secretsNeverShown are saved secrets and new ones that no response may show
var secretsNeverShown = []string{"stored-token", "stored-password", "https://hook.example/stored", "fresh-token", "fresh-password", "https://hook.example/fresh"}

func requireNoSecretsShown(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, secret := range secretsNeverShown {
		assert.NotContains(t, rec.Body.String(), secret)
	}
}

func TestControllerUpdateFullResolvesSecrets(t *testing.T) {
	cases := []struct {
		name        string
		channelType string
		config      string
		// saved is the config of the channel before, storedChannelConfig when empty
		saved      string
		wantStatus int
		// wantStored is the config the store holds afterwards
		wantStored string
		// wantShown is the config in the response
		wantShown string
		// wantError is part of the error message
		wantError string
	}{
		{
			name:        "placeholders keep the stored secrets",
			channelType: "test-secrets",
			config:      `{"url":"https://new.example","server":"https://server.example","token":"********","password":"********","hook_url":"********"}`,
			saved:       storedChannelConfigWithHook,
			wantStatus:  http.StatusOK,
			wantStored:  `{"url":"https://new.example","server":"https://server.example","token":"stored-token","password":"stored-password","hook_url":"https://hook.example/stored"}`,
			wantShown:   `{"url":"https://new.example","server":"https://server.example","token":"********","password":"********","hook_url":"********"}`,
		},
		{
			name:        "a new value replaces the stored secret",
			channelType: "test-secrets",
			config:      `{"url":"https://old.example","server":"https://server.example","token":"fresh-token","password":"********"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"url":"https://old.example","server":"https://server.example","token":"fresh-token","password":"stored-password"}`,
			wantShown:   `{"url":"https://old.example","server":"https://server.example","token":"********","password":"********"}`,
		},
		{
			name:        "an empty value clears an optional secret",
			channelType: "test-secrets",
			config:      `{"server":"https://server.example","token":"********","password":""}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://server.example","token":"stored-token","password":""}`,
			wantShown:   `{"server":"https://server.example","token":"********","password":""}`,
		},
		{
			name:        "an omitted secret is cleared, as in any full update",
			channelType: "test-secrets",
			config:      `{"server":"https://server.example","token":"********"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://server.example","token":"stored-token"}`,
			wantShown:   `{"server":"https://server.example","token":"********"}`,
		},
		{
			name:        "a placeholder keeps the secret under a key of another letter case",
			channelType: "test-secrets",
			config:      `{"server":"https://server.example","Token":"********"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://server.example","Token":"stored-token"}`,
			wantShown:   `{"server":"https://server.example","Token":"********"}`,
		},
		{
			name:        "a new secret endpoint with new secrets is saved",
			channelType: "test-secrets",
			config:      `{"server":"https://server.example","token":"fresh-token","hook_url":"https://hook.example/fresh"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://server.example","token":"fresh-token","hook_url":"https://hook.example/fresh"}`,
			wantShown:   `{"server":"https://server.example","token":"********","hook_url":"********"}`,
		},
		{
			name:        "a changed endpoint refuses the placeholders",
			channelType: "test-secrets",
			config:      `{"server":"https://elsewhere.example","token":"********","password":"********"}`,
			wantStatus:  http.StatusBadRequest,
			wantStored:  storedChannelConfig,
			wantError:   "Invalid config: re-enter the saved secrets (password, token) when changing the type or where the channel sends",
		},
		{
			name:        "a changed endpoint takes new values",
			channelType: "test-secrets",
			config:      `{"server":"https://elsewhere.example","token":"fresh-token","password":"fresh-password"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://elsewhere.example","token":"fresh-token","password":"fresh-password"}`,
			wantShown:   `{"server":"https://elsewhere.example","token":"********","password":"********"}`,
		},
		{
			name:        "a changed secret endpoint refuses the other placeholders",
			channelType: "test-secrets",
			config:      `{"server":"https://server.example","token":"********","hook_url":"https://hook.example/fresh"}`,
			saved:       storedChannelConfigWithHook,
			wantStatus:  http.StatusBadRequest,
			wantStored:  storedChannelConfigWithHook,
			wantError:   "(token)",
		},
		{
			name:        "a changed type refuses the placeholders",
			channelType: "test-other-secrets",
			config:      `{"server":"https://server.example","token":"fresh-token","password":"********"}`,
			wantStatus:  http.StatusBadRequest,
			wantStored:  storedChannelConfig,
			wantError:   "(password)",
		},
		{
			name:        "a changed type takes new values",
			channelType: "test-other-secrets",
			config:      `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`,
			wantStatus:  http.StatusOK,
			wantStored:  `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`,
			wantShown:   `{"server":"https://server.example","token":"********","password":"********"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, cmpOr(tc.saved, storedChannelConfig))

			rec := f.do(http.MethodPut, "/channel-1", putBody(tc.channelType, tc.config))

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			requireSameConfig(t, tc.wantStored, f.stored("channel-1"))
			requireNoSecretsShown(t, rec)
			assert.Contains(t, rec.Body.String(), tc.wantError)
			if tc.wantStatus == http.StatusOK {
				requireSameConfig(t, tc.wantShown, shownConfig(t, rec))
			}
		})
	}
}

func TestControllerUpdatePartialResolvesSecrets(t *testing.T) {
	cases := []struct {
		name string
		body string
		// saved is the config of the channel before, storedChannelConfig when empty
		saved      string
		wantStatus int
		wantStored string
		wantShown  string
		wantError  string
	}{
		{
			name:       "placeholders keep the stored secrets",
			body:       `{"config":` + quote(`{"url":"https://new.example","server":"https://server.example","token":"********","password":"********","hook_url":"********"}`) + `}`,
			saved:      storedChannelConfigWithHook,
			wantStatus: http.StatusOK,
			wantStored: `{"url":"https://new.example","server":"https://server.example","token":"stored-token","password":"stored-password","hook_url":"https://hook.example/stored"}`,
			wantShown:  `{"url":"https://new.example","server":"https://server.example","token":"********","password":"********","hook_url":"********"}`,
		},
		{
			name:       "a new value replaces the stored secret",
			body:       `{"config":` + quote(`{"server":"https://server.example","token":"********","password":"fresh-password"}`) + `}`,
			wantStatus: http.StatusOK,
			wantStored: `{"server":"https://server.example","token":"stored-token","password":"fresh-password"}`,
			wantShown:  `{"server":"https://server.example","token":"********","password":"********"}`,
		},
		{
			name:       "an empty value clears an optional secret",
			body:       `{"config":` + quote(`{"server":"https://server.example","token":"********","password":""}`) + `}`,
			wantStatus: http.StatusOK,
			wantStored: `{"server":"https://server.example","token":"stored-token","password":""}`,
			wantShown:  `{"server":"https://server.example","token":"********","password":""}`,
		},
		{
			name:       "a request without a config keeps the stored config",
			body:       `{"name":"Renamed"}`,
			saved:      storedChannelConfigWithHook,
			wantStatus: http.StatusOK,
			wantStored: storedChannelConfigWithHook,
			wantShown:  `{"url":"https://old.example","server":"https://server.example","token":"********","password":"********","hook_url":"********"}`,
		},
		{
			name:       "naming the stored type again keeps the secrets",
			body:       `{"type":"test-secrets","config":` + quote(`{"server":"https://server.example","token":"********"}`) + `}`,
			wantStatus: http.StatusOK,
			wantStored: `{"server":"https://server.example","token":"stored-token"}`,
			wantShown:  `{"server":"https://server.example","token":"********"}`,
		},
		{
			name:       "a changed endpoint refuses the placeholders",
			body:       `{"config":` + quote(`{"server":"https://elsewhere.example","token":"********","password":"********"}`) + `}`,
			wantStatus: http.StatusBadRequest,
			wantStored: storedChannelConfig,
			wantError:  "Invalid config: re-enter the saved secrets (password, token) when changing the type or where the channel sends",
		},
		{
			name:       "a changed endpoint takes new values",
			body:       `{"config":` + quote(`{"server":"https://elsewhere.example","token":"fresh-token"}`) + `}`,
			wantStatus: http.StatusOK,
			wantStored: `{"server":"https://elsewhere.example","token":"fresh-token"}`,
			wantShown:  `{"server":"https://elsewhere.example","token":"********"}`,
		},
		{
			name:       "a changed secret endpoint refuses the other placeholders",
			body:       `{"config":` + quote(`{"server":"https://server.example","token":"********","hook_url":"https://hook.example/fresh"}`) + `}`,
			saved:      storedChannelConfigWithHook,
			wantStatus: http.StatusBadRequest,
			wantStored: storedChannelConfigWithHook,
			wantError:  "(token)",
		},
		{
			name:       "a changed type refuses the placeholders",
			body:       `{"type":"test-other-secrets","config":` + quote(`{"server":"https://server.example","token":"fresh-token","password":"********"}`) + `}`,
			wantStatus: http.StatusBadRequest,
			wantStored: storedChannelConfig,
			wantError:  "(password)",
		},
		{
			name:       "a changed type takes new values",
			body:       `{"type":"test-other-secrets","config":` + quote(`{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`) + `}`,
			wantStatus: http.StatusOK,
			wantStored: `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`,
			wantShown:  `{"server":"https://server.example","token":"********","password":"********"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, cmpOr(tc.saved, storedChannelConfig))

			rec := f.do(http.MethodPatch, "/channel-1", tc.body)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			requireSameConfig(t, tc.wantStored, f.stored("channel-1"))
			requireNoSecretsShown(t, rec)
			assert.Contains(t, rec.Body.String(), tc.wantError)
			if tc.wantStatus == http.StatusOK {
				requireSameConfig(t, tc.wantShown, shownConfig(t, rec))
			}
		})
	}
}

func TestControllerCreateRefusesPlaceholders(t *testing.T) {
	for _, config := range []string{
		`{"server":"https://server.example","token":"********"}`,
		`{"server":"https://server.example","token":"fresh-token","password":"********"}`,
		`{"server":"https://server.example","token":"fresh-token","hook_url":"********"}`,
	} {
		t.Run(config, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)

			rec := f.do(http.MethodPost, "/", putBody("test-secrets", config))

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), "Invalid config: re-enter the saved secrets (")
			assert.NotContains(t, f.store.channels, "created")
		})
	}

	t.Run("new values are saved", func(t *testing.T) {
		f := newSecretsFixture(t, storedChannelConfig)

		rec := f.do(http.MethodPost, "/", putBody("test-secrets", `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`))

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		requireSameConfig(t, `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`, f.stored("created"))
		requireSameConfig(t, `{"server":"https://server.example","token":"********","password":"********"}`, shownConfig(t, rec))
		requireNoSecretsShown(t, rec)
	})
}

// testBody is the body of a request to test a channel of the given type. id is the saved channel
// the request edits; it is left out when empty.
func testBody(id, channelType, config string) string {
	body := `{"name":"Alerts","type":"` + channelType + `","active":true,"config":` + quote(config)
	if id != "" {
		body += `,"id":` + quote(id)
	}
	return body + `}`
}

func TestControllerTestResolvesSecrets(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		// wantSent is the config the provider sent with, when it sent
		wantSent string
		// wantError is part of the error message
		wantError string
	}{
		{
			name:       "placeholders send with the stored secrets",
			body:       testBody("channel-1", "test-secrets", `{"url":"https://new.example","server":"https://server.example","token":"********","password":"********"}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"url":"https://new.example","server":"https://server.example","token":"stored-token","password":"stored-password"}`,
		},
		{
			name:       "a new value is sent instead of the stored secret",
			body:       testBody("channel-1", "test-secrets", `{"server":"https://server.example","token":"fresh-token","password":"********"}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"server":"https://server.example","token":"fresh-token","password":"stored-password"}`,
		},
		{
			name:       "an empty value is sent empty",
			body:       testBody("channel-1", "test-secrets", `{"server":"https://server.example","token":"********","password":""}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"server":"https://server.example","token":"stored-token","password":""}`,
		},
		{
			name:       "a changed endpoint refuses the placeholders",
			body:       testBody("channel-1", "test-secrets", `{"server":"https://elsewhere.example","token":"********","password":"********"}`),
			wantStatus: http.StatusBadRequest,
			wantError:  "Invalid config: re-enter the saved secrets (password, token) when changing the type or where the channel sends",
		},
		{
			name:       "a changed endpoint takes new values",
			body:       testBody("channel-1", "test-secrets", `{"server":"https://elsewhere.example","token":"fresh-token"}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"server":"https://elsewhere.example","token":"fresh-token"}`,
		},
		{
			name:       "a changed type refuses the placeholders",
			body:       testBody("channel-1", "test-other-secrets", `{"server":"https://server.example","token":"fresh-token","password":"********"}`),
			wantStatus: http.StatusBadRequest,
			wantError:  "(password)",
		},
		{
			name:       "a changed type takes new values",
			body:       testBody("channel-1", "test-other-secrets", `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`,
		},
		{
			name:       "without an id a placeholder is refused",
			body:       testBody("", "test-secrets", `{"server":"https://server.example","token":"********"}`),
			wantStatus: http.StatusBadRequest,
			wantError:  "Invalid config: re-enter the saved secrets (token) when changing the type or where the channel sends",
		},
		{
			name:       "without an id an optional secret's placeholder is refused too",
			body:       testBody("", "test-secrets", `{"server":"https://server.example","token":"fresh-token","password":"********"}`),
			wantStatus: http.StatusBadRequest,
			wantError:  "(password)",
		},
		{
			name:       "without an id new values are sent",
			body:       testBody("", "test-secrets", `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`),
			wantStatus: http.StatusOK,
			wantSent:   `{"server":"https://server.example","token":"fresh-token","password":"fresh-password"}`,
		},
		{
			name:       "a channel that doesn't exist is not found",
			body:       testBody("channel-2", "test-secrets", `{"server":"https://server.example","token":"********"}`),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "an id that isn't a string is refused",
			body:       `{"name":"Alerts","type":"test-secrets","config":"{\"token\":\"fresh-token\"}","id":7}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)

			rec := f.do(http.MethodPost, "/test", tc.body)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantSent == "" {
				assert.Empty(t, f.provider.sent, "nothing is sent")
			} else if assert.Len(t, f.provider.sent, 1) {
				requireSameConfig(t, tc.wantSent, f.provider.sent[0])
			}
			requireNoSecretsShown(t, rec)
			assert.Contains(t, rec.Body.String(), tc.wantError)
			assert.Equal(t, storedChannelConfig, f.stored("channel-1"), "testing a channel doesn't change it")
		})
	}
}

// shownConfig returns the config of the channel in a response
func shownConfig(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Data struct {
			Config string `json:"config"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
	return envelope.Data.Config
}
