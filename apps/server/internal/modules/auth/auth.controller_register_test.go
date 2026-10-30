package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// recordingRegister is the service whose Register keeps the registrations it gets and answers with
// err, or with a session for the email
type recordingRegister struct {
	auth.Service
	err  error
	dtos []auth.RegisterDto
}

func (s *recordingRegister) Register(_ context.Context, dto auth.RegisterDto) (*auth.LoginResponse, error) {
	s.dtos = append(s.dtos, dto)
	if s.err != nil {
		return nil, s.err
	}
	return &auth.LoginResponse{AccessToken: "access", RefreshToken: "refresh", User: &auth.Model{Email: dto.Email}}, nil
}

type registerRig struct {
	controller *auth.Controller
	service    *recordingRegister
	logs       *observer.ObservedLogs
}

func newRegisterRig(t *testing.T, cfg config.Config) *registerRig {
	t.Helper()
	validation.InitializeValidator()
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()
	service := &recordingRegister{}
	gate := auth.NewSignupGate(&cfg, installsettings.Static(&cfg), nil, logger)
	return &registerRig{controller: auth.NewController(service, nil, gate, nil, logger), service: service, logs: logs}
}

// post registers through the controller from the remote address with the JSON body
func (rig *registerRig) post(t *testing.T, remote, body string) (*httpx.Output[utils.ApiResponse[*auth.LoginResponse]], error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remote + ":1234"
	return rig.controller.Register(context.Background(), &auth.RegisterInput{Request: req})
}

func newcomer(extra string) string {
	return fmt.Sprintf(`{"email":"bob@example.com","password":"Correct-horse-1"%s}`, extra)
}

// answer is the status and the message of an error the controller answered with
func answer(t *testing.T, err error) (int, string) {
	t.Helper()
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	var response *httpx.ResponseError
	if errors.As(err, &response) {
		switch body := response.Body.(type) {
		case utils.ApiResponse[any]:
			return response.Status, body.Message
		case httpx.ErrorBody:
			return response.Status, body.Message
		case limits.ReachedResponse:
			return response.Status, body.Message
		}
	}
	return status.GetStatus(), ""
}

func TestController_Register_RefusesABodyThatDoesNotReadOrValidate(t *testing.T) {
	rig := newRegisterRig(t, config.Config{})

	for name, tc := range map[string]struct{ body, message string }{
		"a body that is not JSON":   {`email=bob`, ""},
		"no email":                  {`{"password":"Correct-horse-1"}`, "Email is required"},
		"an email that is no email": {`{"email":"bob","password":"Correct-horse-1"}`, "Please provide a valid email address"},
		"no password":               {`{"email":"bob@example.com"}`, "Password is required"},
		"a weak password":           {`{"email":"bob@example.com","password":"weak"}`, "Password must be at least 8 characters long and contain uppercase, lowercase, number, and special character"},
		"nothing at all":            {`{}`, "Email is required; Password is required"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := rig.post(t, "203.0.113.5", tc.body)

			assert.Nil(t, out)
			status, message := answer(t, err)
			assert.Equal(t, http.StatusBadRequest, status)
			if tc.message != "" {
				assert.Equal(t, tc.message, message)
			} else {
				assert.NotEmpty(t, message)
			}
		})
	}
	assert.Empty(t, rig.service.dtos, "the service is not asked")
}

func TestController_Register_AnAcceptedSignupIsCreated(t *testing.T) {
	rig := newRegisterRig(t, config.Config{})

	out, err := rig.post(t, "203.0.113.5", newcomer(""))

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, out.Status)
	assert.Equal(t, "User registered successfully", out.Body.Message)
	assert.Equal(t, "access", out.Body.Data.AccessToken)
	assert.Equal(t, "bob@example.com", out.Body.Data.User.Email)
	assert.Equal(t, []auth.RegisterDto{{Email: "bob@example.com", Password: "Correct-horse-1"}}, rig.service.dtos)
}

func TestController_Register_AnswersTheServicesRefusalsAndLogsEachAsItIs(t *testing.T) {
	reached := &limits.ReachedError{Code: "LIMIT_REACHED", Limit: limits.Members, Max: 3}
	tests := []struct {
		name    string
		invite  string
		err     error
		status  int
		message string
		logged  string
		level   zapcore.Level
	}{
		{"an address with an account", "", auth.ErrEmailTaken, http.StatusConflict, "An account with this email address exists already: sign in instead", "Registration refused: an account with this email exists", zapcore.InfoLevel},
		{"the install has its admin", "", auth.ErrAdminExists, http.StatusBadRequest, auth.ErrAdminExists.Error(), "Registration refused: the install has its admin already", zapcore.InfoLevel},
		{"the install has its admin, for an invite", "pki_x", auth.ErrAdminExists, http.StatusBadRequest, auth.ErrAdminExists.Error(), "Registration refused: the install has its admin already", zapcore.InfoLevel},
		{"a workspace with its members", "pki_x", reached, http.StatusForbidden, reached.Error(), "Invite refused: the workspace has reached its members limit", zapcore.InfoLevel},
		{"an invite for an address with an account", "pki_x", auth.ErrEmailTaken, http.StatusConflict, "An account with this email exists: sign in and accept the invite", "Invite refused: an account with this email exists", zapcore.InfoLevel},
		{"an invite that was used", "pki_x", workspace.ErrInviteUsed, http.StatusGone, "This invite can't be used: it was already used", "Invite refused: already used", zapcore.InfoLevel},
		{"an invite that is not there", "pki_x", workspace.ErrInviteNotFound, http.StatusNotFound, "Invite not found", "Invite refused: not found", zapcore.InfoLevel},
		{"an invite for another address", "pki_x", workspace.ErrInviteEmailMismatch, http.StatusForbidden, "This invite is for another email address", "Invite refused: for another email address", zapcore.InfoLevel},
		{"an invite to a workspace the user is in", "pki_x", workspace.ErrAlreadyMember, http.StatusConflict, "Already a member of this workspace", "Invite refused: already a member of the workspace", zapcore.InfoLevel},
		{"anything else", "", errors.New("database is down"), http.StatusInternalServerError, "database is down", "Failed to register", zapcore.ErrorLevel},
		{"anything else for an invite", "pki_x", errors.New("database is down"), http.StatusInternalServerError, "database is down", "Failed to register", zapcore.ErrorLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newRegisterRig(t, config.Config{})
			rig.service.err = tc.err
			extra := ""
			if tc.invite != "" {
				extra = fmt.Sprintf(`,"inviteToken":%q`, tc.invite)
			}

			out, err := rig.post(t, "203.0.113.5", newcomer(extra))

			assert.Nil(t, out)
			status, message := answer(t, err)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.message, message)
			require.Len(t, rig.logs.All(), 1)
			assert.Equal(t, tc.logged, rig.logs.All()[0].Message)
			assert.Equal(t, tc.level, rig.logs.All()[0].Level)
		})
	}
}

// An open signup is let in by the gate before the service is asked: five from one client an hour. An
// invite signup does not pass it.
func TestController_Register_AnOpenSignupPassesTheGateAndAnInviteDoesNot(t *testing.T) {
	rig := newRegisterRig(t, config.Config{SignupMode: config.SignupModeOpen})

	for i := 0; i < 5; i++ {
		_, err := rig.post(t, "203.0.113.5", newcomer(""))
		require.NoError(t, err, "signup %d", i+1)
	}
	out, err := rig.post(t, "203.0.113.5", newcomer(""))

	assert.Nil(t, out)
	var rateLimited huma.HeadersError
	require.ErrorAs(t, err, &rateLimited)
	status, _ := answer(t, err)
	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.NotEmpty(t, rateLimited.GetHeaders().Get("Retry-After"))
	assert.Len(t, rig.service.dtos, 5, "the sixth never reached the service")

	_, err = rig.post(t, "203.0.113.5", newcomer(`,"inviteToken":"pki_x"`))
	require.NoError(t, err, "an invite signup is not counted or refused")
	_, err = rig.post(t, "198.51.100.9", newcomer(""))
	require.NoError(t, err, "another client has its own five")
	assert.Len(t, rig.service.dtos, 7)
}

func TestController_Register_AnInstallThatIsNotOpenHasNoGate(t *testing.T) {
	rig := newRegisterRig(t, config.Config{})

	for i := 0; i < 8; i++ {
		_, err := rig.post(t, "203.0.113.5", newcomer(""))
		require.NoError(t, err)
	}
	assert.Len(t, rig.service.dtos, 8)
}

// An open signup that Turnstile does not accept is refused with the one message, and the reason stays
// in the log
func TestController_Register_ATurnstileRefusalIsAskedToBeTriedAgain(t *testing.T) {
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	t.Cleanup(cloudflare.Close)
	rig := newRegisterRig(t, config.Config{
		SignupMode: config.SignupModeOpen, SignupTurnstileSiteKey: "site", SignupTurnstileSecret: "secret", SignupTurnstileVerifyURL: cloudflare.URL,
	})

	out, err := rig.post(t, "203.0.113.5", newcomer(`,"turnstileToken":"forged"`))

	assert.Nil(t, out)
	status, message := answer(t, err)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "We could not verify that you are not a robot. Reload the page and try again.", message)
	assert.Empty(t, rig.service.dtos)

	_, err = rig.post(t, "203.0.113.5", newcomer(`,"inviteToken":"pki_x"`))
	require.NoError(t, err, "an invite signup needs no answer")
}
