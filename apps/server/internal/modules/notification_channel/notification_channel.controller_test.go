package notification_channel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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

type updateRecordingService struct {
	Service
	updated bool
}

func (s *updateRecordingService) UpdateFull(_ context.Context, _ string, _ *CreateUpdateDto) (*Model, error) {
	s.updated = true
	return &Model{}, nil
}

func TestControllerUpdateFullValidatesConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	RegisterNotificationChannelProvider("test-valid", validatingProvider{})
	RegisterNotificationChannelProvider("test-rejecting", validatingProvider{err: errors.New("bad config")})
	defer delete(NotificationChannelProviderRegistry, "test-valid")
	defer delete(NotificationChannelProviderRegistry, "test-rejecting")

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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &updateRecordingService{}
			router := gin.New()
			router.PUT("/:id", NewController(service, zap.NewNop().Sugar()).UpdateFull)

			body := `{"name": "Alerts", "type": "` + tc.channelType + `", "config": "{}"}`
			req := httptest.NewRequest(http.MethodPut, "/channel-1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantResponse)
			assert.Equal(t, tc.wantSaved, service.updated)
		})
	}
}
