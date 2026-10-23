package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"

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

// unlinkedNotifications and unlinkedTags are link services whose monitors have no links.
type unlinkedNotifications struct{ monitor_notification.Service }

func (unlinkedNotifications) FindByMonitorIDs(context.Context, []string) (map[string][]*monitor_notification.Model, error) {
	return nil, nil
}

type unlinkedTags struct{ monitor_tag.Service }

func (unlinkedTags) FindByMonitorIDs(context.Context, []string) (map[string][]*monitor_tag.Model, error) {
	return nil, nil
}

func TestControllerCreateLogsTheMonitorsIDAndTypeButNotItsConfig(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	controller := NewMonitorController(creatingService{}, zap.New(core).Sugar(), unlinkedNotifications{}, unlinkedTags{}, nil, nil)
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

// failingNotifications and failingTags are link services that can't read the links back.
type failingNotifications struct{ monitor_notification.Service }

func (failingNotifications) FindByMonitorIDs(context.Context, []string) (map[string][]*monitor_notification.Model, error) {
	return nil, errors.New("database unavailable")
}

type failingTags struct{ monitor_tag.Service }

func (failingTags) FindByMonitorIDs(context.Context, []string) (map[string][]*monitor_tag.Model, error) {
	return nil, errors.New("database unavailable")
}

// A create that answered 500 after the monitor was committed would have a client send the POST
// again and create the monitor twice, so the response never depends on a read of the links.
func TestControllerCreateAnswersWithoutReadingTheLinksBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want map[string]any
	}{
		{"links, repeated IDs once each",
			`"notification_ids": ["channel-a", "channel-b", "channel-a"], "tag_ids": ["tag-a", "tag-a"]`,
			map[string]any{"notification_ids": []any{"channel-a", "channel-b"}, "tag_ids": []any{"tag-a"}}},
		{"no links, empty lists",
			`"notification_ids": []`,
			map[string]any{"notification_ids": []any{}, "tag_ids": []any{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := NewMonitorController(creatingService{}, zap.NewNop().Sugar(), failingNotifications{}, failingTags{}, nil, nil)
			router := httpx.NewRouter()
			httpx.Register(router, "POST", "/", huma.Operation{Errors: []int{400, 500}}, controller.Create)

			body := `{"type": "http", "name": "Public API", "interval": 60, "retry_interval": 60, "timeout": 16, ` + tc.body +
				`, "config": "{\"url\": \"https://example.com\"}"}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			var response struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			assert.Equal(t, "mon-1", response.Data["id"])
			assert.Equal(t, tc.want["notification_ids"], response.Data["notification_ids"])
			assert.Equal(t, tc.want["tag_ids"], response.Data["tag_ids"])
		})
	}
}
