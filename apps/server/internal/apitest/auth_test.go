package apitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func bearer(token string) credentials {
	return func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+token) }
}

func (a *app) login(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, "/api/v1/auth/login", body)
}

func TestAuthAPI_SignsAdminIn(t *testing.T) {
	a := newAppWithAdmin(t)

	session := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}), http.StatusOK)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(session.AccessToken)).Code)

	refreshed := decode[tokens](t, a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": session.RefreshToken}), http.StatusOK)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(refreshed.AccessToken)).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(session.RefreshToken)).Code,
		"a refresh token is not an access token")

	const newPassword = "Battery-staple-2"
	decode[any](t, a.request(t, http.MethodPut, "/api/v1/auth/password", map[string]any{"currentPassword": adminPassword, "newPassword": newPassword}, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}).Code)
	assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": newPassword}).Code)
}

func TestAuthAPI_RequiresSecondFactorOnceEnabled(t *testing.T) {
	a := newAppWithAdmin(t)
	credentials := map[string]any{"email": adminEmail, "password": adminPassword}

	w := a.request(t, http.MethodPost, "/api/v1/auth/2fa/setup", credentials, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = a.request(t, http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail, "code": code}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, a.login(t, credentials).Code)
	assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword, "token": "000000"}).Code)
	code, err = totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword, "token": code}).Code)

	decode[any](t, a.request(t, http.MethodPost, "/api/v1/auth/2fa/disable", credentials, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusOK, a.login(t, credentials).Code)
}

func TestAuthAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)

	requests := []struct {
		name, method, path string
		body               any
		creds              []credentials
		status             int
	}{
		{"register with malformed JSON", http.MethodPost, "/api/v1/auth/register", "{", nil, http.StatusBadRequest},
		{"register with an invalid email", http.MethodPost, "/api/v1/auth/register", map[string]any{"email": "admin", "password": adminPassword}, nil, http.StatusBadRequest},
		{"register with a weak password", http.MethodPost, "/api/v1/auth/register", map[string]any{"email": "second@example.com", "password": "password"}, nil, http.StatusBadRequest},
		{"register a second admin", http.MethodPost, "/api/v1/auth/register", map[string]any{"email": "second@example.com", "password": adminPassword}, nil, http.StatusBadRequest},
		{"login with malformed JSON", http.MethodPost, "/api/v1/auth/login", "{", nil, http.StatusBadRequest},
		{"login with an invalid email", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": "admin", "password": adminPassword}, nil, http.StatusBadRequest},
		{"login with a wrong password", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": adminEmail, "password": "Wrong-password-1"}, nil, http.StatusUnauthorized},
		{"refresh without a token", http.MethodPost, "/api/v1/auth/refresh", map[string]any{}, nil, http.StatusBadRequest},
		{"refresh with an invalid token", http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": "not-a-token"}, nil, http.StatusUnauthorized},
		{"refresh with an access token", http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": a.accessToken}, nil, http.StatusUnauthorized},
		{"password change to a weak password", http.MethodPut, "/api/v1/auth/password", map[string]any{"currentPassword": adminPassword, "newPassword": "password"}, []credentials{a.asAdmin}, http.StatusBadRequest},
		{"password change with a wrong current password", http.MethodPut, "/api/v1/auth/password", map[string]any{"currentPassword": "Wrong-password-1", "newPassword": "Battery-staple-2"}, []credentials{a.asAdmin}, http.StatusBadRequest},
		{"2FA setup with a wrong password", http.MethodPost, "/api/v1/auth/2fa/setup", map[string]any{"email": adminEmail, "password": "Wrong-password-1"}, []credentials{a.asAdmin}, http.StatusBadRequest},
		{"2FA verify without a code", http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail}, []credentials{a.asAdmin}, http.StatusBadRequest},
	}
	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			w := a.request(t, r.method, r.path, r.body, r.creds...)
			assert.Equal(t, r.status, w.Code, w.Body.String())
		})
	}

	assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}).Code)
}
