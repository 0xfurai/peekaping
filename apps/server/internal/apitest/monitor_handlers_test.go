package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/modules/workspace"
	"sync"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// recordingMonitorService is the monitor service, noting which of its methods are called.
type recordingMonitorService struct {
	monitor.Service

	mu    sync.Mutex
	calls []string
}

func (s *recordingMonitorService) record(method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, method)
}

// take returns the calls made since the last take.
func (s *recordingMonitorService) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	calls := s.calls
	s.calls = nil
	return calls
}

func (s *recordingMonitorService) ValidateMonitorConfig(monitorType, configJSON string) error {
	s.record("ValidateMonitorConfig")
	return s.Service.ValidateMonitorConfig(monitorType, configJSON)
}

func (s *recordingMonitorService) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*monitor.Model, error) {
	s.record("FindByID")
	return s.Service.FindByID(ctx, workspaceID, id)
}

func (s *recordingMonitorService) Create(ctx context.Context, workspaceID workspace.ID, dto *monitor.CreateUpdateDto) (*monitor.Model, error) {
	s.record("Create")
	return s.Service.Create(ctx, workspaceID, dto)
}

func (s *recordingMonitorService) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, dto *monitor.CreateUpdateDto) (*monitor.Model, error) {
	s.record("UpdateFull")
	return s.Service.UpdateFull(ctx, workspaceID, id, dto)
}

func (s *recordingMonitorService) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, dto *monitor.PartialUpdateDto, noPublish bool) (*monitor.Model, error) {
	s.record("UpdatePartial")
	return s.Service.UpdatePartial(ctx, workspaceID, id, dto, noPublish)
}

func (s *recordingMonitorService) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	s.record("Delete")
	return s.Service.Delete(ctx, workspaceID, id)
}

// failMessage returns the message of an error response.
func failMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope utils.ApiResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())
	return envelope.Message
}

// A handler binds and validates its request, calls one service method that does the work, and
// maps the error. What a request needs from the stored monitor is read by that method, in the same
// transaction as the write. The one exception is the config of a full monitor (POST and PUT): it
// is checked against the request alone, so the handler validates it through the service first.
func TestMonitorsAPI_HandlersCallOneServiceMethodToWrite(t *testing.T) {
	service := &recordingMonitorService{}
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(s monitor.Service) monitor.Service {
			service.Service = s
			return service
		})
	})
	a.registerAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-token")
	const missingID = "00000000-0000-4000-8000-000000000000"

	for _, tc := range []struct {
		name         string
		method, path string
		body         any
		status       int
		calls        []string
	}{
		{"create", http.MethodPost, "/api/v1/monitors", pushMonitorBody("Search", "search-token"), http.StatusCreated,
			[]string{"ValidateMonitorConfig", "Create"}},
		{"full update", http.MethodPut, "/api/v1/monitors/" + id, pushMonitorBody("Checkout API", "checkout-token"), http.StatusOK,
			[]string{"ValidateMonitorConfig", "UpdateFull"}},
		{"partial update", http.MethodPatch, "/api/v1/monitors/" + id, map[string]any{"name": "Checkout v2", "config": `{"pushToken":"checkout-token"}`}, http.StatusOK,
			[]string{"UpdatePartial"}},
		{"partial update with a bad config", http.MethodPatch, "/api/v1/monitors/" + id, map[string]any{"config": "{}"}, http.StatusBadRequest,
			[]string{"UpdatePartial"}},
		{"partial update of a missing monitor", http.MethodPatch, "/api/v1/monitors/" + missingID, map[string]any{"name": "Gone"}, http.StatusNotFound,
			[]string{"UpdatePartial"}},
		{"delete of a missing monitor", http.MethodDelete, "/api/v1/monitors/" + missingID, nil, http.StatusNotFound,
			[]string{"Delete"}},
		{"delete", http.MethodDelete, "/api/v1/monitors/" + id, nil, http.StatusOK,
			[]string{"Delete"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service.take()

			w := a.request(t, tc.method, tc.path, tc.body, a.asAdmin)

			assert.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, tc.calls, service.take())
		})
	}
}

func TestMonitorsAPI_DeleteOfAMissingMonitorMessage(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodDelete, "/api/v1/monitors/00000000-0000-4000-8000-000000000000", nil, a.asAdmin)

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, "Monitor not found", failMessage(t, w))
}

func TestMonitorsAPI_PartialUpdateErrorMessages(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-token")

	w := a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, map[string]any{"config": "{}"}, a.asAdmin)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "Invalid monitor configuration: Key: 'PushConfig.PushToken' Error:Field validation for 'PushToken' failed on the 'required' tag", failMessage(t, w))

	w = a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, map[string]any{"type": "http"}, a.asAdmin)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, failMessage(t, w), "Invalid monitor configuration: ", "the config the monitor has is checked against the new type")

	w = a.request(t, http.MethodPatch, "/api/v1/monitors/00000000-0000-4000-8000-000000000000", map[string]any{"name": "Gone"}, a.asAdmin)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, "Monitor not found", failMessage(t, w))
}
