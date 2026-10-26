package modules_test

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type paginationCall struct {
	calls int
	page  int
	limit int
}

func (c *paginationCall) record(page, limit int) {
	c.calls++
	c.page = page
	c.limit = limit
}

type tagPaginationService struct {
	tag.Service
	*paginationCall
}

func (s *tagPaginationService) FindAll(_ context.Context, page, limit int, _ string) ([]*tag.Model, error) {
	s.record(page, limit)
	return nil, nil
}

type proxyPaginationService struct {
	proxy.Service
	*paginationCall
}

func (s *proxyPaginationService) FindAll(_ context.Context, page, limit int, _ string) ([]*proxy.Model, error) {
	s.record(page, limit)
	return nil, nil
}

type notificationPaginationService struct {
	notification_channel.Service
	*paginationCall
}

func (s *notificationPaginationService) FindAll(_ context.Context, page, limit int, _ string) ([]*notification_channel.Model, error) {
	s.record(page, limit)
	return nil, nil
}

type maintenancePaginationService struct {
	maintenance.Service
	*paginationCall
}

func (s *maintenancePaginationService) FindAll(_ context.Context, page, limit int, _, _ string) ([]*maintenance.Model, error) {
	s.record(page, limit)
	return nil, nil
}

type statusPagePaginationService struct {
	status_page.Service
	*paginationCall
}

func (s *statusPagePaginationService) FindAll(_ context.Context, page, limit int, _ string) ([]*status_page.Model, error) {
	s.record(page, limit)
	return nil, nil
}

type monitorPaginationService struct {
	monitor.Service
	*paginationCall
}

func (s *monitorPaginationService) FindAll(_ context.Context, page, limit int, _ string, _ *bool, _ *int, _ []string) ([]*monitor.Model, error) {
	s.record(page, limit)
	return nil, nil
}

func (s *monitorPaginationService) FindByID(_ context.Context, id string) (*monitor.Model, error) {
	return &monitor.Model{ID: id}, nil
}

func (s *monitorPaginationService) GetHeartbeats(_ context.Context, _, _ string, limit, page int, _ *bool, _ bool) ([]*heartbeat.Model, error) {
	s.record(page, limit)
	return nil, nil
}

func TestListEndpointsPagination(t *testing.T) {

	logger := zap.NewNop().Sugar()
	endpoints := []struct {
		name         string
		defaultLimit int
		register     func(*httpx.Router, *paginationCall)
	}{
		{"tags", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, tag.NewController(&tagPaginationService{paginationCall: call}, logger).FindAll)
		}},
		{"proxies", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, proxy.NewController(&proxyPaginationService{paginationCall: call}, logger).FindAll)
		}},
		{"notification-channels", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, notification_channel.NewController(&notificationPaginationService{paginationCall: call}, nil, logger).FindAll)
		}},
		{"maintenances", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, maintenance.NewController(&maintenancePaginationService{paginationCall: call}, logger).FindAll)
		}},
		{"status-pages", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, status_page.NewController(&statusPagePaginationService{paginationCall: call}, nil, nil, logger).FindAll)
		}},
		{"monitors", 10, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, monitor.NewMonitorController(&monitorPaginationService{paginationCall: call}, logger, nil, nil, nil, nil).FindAll)
		}},
		{"heartbeats", 50, func(router *httpx.Router, call *paginationCall) {
			httpx.Register(router, "GET", "/", huma.Operation{Errors: []int{400, 500}}, monitor.NewMonitorController(&monitorPaginationService{paginationCall: call}, logger, nil, nil, nil, nil).FindByMonitorIDPaginated)
		}},
	}
	cases := []struct {
		name   string
		query  string
		status int
		limit  int
		page   int
	}{
		{"defaults", "", http.StatusOK, 0, 0},
		{"empty_limit", "?limit=", http.StatusOK, 0, 0},
		{"minimum", "?limit=1&page=2", http.StatusOK, 1, 2},
		{"tag_picker_size", "?limit=100&page=2", http.StatusOK, 100, 2},
		{"heartbeat_chart_size", "?limit=150&page=2", http.StatusOK, 150, 2},
		{"maximum", "?limit=1000&page=2", http.StatusOK, 1000, 2},
		{"above_maximum", "?limit=1001", http.StatusBadRequest, 0, 0},
		{"excessive", "?limit=100000", http.StatusBadRequest, 0, 0},
		{"zero", "?limit=0", http.StatusBadRequest, 0, 0},
		{"negative", "?limit=-1", http.StatusBadRequest, 0, 0},
		{"invalid", "?limit=nope", http.StatusBadRequest, 0, 0},
		{"overflow", "?limit=999999999999999999999999", http.StatusBadRequest, 0, 0},
		{"negative_page", "?page=-1", http.StatusBadRequest, 0, 0},
		{"invalid_page", "?page=nope", http.StatusBadRequest, 0, 0},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					call := &paginationCall{}
					router := httpx.NewRouter()
					endpoint.register(router, call)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+tc.query, nil))
					require.Equal(t, tc.status, response.Code, response.Body.String())
					if tc.status == http.StatusBadRequest {
						assert.Zero(t, call.calls, "invalid pagination must not reach the service")
						assert.Contains(t, response.Body.String(), "Invalid")
						return
					}
					require.Equal(t, 1, call.calls)
					expectedLimit := tc.limit
					if expectedLimit == 0 {
						expectedLimit = endpoint.defaultLimit
					}
					assert.Equal(t, expectedLimit, call.limit)
					assert.Equal(t, tc.page, call.page)
				})
			}
		})
	}
}
