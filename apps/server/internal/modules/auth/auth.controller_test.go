package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// registerFailing is the service whose Register fails with err
type registerFailing struct {
	auth.Service
	err error
}

func (s registerFailing) Register(context.Context, auth.RegisterDto) (*auth.LoginResponse, error) {
	return nil, s.err
}

// register calls the controller's Register with the invite token and returns what it logged
func register(t *testing.T, err error, token string) (status int, logs []observer.LoggedEntry) {
	t.Helper()
	validation.InitializeValidator()
	core, recorded := observer.New(zapcore.DebugLevel)
	controller := auth.NewController(registerFailing{err: err}, zap.New(core).Sugar())
	body := fmt.Sprintf(`{"email":"bob@example.com","password":"Correct-horse-1","inviteToken":%q}`, token)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, got := controller.Register(context.Background(), &auth.RegisterInput{Request: req})

	var failure *httpx.ResponseError
	require.ErrorAs(t, got, &failure)
	return failure.Status, recorded.All()
}

func TestController_Register_ARefusedInviteIsLoggedAtInfoWithItsReasonAndNeverItsToken(t *testing.T) {
	const token = "pki_A0123456789012345678901234567890123456789ab"
	for _, tt := range []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"used", fmt.Errorf("%w", workspace.ErrInviteUsed), http.StatusGone, "already used"},
		{"expired", workspace.ErrInviteExpired, http.StatusGone, "expired"},
		{"revoked", workspace.ErrInviteRevoked, http.StatusGone, "revoked"},
		{"unknown", workspace.ErrInviteNotFound, http.StatusNotFound, "not found"},
		{"another email", workspace.ErrInviteEmailMismatch, http.StatusForbidden, "another email"},
		{"existing account", auth.ErrEmailTaken, http.StatusConflict, "account with this email exists"},
		{"already a member", workspace.ErrAlreadyMember, http.StatusConflict, "already a member"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, logs := register(t, tt.err, token)

			assert.Equal(t, tt.status, status)
			require.Len(t, logs, 1)
			assert.Equal(t, zapcore.InfoLevel, logs[0].Level, "a refusal is no error of the server")
			assert.Contains(t, logs[0].Message, "Invite refused")
			assert.Contains(t, logs[0].Message, tt.reason)
			assert.NotContains(t, fmt.Sprint(logs[0].Message, logs[0].Context), token)
			assert.NotContains(t, fmt.Sprint(logs[0].Message, logs[0].Context), "Failed to register admin")
		})
	}
}

func TestController_Register_AnUnexpectedFailureIsStillLoggedAsAnError(t *testing.T) {
	status, logs := register(t, errors.New("database is down"), "pki_A0123456789012345678901234567890123456789ab")

	assert.Equal(t, http.StatusInternalServerError, status)
	require.Len(t, logs, 1)
	assert.Equal(t, zapcore.ErrorLevel, logs[0].Level)
	assert.Equal(t, "Failed to register", logs[0].Message)
}
