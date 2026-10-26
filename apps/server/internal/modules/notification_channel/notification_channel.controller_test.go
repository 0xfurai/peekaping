package notification_channel

import (
	"context"
	"errors"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/notification_channel/providers"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type validatingProvider struct {
	NotificationChannelProvider
	err error
}

func (p validatingProvider) Validate(string) error {
	return p.err
}

func (validatingProvider) Unmarshal(string) (any, error) { return nil, nil }

type updateRecordingService struct {
	Service
	saved   *Model
	updated bool
	patch   *PartialUpdateDto
}

func (s *updateRecordingService) FindByID(_ context.Context, _ string) (*Model, error) {
	return s.saved, nil
}

func (s *updateRecordingService) UpdateFull(_ context.Context, _ string, _ *CreateUpdateDto) (*Model, error) {
	s.updated = true
	return &Model{}, nil
}

func (s *updateRecordingService) UpdatePartial(_ context.Context, _ string, dto *PartialUpdateDto) (*Model, error) {
	s.updated = true
	s.patch = dto
	return &Model{}, nil
}

func TestControllerUpdateValidatesConfig(t *testing.T) {
	providers := newTestRegistry(map[string]NotificationChannelProvider{
		"test-valid":     validatingProvider{},
		"test-rejecting": validatingProvider{err: errors.New("bad config")},
	})

	cases := []struct {
		name         string
		channelType  string
		wantStatus   int
		wantResponse string
		wantSaved    bool
	}{
		{"valid config", "test-valid", http.StatusOK, "notification updated successfully", true},
		{"unsupported type", "test-missing", http.StatusBadRequest, "Unsupported notification type", false},
		{"invalid config", "test-rejecting", http.StatusBadRequest, "Invalid config: bad config", false},
	}

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		for _, tc := range cases {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				service := &updateRecordingService{saved: &Model{Type: "test-valid", Config: stringPtr("{}")}}
				controller := NewController(service, providers, zap.NewNop().Sugar())
				router := httpx.NewRouter()
				httpx.Register(router, "PUT", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, controller.UpdateFull)
				httpx.Register(router, "PATCH", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, controller.UpdatePartial)

				body := `{"name": "Alerts", "type": "` + tc.channelType + `", "config": "{}"}`
				req := httptest.NewRequest(method, "/channel-1", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Equal(t, tc.wantStatus, rec.Code)
				assert.Contains(t, rec.Body.String(), tc.wantResponse)
				assert.Equal(t, tc.wantSaved, service.updated)
			})
		}
	}
}

func TestControllerUpdatePartialMergesSavedChannel(t *testing.T) {
	providers := newTestRegistry(map[string]NotificationChannelProvider{
		"test-valid":     validatingProvider{},
		"test-rejecting": validatingProvider{err: errors.New("bad config")},
	})

	cases := []struct {
		name         string
		saved        *Model
		body         string
		wantStatus   int
		wantResponse string
		wantSaved    bool
	}{
		{"name only skips validation", &Model{Type: "test-rejecting", Config: stringPtr("{}")}, `{"name": "Renamed"}`, http.StatusOK, "notification updated successfully", true},
		{"active only skips validation", &Model{Type: "test-rejecting", Config: stringPtr("{}")}, `{"active": false}`, http.StatusOK, "notification updated successfully", true},
		{"config checked against saved type", &Model{Type: "test-rejecting", Config: stringPtr("{}")}, `{"config": "{}"}`, http.StatusBadRequest, "Invalid config: bad config", false},
		{"config accepted by saved type", &Model{Type: "test-valid", Config: stringPtr("{}")}, `{"config": "{}"}`, http.StatusOK, "notification updated successfully", true},
		{"type checked with saved config", &Model{Type: "test-valid", Config: stringPtr("{}")}, `{"type": "test-missing"}`, http.StatusBadRequest, "Unsupported notification type", false},
		{"channel not found", nil, `{"config": "{}"}`, http.StatusNotFound, "Notification channel not found", false},
		{"name only on a missing channel", nil, `{"name": "Renamed"}`, http.StatusNotFound, "Notification channel not found", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &updateRecordingService{saved: tc.saved}
			router := httpx.NewRouter()
			httpx.Register(router, "PATCH", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, NewController(service, providers, zap.NewNop().Sugar()).UpdatePartial)

			req := httptest.NewRequest(http.MethodPatch, "/channel-1", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantResponse)
			assert.Equal(t, tc.wantSaved, service.updated)
		})
	}

	t.Run("omitted fields reach the service as nil", func(t *testing.T) {
		service := &updateRecordingService{saved: &Model{Type: "test-valid", Config: stringPtr("{}")}}
		router := httpx.NewRouter()
		httpx.Register(router, "PATCH", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, NewController(service, providers, zap.NewNop().Sugar()).UpdatePartial)

		req := httptest.NewRequest(http.MethodPatch, "/channel-1", strings.NewReader(`{"name": "Renamed"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(httptest.NewRecorder(), req)

		if assert.NotNil(t, service.patch) {
			assert.Equal(t, "Renamed", *service.patch.Name)
			assert.Nil(t, service.patch.Type)
			assert.Nil(t, service.patch.Active)
			assert.Nil(t, service.patch.IsDefault)
			assert.Nil(t, service.patch.Config)
		}
	})

	t.Run("active only reaches the service alone", func(t *testing.T) {
		service := &updateRecordingService{saved: &Model{Type: "test-valid", Config: stringPtr("{}")}}
		router := httpx.NewRouter()
		httpx.Register(router, "PATCH", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, NewController(service, providers, zap.NewNop().Sugar()).UpdatePartial)

		req := httptest.NewRequest(http.MethodPatch, "/channel-1", strings.NewReader(`{"active": false}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		if assert.NotNil(t, service.patch) && assert.NotNil(t, service.patch.Active, "active: false is a value, not an omitted field") {
			assert.False(t, *service.patch.Active)
		}
		if assert.NotNil(t, service.patch) {
			assert.Nil(t, service.patch.Name)
			assert.Nil(t, service.patch.Type)
			assert.Nil(t, service.patch.IsDefault)
			assert.Nil(t, service.patch.Config)
		}
	})
}

func TestControllerTestSendsWithADeadline(t *testing.T) {
	var hasDeadline bool
	providers := newTestRegistry(map[string]NotificationChannelProvider{
		"test-valid": &recordingProvider{send: func(ctx context.Context) error {
			_, hasDeadline = ctx.Deadline()
			return nil
		}},
	})
	router := httpx.NewRouter()
	httpx.Register(router, "POST", "/test", huma.Operation{Errors: []int{400, 404, 500}}, NewController(nil, providers, zap.NewNop().Sugar()).Test)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name": "Alerts", "type": "test-valid", "config": "{}"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, hasDeadline, "a test notification must stop at a deadline, like any other send")
}

func TestControllerTestKeepsTheTelegramTokenOutOfAFailedSend(t *testing.T) {
	// A closed server refuses the connection, so the send fails with a *url.Error
	// that carries the request URL, and the Telegram bot token is part of that URL
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	registry := newTestRegistry(map[string]NotificationChannelProvider{
		"telegram": providers.NewTelegramSender(zap.NewNop().Sugar(), providers.NewHTTPClient()),
	})
	router := httpx.NewRouter()
	httpx.Register(router, "POST", "/test", huma.Operation{Errors: []int{400, 404, 500}}, NewController(nil, registry, zap.NewNop().Sugar()).Test)

	const botToken = "123456:secret-bot-token"
	body := `{"name": "Alerts", "type": "telegram", "config": "{\"bot_token\": \"` + botToken + `\", \"chat_id\": \"42\", \"server_url\": \"` + server.URL + `\"}"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Failed to send test notification")
	assert.NotContains(t, rec.Body.String(), botToken)
}
