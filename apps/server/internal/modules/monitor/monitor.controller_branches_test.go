package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// branchService is a Service that answers each call the controller makes with what the test sets,
// and keeps the arguments of the call.
type branchService struct {
	Service

	validateErr error
	createErr   error
	updateErr   error
	updated     *Model
	minimum     int
	minimumErr  error

	findAllErr   error
	findAllCalls []findAllCall

	heartbeatsErr   error
	heartbeatsCalls []heartbeatsCall

	statErr   error
	statCalls []statCall
}

type findAllCall struct {
	page, limit int
	q           string
	active      *bool
	status      *int
	tagIds      []string
}

type heartbeatsCall struct {
	probeID     string
	limit, page int
	important   *bool
	reverse     bool
}

type statCall struct {
	id, probeID, granularity string
	since, until             time.Time
}

func (s *branchService) ValidateMonitorConfig(string, string) error { return s.validateErr }

func (s *branchService) Create(context.Context, workspace.ID, *CreateUpdateDto) (*Model, error) {
	return nil, s.createErr
}

func (s *branchService) UpdateFull(context.Context, workspace.ID, string, *CreateUpdateDto) (*Model, error) {
	return s.updated, s.updateErr
}

func (s *branchService) MinCheckInterval(context.Context, workspace.ID) (int, error) {
	return s.minimum, s.minimumErr
}

func (s *branchService) FindAll(_ context.Context, _ workspace.ID, page, limit int, q string, active *bool, status *int, tagIds []string) ([]*Model, error) {
	s.findAllCalls = append(s.findAllCalls, findAllCall{page, limit, q, active, status, tagIds})
	return nil, s.findAllErr
}

func (s *branchService) GetHeartbeats(_ context.Context, _ workspace.ID, _, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	s.heartbeatsCalls = append(s.heartbeatsCalls, heartbeatsCall{probeID, limit, page, important, reverse})
	return nil, s.heartbeatsErr
}

func (s *branchService) GetStatPoints(_ context.Context, _ workspace.ID, id, probeID string, since, until time.Time, granularity string) (*StatPointsSummaryDto, error) {
	s.statCalls = append(s.statCalls, statCall{id, probeID, granularity, since, until})
	return &StatPointsSummaryDto{}, s.statErr
}

func branchController(service Service) (*MonitorController, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	return NewMonitorController(service, zap.New(core).Sugar(), unlinkedNotifications{}, unlinkedTags{}, nil, nil), logs
}

// branchCtx is the context of a request by an admin of the test workspace, which the controller
// reads the workspace from.
func branchCtx() context.Context {
	return workspace.WithContext(context.Background(), testWorkspace, workspace.RoleAdmin)
}

func branchRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.SetPathValue("id", "mon-1")
	return req
}

// failureOf is the status and the JSON body of the response an error from the controller stands for.
func failureOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var failure *httpx.ResponseError
	require.ErrorAs(t, err, &failure)
	body, marshalErr := json.Marshal(failure.Body)
	require.NoError(t, marshalErr)
	return failure.Status, string(body)
}

func assertFailure(t *testing.T, err error, status int, message string) {
	t.Helper()
	gotStatus, body := failureOf(t, err)
	assert.Equal(t, status, gotStatus)
	assert.JSONEq(t, `{"message":`+quote(message)+`,"data":null}`, body)
}

func quote(s string) string {
	quoted, _ := json.Marshal(s)
	return string(quoted)
}

func assertLogged(t *testing.T, logs *observer.ObservedLogs, message string) {
	t.Helper()
	assert.Len(t, logs.FilterMessage(message).All(), 1, "logged %q", message)
}

const validMonitorBody = `{"type": "http", "name": "Public API", "interval": 60, "retry_interval": 60, "timeout": 16, "notification_ids": [],` +
	` "config": "{\"url\": \"https://example.com\"}"}`

func TestControllerFindAllRefusesAQueryItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		query   string
		message string
	}{
		{"page not a number", "page=x", "Invalid page parameter"},
		{"page below 0", "page=-1", "Invalid page parameter"},
		{"limit not a number", "limit=x", "Invalid limit parameter"},
		{"limit 0", "limit=0", "Invalid limit parameter"},
		{"limit above the maximum", "limit=1001", "Invalid limit parameter"},
		{"page is judged before limit", "page=-1&limit=0", "Invalid page parameter"},
		{"active not a boolean", "active=maybe", "Invalid active parameter (must be true or false)"},
		{"limit is judged before active", "limit=0&active=maybe", "Invalid limit parameter"},
		{"status not a number", "status=up", "Invalid status parameter (must be int)"},
		{"active is judged before status", "active=maybe&status=up", "Invalid active parameter (must be true or false)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			_, err := controller.FindAll(branchCtx(), &FindAllInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			assertFailure(t, err, http.StatusBadRequest, tc.message)
			assert.Empty(t, service.findAllCalls)
		})
	}
}

func TestControllerFindAllPassesTheQueryToTheService(t *testing.T) {
	yes, no, five := true, false, 5
	for _, tc := range []struct {
		name  string
		query string
		want  findAllCall
	}{
		{"defaults", "", findAllCall{page: 0, limit: 10}},
		{"everything set", "page=2&limit=25&q=api&active=true&status=5&tag_ids=a,b",
			findAllCall{page: 2, limit: 25, q: "api", active: &yes, status: &five, tagIds: []string{"a", "b"}}},
		{"inactive", "active=false", findAllCall{limit: 10, active: &no}},
		{"status 0 is a status", "status=0", findAllCall{limit: 10, status: new(int)}},
		{"tag IDs are trimmed and the empty ones dropped", "tag_ids=%20a%20,,%20,b",
			findAllCall{limit: 10, tagIds: []string{"a", "b"}}},
		{"only empty tag IDs leave no tag filter", "tag_ids=,%20,", findAllCall{limit: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			out, err := controller.FindAll(branchCtx(), &FindAllInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, out.Status)
			require.Len(t, service.findAllCalls, 1)
			assert.Equal(t, tc.want, service.findAllCalls[0])
		})
	}
}

func TestControllerFindAllAnswers500WhenAReadFails(t *testing.T) {
	t.Run("the monitors", func(t *testing.T) {
		controller, logs := branchController(&branchService{findAllErr: errors.New("database unavailable")})

		_, err := controller.FindAll(branchCtx(), &FindAllInput{Request: branchRequest(http.MethodGet, "/", "")})
		assertFailure(t, err, http.StatusInternalServerError, "Internal server error")
		assertLogged(t, logs, "Failed to fetch monitors")
	})
	t.Run("the links", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		controller := NewMonitorController(&branchService{}, zap.New(core).Sugar(), unlinkedNotifications{}, unlinkedTags{}, nil, nil)
		service := &listingService{monitors: []*Model{{ID: "mon-1"}}}
		controller.monitorService = service
		controller.monitorNotificationService = failingNotifications{}

		_, err := controller.FindAll(branchCtx(), &FindAllInput{Request: branchRequest(http.MethodGet, "/", "")})
		assertFailure(t, err, http.StatusInternalServerError, "Internal server error")
		assertLogged(t, logs, "Failed to fetch monitor links")
	})
}

// listingService answers FindAll with monitors.
type listingService struct {
	Service
	monitors []*Model
}

func (s *listingService) FindAll(context.Context, workspace.ID, int, int, string, *bool, *int, []string) ([]*Model, error) {
	return s.monitors, nil
}

func TestControllerCreateAnswers500WhenTheServiceFailsOtherwise(t *testing.T) {
	controller, logs := branchController(&branchService{createErr: errors.New("database unavailable")})

	_, err := controller.Create(branchCtx(), &CreateInput{Request: branchRequest(http.MethodPost, "/", validMonitorBody)})
	assertFailure(t, err, http.StatusInternalServerError, "Internal server error")
	assertLogged(t, logs, "Failed to create monitor")
}

func TestControllerUpdateFullRefusesAndFails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service *branchService
		status  int
		message string
		logged  string
	}{
		{"a config the service refuses", &branchService{validateErr: errors.New("url is required")},
			http.StatusBadRequest, "Invalid monitor configuration: url is required", ""},
		{"a monitor that isn't there", &branchService{updateErr: ErrMonitorNotFound},
			http.StatusNotFound, "monitor not found", "Failed to update monitor"},
		{"a failed write", &branchService{updateErr: errors.New("database unavailable")},
			http.StatusInternalServerError, "Internal server error", "Failed to update monitor"},
		{"links that can't be read back", &branchService{updated: &Model{ID: "mon-1"}},
			http.StatusInternalServerError, "Internal server error", "Failed to fetch monitor links"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller, logs := branchController(tc.service)
			if tc.service.updated != nil {
				controller.monitorNotificationService = failingNotifications{}
			}

			_, err := controller.UpdateFull(branchCtx(), &UpdateFullInput{Request: branchRequest(http.MethodPut, "/", validMonitorBody)})
			assertFailure(t, err, tc.status, tc.message)
			if tc.logged == "" {
				assert.Zero(t, logs.Len())
			} else {
				assertLogged(t, logs, tc.logged)
			}
		})
	}
}

func TestControllerAnswers500WhenTheMinimumIntervalCannotBeRead(t *testing.T) {
	body := strings.Replace(validMonitorBody, `"interval": 60`, `"interval": 5`, 1)
	controller, logs := branchController(&branchService{minimumErr: errors.New("database unavailable")})

	_, err := controller.Create(branchCtx(), &CreateInput{Request: branchRequest(http.MethodPost, "/", body)})
	assertFailure(t, err, http.StatusInternalServerError, "Internal server error")
	assertLogged(t, logs, "Failed to read the minimum check interval")
}

func TestControllerFindByMonitorIDPaginatedRefusesAQueryItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		query   string
		message string
	}{
		{"limit not a number", "limit=x", "Invalid limit parameter (1-1000)"},
		{"limit 0", "limit=0", "Invalid limit parameter (1-1000)"},
		{"limit above the maximum", "limit=1001", "Invalid limit parameter (1-1000)"},
		{"page not a number", "page=x", "Invalid page parameter (>=0)"},
		{"page below 0", "page=-1", "Invalid page parameter (>=0)"},
		{"limit is judged before page", "limit=0&page=-1", "Invalid limit parameter (1-1000)"},
		{"important not a boolean", "important=maybe", "Invalid important parameter (must be true or false)"},
		{"page is judged before important", "page=-1&important=maybe", "Invalid page parameter (>=0)"},
		{"reverse not a boolean", "reverse=maybe", "Invalid reverse parameter (must be true or false)"},
		{"important is judged before reverse", "important=maybe&reverse=maybe", "Invalid important parameter (must be true or false)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			_, err := controller.FindByMonitorIDPaginated(branchCtx(), &FindByMonitorIDPaginatedInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			assertFailure(t, err, http.StatusBadRequest, tc.message)
			assert.Empty(t, service.heartbeatsCalls)
		})
	}
}

func TestControllerFindByMonitorIDPaginatedPassesTheQueryToTheService(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name  string
		query string
		want  heartbeatsCall
	}{
		{"defaults", "", heartbeatsCall{limit: 50}},
		{"everything set", "limit=20&page=3&important=true&reverse=true&probe_id=frankfurt",
			heartbeatsCall{probeID: "frankfurt", limit: 20, page: 3, important: &yes, reverse: true}},
		{"important false is a filter", "important=false", heartbeatsCall{limit: 50, important: &no}},
		{"reverse false", "reverse=false", heartbeatsCall{limit: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			out, err := controller.FindByMonitorIDPaginated(branchCtx(), &FindByMonitorIDPaginatedInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, out.Status)
			require.Len(t, service.heartbeatsCalls, 1)
			assert.Equal(t, tc.want, service.heartbeatsCalls[0])
		})
	}
}

func TestControllerFindByMonitorIDPaginatedMapsTheServicesErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		logged string
	}{
		{"not found", ErrMonitorNotFound, http.StatusNotFound, ""},
		{"lookup failed", &LookupError{Err: errors.New("database unavailable")}, http.StatusInternalServerError, "Failed to find monitor"},
		{"read failed", errors.New("database unavailable"), http.StatusInternalServerError, "Failed to get heartbeats"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller, logs := branchController(&branchService{heartbeatsErr: tc.err})

			_, err := controller.FindByMonitorIDPaginated(branchCtx(), &FindByMonitorIDPaginatedInput{Request: branchRequest(http.MethodGet, "/", "")})
			status, _ := failureOf(t, err)
			assert.Equal(t, tc.status, status)
			if tc.logged != "" {
				assertLogged(t, logs, tc.logged)
			}
		})
	}
}

func TestControllerGetStatPointsRefusesAQueryItCannotServe(t *testing.T) {
	const since = "2026-01-01T00:00:00Z"
	for _, tc := range []struct {
		name    string
		query   string
		message string
	}{
		{"no since", "", "Missing required 'since' parameter"},
		{"since not RFC3339", "since=yesterday", "Invalid 'since' parameter (must be RFC3339)"},
		{"until not RFC3339", "since=" + since + "&until=tomorrow", "Invalid 'until' parameter (must be RFC3339)"},
		{"unknown granularity", "since=" + since + "&until=2026-01-01T01:00:00Z&granularity=week",
			"Invalid 'granularity' parameter (must be minute, hour, or day)"},
		{"granularity is judged before the order of since and until", "since=2026-01-02T00:00:00Z&until=" + since + "&granularity=week",
			"Invalid 'granularity' parameter (must be minute, hour, or day)"},
		{"until before since", "since=2026-01-02T00:00:00Z&until=" + since, "'until' must be after 'since'"},
		{"25 hours by the minute", "since=" + since + "&until=2026-01-02T01:00:00Z", "Too many points requested: 1501 (max 1441)"},
		{"a year by the hour", "since=" + since + "&until=2027-01-01T00:00:00Z&granularity=hour", "Too many points requested: 8761 (max 1441)"},
		{"1442 days", "since=" + since + "&until=2029-12-12T00:00:00Z&granularity=day", "Too many points requested: 1442 (max 1441)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			_, err := controller.GetStatPoints(branchCtx(), &GetStatPointsInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			assertFailure(t, err, http.StatusBadRequest, tc.message)
			assert.Empty(t, service.statCalls)
		})
	}
}

func TestControllerGetStatPointsPassesTheRangeToTheService(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		query string
		want  statCall
	}{
		{"24 hours by the minute is 1441 points, the most", "since=2026-01-01T00:00:00Z&until=2026-01-02T00:00:00Z",
			statCall{id: "mon-1", granularity: "minute", since: since, until: since.Add(24 * time.Hour)}},
		{"by the hour", "since=2026-01-01T00:00:00Z&until=2026-01-10T00:00:00Z&granularity=hour&probe_id=local",
			statCall{id: "mon-1", probeID: "local", granularity: "hour", since: since, until: since.Add(9 * 24 * time.Hour)}},
		{"by the day", "since=2026-01-01T00:00:00Z&until=2026-03-01T00:00:00Z&granularity=day",
			statCall{id: "mon-1", granularity: "day", since: since, until: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}},
		{"an empty range", "since=2026-01-01T00:00:00Z&until=2026-01-01T00:00:00Z",
			statCall{id: "mon-1", granularity: "minute", since: since, until: since}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchService{}
			controller, _ := branchController(service)

			out, err := controller.GetStatPoints(branchCtx(), &GetStatPointsInput{Request: branchRequest(http.MethodGet, "/?"+tc.query, "")})
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, out.Status)
			require.Len(t, service.statCalls, 1)
			assert.Equal(t, tc.want, service.statCalls[0])
		})
	}
}

func TestControllerGetStatPointsMapsTheServicesErrors(t *testing.T) {
	const query = "/?since=2026-01-01T00:00:00Z&until=2026-01-01T01:00:00Z"
	t.Run("not found", func(t *testing.T) {
		controller, _ := branchController(&branchService{statErr: ErrMonitorNotFound})
		_, err := controller.GetStatPoints(branchCtx(), &GetStatPointsInput{Request: branchRequest(http.MethodGet, query, "")})
		assertFailure(t, err, http.StatusNotFound, "Monitor not found")
	})
	t.Run("lookup failed", func(t *testing.T) {
		controller, logs := branchController(&branchService{statErr: &LookupError{Err: errors.New("database unavailable")}})
		_, err := controller.GetStatPoints(branchCtx(), &GetStatPointsInput{Request: branchRequest(http.MethodGet, query, "")})
		assertFailure(t, err, http.StatusInternalServerError, "Internal server error")
		assertLogged(t, logs, "Failed to find monitor")
	})
	t.Run("any other error is a 400 with its text", func(t *testing.T) {
		controller, logs := branchController(&branchService{statErr: errors.New("stats unavailable")})
		_, err := controller.GetStatPoints(branchCtx(), &GetStatPointsInput{Request: branchRequest(http.MethodGet, query, "")})
		assertFailure(t, err, http.StatusBadRequest, "stats unavailable")
		assert.Zero(t, logs.Len())
	})
}
