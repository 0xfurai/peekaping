package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
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

// Registrations that arrive together on a new install create exactly one admin. The
// others fail with "admin already exists", whatever email they register.
func TestAuthAPI_ConcurrentFirstRegistrationsCreateOneAdmin(t *testing.T) {
	const attempts = 10
	emails := map[string]func(n int) string{
		"different emails": func(n int) string { return fmt.Sprintf("admin%d@example.com", n) },
		"the same email":   func(int) string { return adminEmail },
	}
	for name, email := range emails {
		t.Run(name, func(t *testing.T) {
			a := newApp(t)
			responses := make(chan *httptest.ResponseRecorder, attempts)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for n := range attempts {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					responses <- a.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": email(n), "password": adminPassword})
				}()
			}
			close(start)
			wg.Wait()
			close(responses)

			created := 0
			for w := range responses {
				if w.Code == http.StatusCreated {
					created++
					continue
				}
				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), "admin already exists")
			}
			assert.Equal(t, 1, created, "registrations that created an admin")

			var admins int
			require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
				return db.NewSelect().Table("users").ColumnExpr("count(*)").Scan(context.Background(), &admins)
			}))
			assert.Equal(t, 1, admins)

			// The one that won is the owner of the one workspace, and the others left none
			var owners, workspaces int
			require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
				if err := db.NewSelect().Table("workspace_members").ColumnExpr("count(*)").Where("role = 'owner'").Scan(context.Background(), &owners); err != nil {
					return err
				}
				return db.NewSelect().Table("workspaces").ColumnExpr("count(*)").Scan(context.Background(), &workspaces)
			}))
			assert.Equal(t, 1, owners)
			assert.Equal(t, 1, workspaces)
		})
	}
}

// Signup makes the workspace, and the API shows nothing of it yet
func TestAuthAPI_RegisterMakesTheWorkspaceAndShowsNoWorkspaceFields(t *testing.T) {
	a := newApp(t)

	w := a.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": adminEmail, "password": adminPassword})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "instance")
	assert.NotContains(t, w.Body.String(), "workspace")
	var rows []struct {
		Name          string `bun:"name"`
		Role          string `bun:"role"`
		InstanceAdmin bool   `bun:"instance_admin"`
	}
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().TableExpr("workspace_members AS m").
			Join("JOIN workspaces AS w ON w.id = m.workspace_id").
			Join("JOIN users AS u ON u.id = m.user_id").
			ColumnExpr("w.name, m.role, u.instance_admin").
			Scan(context.Background(), &rows)
	}))
	require.Len(t, rows, 1)
	assert.Equal(t, "My workspace", rows[0].Name)
	assert.Equal(t, "owner", rows[0].Role)
	assert.True(t, rows[0].InstanceAdmin)
}

func (a *app) refresh(t *testing.T, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": refreshToken})
}

// Changing the password ends every refresh token issued before it, and the ones
// issued after it work.
func TestAuthAPI_PasswordChangeEndsEarlierRefreshTokens(t *testing.T) {
	a := newAppWithAdmin(t)
	const newPassword = "Battery-staple-2"

	earlier := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}), http.StatusOK)
	refreshedEarlier := decode[tokens](t, a.refresh(t, earlier.RefreshToken), http.StatusOK)

	decode[any](t, a.request(t, http.MethodPut, "/api/v1/auth/password", map[string]any{"currentPassword": adminPassword, "newPassword": newPassword}, a.asAdmin), http.StatusOK)

	assert.Equal(t, http.StatusUnauthorized, a.refresh(t, earlier.RefreshToken).Code, "a refresh token from before the change")
	assert.Equal(t, http.StatusUnauthorized, a.refresh(t, refreshedEarlier.RefreshToken).Code, "a refresh token that was refreshed from one before the change")

	later := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": newPassword}), http.StatusOK)
	refreshedLater := decode[tokens](t, a.refresh(t, later.RefreshToken), http.StatusOK)
	assert.Equal(t, http.StatusOK, a.refresh(t, refreshedLater.RefreshToken).Code, "a refresh token issued after the change")
}

// A refresh token issued by an earlier release carries no password fingerprint, so
// the user signs in again once after the upgrade.
func TestAuthAPI_RefusesRefreshTokenFromBeforeFingerprints(t *testing.T) {
	a := newAppWithAdmin(t)
	session := decode[struct {
		RefreshToken string `json:"refreshToken"`
		User         struct {
			ID string `json:"id"`
		} `json:"user"`
	}](t, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}), http.StatusOK)

	// Signed the way an earlier release signed it: the right secret, the right user, no fingerprint
	var secret string
	require.NoError(t, a.container.Invoke(func(settings setting.Service) error {
		stored, err := settings.GetByKey(context.Background(), shared.RefreshTokenSecretKey)
		if err == nil {
			secret = stored.Value
		}
		return err
	}))
	old, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &auth.Claims{
		UserID: session.User.ID,
		Email:  adminEmail,
		Type:   "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}).SignedString([]byte(secret))
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, a.refresh(t, old).Code)
	assert.Equal(t, http.StatusOK, a.refresh(t, session.RefreshToken).Code, "a refresh token from this release")
}

func TestAuthAPI_RequiresSecondFactorOnceEnabled(t *testing.T) {
	a := newAppWithAdmin(t)
	account := map[string]any{"email": adminEmail, "password": adminPassword}

	w := a.request(t, http.MethodPost, "/api/v1/auth/2fa/setup", account, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = a.request(t, http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail, "code": code}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, a.login(t, account).Code)
	assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword, "token": "000000"}).Code)
	code, err = totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword, "token": code}).Code)

	decode[any](t, a.request(t, http.MethodPost, "/api/v1/auth/2fa/disable", account, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusOK, a.login(t, account).Code)
}

func TestAuthAPI_RefusesAWrongSetupCodeAndLeavesSecondFactorOff(t *testing.T) {
	a := newAppWithAdmin(t)
	account := map[string]any{"email": adminEmail, "password": adminPassword}

	w := a.request(t, http.MethodPost, "/api/v1/auth/2fa/setup", account, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))

	// A code from five minutes ahead is outside the window the server accepts.
	wrong, err := totp.GenerateCode(setup.Secret, time.Now().Add(5*time.Minute))
	require.NoError(t, err)
	w = a.request(t, http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail, "code": wrong}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var refusal struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.False(t, refusal.Success)
	assert.Equal(t, "invalid 2FA code", refusal.Message)
	assert.Equal(t, http.StatusOK, a.login(t, account).Code, "2FA is still off, so a password is enough")

	right, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = a.request(t, http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": adminEmail, "code": right}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var accepted struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	assert.True(t, accepted.Success)
	assert.Equal(t, http.StatusUnauthorized, a.login(t, account).Code, "2FA is on, so a password is no longer enough")
}

func TestAuthAPI_LocksOutRepeatedFailedLogins(t *testing.T) {
	a := newAppWithAdmin(t)
	right := map[string]any{"email": adminEmail, "password": adminPassword}
	wrong := map[string]any{"email": adminEmail, "password": "Wrong-password-1"}

	for range maxFailedLogins - 1 {
		require.Equal(t, http.StatusUnauthorized, a.login(t, wrong).Code)
	}
	require.Equal(t, http.StatusOK, a.login(t, right).Code, "one failure short of the limit")

	for range maxFailedLogins {
		require.Equal(t, http.StatusUnauthorized, a.login(t, wrong).Code, "the successful login reset the count")
	}
	w := a.login(t, right)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "locked even with the right password: %s", w.Body.String())
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.InDelta(t, 60, retryAfter, 1, "the lockout lasts a minute")

	assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": "other@example.com", "password": adminPassword}).Code,
		"another email from the same IP isn't locked")
}

// forwardedFor sets X-Forwarded-For to a different address for each n, the way a client that
// spoofs the header to look like a new visitor on every request does.
func forwardedFor(n int) credentials {
	return func(req *http.Request) {
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.%d.%d", n/256, n%256))
	}
}

func TestAuthAPI_LockoutIgnoresSpoofedForwardedFor(t *testing.T) {
	a := newAppWithAdmin(t)
	wrong := map[string]any{"email": adminEmail, "password": "Wrong-password-1"}

	for n := range maxFailedLogins {
		w := a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(n))
		require.Equal(t, http.StatusUnauthorized, w.Code, "failed login %d", n+1)
	}
	w := a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(maxFailedLogins))
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "the next login is locked out: %s", w.Body.String())
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

// The apitest client connects from 192.0.2.1, so this makes it a trusted proxy whose
// X-Forwarded-For counts, the way a deployment behind its own gateway is set up.
func withTrustedClient(cfg *config.Config) { cfg.TrustedProxies = "192.0.2.0/24" }

// withAccountLimit lowers the failed logins that lock one account, so that a test reaches it
// without hundreds of requests.
func withAccountLimit(limit int) func(*config.Config) {
	return func(cfg *config.Config) { cfg.BruteforceAccountMaxAttempts = limit }
}

// withoutContentType sends the body without saying it is JSON, which the login handler accepts.
func withoutContentType(req *http.Request) { req.Header.Del("Content-Type") }

func TestAuthAPI_LocksAnAccountNoMatterWhereTheGuessesComeFrom(t *testing.T) {
	const accountLimit = 30 // above maxFailedLogins, so that no single address reaches its own limit
	a := newAppWithAdmin(t, withTrustedClient, withAccountLimit(accountLimit))
	wrong := map[string]any{"email": adminEmail, "password": "Wrong-password-1"}

	for n := range accountLimit {
		w := a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(n))
		require.Equal(t, http.StatusUnauthorized, w.Code, "failed login %d", n+1)
	}
	w := a.request(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": adminEmail, "password": adminPassword}, forwardedFor(accountLimit))
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "locked even with the right password, from an address that never failed: %s", w.Body.String())
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.InDelta(t, 60, retryAfter, 1, "a lock lasts the account lockout, a minute")

	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "other@example.com", "password": adminPassword}, forwardedFor(accountLimit+1)).Code,
		"another account isn't locked")
}

func TestAuthAPI_AccountLockoutCountsLoginsThatHideTheirEmail(t *testing.T) {
	const accountLimit = 30
	a := newAppWithAdmin(t, withTrustedClient, withAccountLimit(accountLimit))
	wrong := map[string]any{"EMAIL": "Admin@Example.com", "password": "Wrong-password-1"}

	for n := range accountLimit {
		w := a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(n), withoutContentType)
		require.Equal(t, http.StatusUnauthorized, w.Code, "failed login %d", n+1)
	}
	w := a.request(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": adminEmail, "password": adminPassword}, forwardedFor(accountLimit))
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "the account is locked: %s", w.Body.String())
}

func TestAuthAPI_SuccessfulLoginResetsBothCounters(t *testing.T) {
	const accountLimit = 30
	a := newAppWithAdmin(t, withTrustedClient, withAccountLimit(accountLimit))
	right := map[string]any{"email": adminEmail, "password": adminPassword}
	wrong := map[string]any{"email": adminEmail, "password": "Wrong-password-1"}
	fixed := func(req *http.Request) { req.Header.Set("X-Forwarded-For", "198.51.100.7") }

	// One address fails just under its own limit, and the account just under its limit
	for range maxFailedLogins - 1 {
		require.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, fixed).Code)
	}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, "/api/v1/auth/login", right, fixed).Code)

	// Without a reset, the address would be locked at once and the account soon after
	for n := range maxFailedLogins - 1 {
		require.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, fixed).Code,
			"failed login %d after the success", n+1)
	}

	// The account counter alone: addresses that never repeat
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, "/api/v1/auth/login", right, forwardedFor(0)).Code)
	for n := range accountLimit - 1 {
		require.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(n+1)).Code)
	}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, "/api/v1/auth/login", right, forwardedFor(accountLimit)).Code, "one failure short of the limit")
	for n := range accountLimit {
		require.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/login", wrong, forwardedFor(accountLimit+1+n)).Code,
			"failed login %d after the success", n+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, a.request(t, http.MethodPost, "/api/v1/auth/login", right, forwardedFor(2*accountLimit+1)).Code)
}

// loginBody is a login with padding before the email, as a client uses to push the email past
// what a guard reads.
func loginBody(t *testing.T, padding int, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"padding": strings.Repeat("a", padding), "email": adminEmail, "password": password})
	require.NoError(t, err)
	return string(body)
}

func TestAuthAPI_RefusesAnOversizedLoginBodyWithoutCountingIt(t *testing.T) {
	a := newAppWithAdmin(t)
	// Padding before the email, with the right password: if the oversized body skipped the
	// guards and reached the handler, it would sign in
	oversized := loginBody(t, 70<<10, adminPassword)

	for n := range 2 * maxFailedLogins {
		w := a.request(t, http.MethodPost, "/api/v1/auth/login", oversized, forwardedFor(n))
		require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "oversized login %d: %s", n+1, w.Body.String())
	}
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		a.request(t, http.MethodPost, "/api/v1/auth/login", oversized, withoutContentType).Code, "whatever the Content-Type")

	// They counted for nothing: the lockout still takes its usual number of failures
	wrong := map[string]any{"email": adminEmail, "password": "Wrong-password-1"}
	for n := range maxFailedLogins {
		require.Equal(t, http.StatusUnauthorized, a.login(t, wrong).Code, "failed login %d", n+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, a.login(t, wrong).Code)
}

func TestAuthAPI_RefusesA10MBLoginBody(t *testing.T) {
	a := newAppWithAdmin(t)
	// The right password, so that a login the limit let through would sign in
	w := a.request(t, http.MethodPost, "/api/v1/auth/login", loginBody(t, 10<<20, adminPassword))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
}

func TestAuthAPI_AccountLockoutCountsLoginsPaddedBeforeTheEmail(t *testing.T) {
	const accountLimit = 30
	a := newAppWithAdmin(t, withTrustedClient, withAccountLimit(accountLimit))
	padded := loginBody(t, 60<<10, "Wrong-password-1") // under the body limit

	for n := range accountLimit {
		w := a.request(t, http.MethodPost, "/api/v1/auth/login", padded, forwardedFor(n))
		require.Equal(t, http.StatusUnauthorized, w.Code, "failed login %d", n+1)
	}
	w := a.request(t, http.MethodPost, "/api/v1/auth/login", padded, forwardedFor(accountLimit))
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "the account is locked: %s", w.Body.String())
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
