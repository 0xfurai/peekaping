package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/admin"
	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordingService is the admin service as far as UpdateWorkspace calls it: it keeps the calls it
// gets and answers each with a workspace named after the call, or with the error the test sets
type recordingService struct {
	admin.Service
	calls        []string
	ownLimits    []*limits.Limits
	limitsErr    error
	suspendedErr error
}

func (s *recordingService) SetWorkspaceLimits(_ context.Context, actorID, workspaceID string, own *limits.Limits) (*admin.Workspace, error) {
	s.calls = append(s.calls, "limits:"+actorID+":"+workspaceID)
	s.ownLimits = append(s.ownLimits, own)
	if s.limitsErr != nil {
		return nil, s.limitsErr
	}
	return &admin.Workspace{ID: "ws-1", Name: "after limits"}, nil
}

func (s *recordingService) SetWorkspaceSuspended(_ context.Context, actorID, workspaceID string, suspended bool) (*admin.Workspace, error) {
	call := "resume"
	if suspended {
		call = "suspend"
	}
	s.calls = append(s.calls, call+":"+actorID+":"+workspaceID)
	if s.suspendedErr != nil {
		return nil, s.suspendedErr
	}
	return &admin.Workspace{ID: "ws-1", Name: "after " + call}, nil
}

// patch calls UpdateWorkspace as the admin "admin-1" for workspace ws-1 with the JSON body
func patch(t *testing.T, service admin.Service, body string) (*httpx.Output[utils.ApiResponse[*admin.WorkspaceResponse]], error) {
	t.Helper()
	controller := admin.NewController(service, nil, zap.NewNop().Sugar())
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/workspaces/ws-1", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", "ws-1")
	ctx := httpx.WithValue(r, "userId", "admin-1").Context()
	return controller.UpdateWorkspace(ctx, &admin.UpdateWorkspaceInput{Request: r})
}

// failed is the status and the message of the answer an error is
func failed(t *testing.T, err error) (int, string) {
	t.Helper()
	var response *httpx.ResponseError
	require.ErrorAs(t, err, &response)
	body, ok := response.Body.(utils.ApiResponse[any])
	require.True(t, ok)
	return response.Status, body.Message
}

func TestController_UpdateWorkspace_DoesWhatTheBodyNamesLimitsFirst(t *testing.T) {
	seven := 7
	tests := []struct {
		name      string
		body      string
		calls     []string
		ownLimits []*limits.Limits
		wantName  string
	}{
		{"suspending", `{"suspended":true}`, []string{"suspend:admin-1:ws-1"}, nil, "after suspend"},
		{"resuming", `{"suspended":false}`, []string{"resume:admin-1:ws-1"}, nil, "after resume"},
		{"giving limits", `{"limits":{"monitors":7}}`, []string{"limits:admin-1:ws-1"}, []*limits.Limits{{Monitors: &seven}}, "after limits"},
		{"giving no limits at all", `{"limits":{}}`, []string{"limits:admin-1:ws-1"}, []*limits.Limits{{}}, "after limits"},
		{"clearing limits", `{"clearLimits":true}`, []string{"limits:admin-1:ws-1"}, []*limits.Limits{nil}, "after limits"},
		{"limits and suspending", `{"limits":{"monitors":7},"suspended":true}`, []string{"limits:admin-1:ws-1", "suspend:admin-1:ws-1"}, []*limits.Limits{{Monitors: &seven}}, "after suspend"},
		{"clearing limits and resuming", `{"clearLimits":true,"suspended":false}`, []string{"limits:admin-1:ws-1", "resume:admin-1:ws-1"}, []*limits.Limits{nil}, "after resume"},
		{"clearLimits false and suspending", `{"clearLimits":false,"suspended":true}`, []string{"suspend:admin-1:ws-1"}, nil, "after suspend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &recordingService{}

			out, err := patch(t, service, tc.body)

			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, out.Status)
			assert.Equal(t, "Workspace updated successfully", out.Body.Message)
			assert.Equal(t, tc.wantName, out.Body.Data.Name, "the answer is the workspace the last call gave")
			assert.Equal(t, tc.calls, service.calls)
			assert.Equal(t, tc.ownLimits, service.ownLimits)
		})
	}
}

func TestController_UpdateWorkspace_RefusesABodyThatNamesNothingOrTooMuch(t *testing.T) {
	const nothing = "Name what to change: suspended, limits or clearLimits"
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{"an empty object", `{}`, nothing},
		{"clearLimits false only", `{"clearLimits":false}`, nothing},
		{"a body that is not JSON", `suspended`, nothing},
		{"no body", ``, nothing},
		{"limits and clearLimits", `{"limits":{"monitors":1},"clearLimits":true}`, "Send limits or clearLimits, not both"},
		{"empty limits and clearLimits", `{"limits":{},"clearLimits":true}`, "Send limits or clearLimits, not both"},
		{"a limit below 0", `{"limits":{"monitors":-1}}`, "the limit monitors must be 0 or more"},
		{"a limit below 0 with suspending", `{"limits":{"members":-3},"suspended":true}`, "the limit members must be 0 or more"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &recordingService{}

			out, err := patch(t, service, tc.body)

			require.Error(t, err)
			assert.Nil(t, out)
			status, message := failed(t, err)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, tc.message, message)
			assert.Empty(t, service.calls, "nothing is changed")
		})
	}
}

func TestController_UpdateWorkspace_StopsAtTheFirstCallThatFails(t *testing.T) {
	t.Run("a workspace that is not there for the limits", func(t *testing.T) {
		service := &recordingService{limitsErr: admin.ErrWorkspaceNotFound}

		out, err := patch(t, service, `{"limits":{},"suspended":true}`)

		assert.Nil(t, out)
		status, message := failed(t, err)
		assert.Equal(t, http.StatusNotFound, status)
		assert.Equal(t, "Workspace not found", message)
		assert.Equal(t, []string{"limits:admin-1:ws-1"}, service.calls, "and the suspension is not tried")
	})
	t.Run("a suspension that fails after the limits were set", func(t *testing.T) {
		service := &recordingService{suspendedErr: errors.New("database is closed")}

		out, err := patch(t, service, `{"clearLimits":true,"suspended":true}`)

		assert.Nil(t, out)
		status, message := failed(t, err)
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Equal(t, "Internal server error", message, "the reason stays in the log")
		assert.Equal(t, []string{"limits:admin-1:ws-1", "suspend:admin-1:ws-1"}, service.calls)
	})
	t.Run("a suspension of a workspace that is not there", func(t *testing.T) {
		service := &recordingService{suspendedErr: admin.ErrWorkspaceNotFound}

		_, err := patch(t, service, `{"suspended":false}`)

		status, _ := failed(t, err)
		assert.Equal(t, http.StatusNotFound, status)
	})
}

func TestController_UpdateWorkspace_NeedsTheAdminTheRouteLetsThrough(t *testing.T) {
	controller := admin.NewController(&recordingService{}, nil, zap.NewNop().Sugar())
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/workspaces/ws-1", strings.NewReader(`{"suspended":true}`))
	r.SetPathValue("id", "ws-1")

	out, err := controller.UpdateWorkspace(context.Background(), &admin.UpdateWorkspaceInput{Request: r})

	assert.Nil(t, out)
	status, message := failed(t, err)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "Unauthorized", message)
}
