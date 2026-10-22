package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type creatingService struct {
	Service
}

func (creatingService) ValidateMonitorConfig(string, string) error { return nil }

func (creatingService) Create(_ context.Context, dto *CreateUpdateDto) (*Model, error) {
	return &Model{ID: "mon-1", Type: dto.Type, Name: dto.Name, Config: dto.Config}, nil
}

func TestControllerCreateLogsTheMonitorsIDAndTypeButNotItsConfig(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	controller := NewMonitorController(creatingService{}, zap.New(core).Sugar(), nil, nil, nil, nil)
	router := httpx.NewRouter()
	httpx.Register(router, "POST", "/", huma.Operation{Errors: []int{400, 500}}, controller.Create)

	body := `{"type": "http", "name": "Public API", "interval": 60, "retry_interval": 60, "timeout": 16, "notification_ids": [],` +
		` "config": "{\"url\": \"https://example.com\", \"authMethod\": \"basic\", \"basic_auth_pass\": \"secret-monitor-password\"}"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := logs.FilterMessageSnippet("Created monitor").All()
	require.Len(t, created, 1)
	assert.Contains(t, created[0].Message, "mon-1")
	assert.Contains(t, created[0].Message, "http")
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, "secret-monitor-password")
		assert.NotContains(t, entry.Message, "basic_auth_pass")
	}
}
