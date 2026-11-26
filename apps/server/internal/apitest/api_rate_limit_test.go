package apitest

import (
	"net/http"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-159: API_RATE_LIMIT_PER_WORKSPACE and API_RATE_LIMIT_PER_API_KEY cap the requests per minute of
// a workspace and of one of its API keys. 0, the default, is off. A request over either answers 429
// with Retry-After in seconds.

const keysPath = "/api/v1/api-keys"

// withAPIRateLimits sets the two limits and the window they count over
func withAPIRateLimits(perWorkspace, perKey int, window time.Duration) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.APIRateLimitPerWorkspace, cfg.APIRateLimitPerAPIKey, cfg.APIRateLimitWindow = perWorkspace, perKey, window
	}
}

// limitedReplica is a replica of the install that first starts as, with the limits on. It has
// counts of its own, which the setup of the install (a request to make the API key) has not touched.
func limitedReplica(t *testing.T, first *app, perWorkspace, perKey int, window time.Duration) *app {
	t.Helper()
	return newReplica(t, first, withAPIRateLimits(perWorkspace, perKey, window))
}

// read is a request to a route of the workspace
func (a *app) read(t *testing.T, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodGet, "/api/v1/monitors", nil, creds...)
}

// newKey makes an API key in the admin's workspace and returns the credentials of the key
func (a *app) newKey(t *testing.T, name string) credentials {
	t.Helper()
	created := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, keysPath, map[string]string{"name": name}, a.asAdmin), http.StatusCreated)
	return withHeader("X-API-Key", created.Token)
}

func assertTooManyRequests(t *testing.T, w *httpRecorder, msg string) {
	t.Helper()
	require.Equal(t, http.StatusTooManyRequests, w.Code, msg+": "+w.Body.String())
	assertRetryAfterWithin(t, w, time.Minute)
	assert.JSONEq(t, `{"message":"Too many requests, try again later","data":null}`, w.Body.String(), msg)
}

func TestAPIRateLimit_AWorkspaceMakesTheLimitAndNoMoreRequestsAMinute(t *testing.T) {
	first := newAppWithAdmin(t)
	a := limitedReplica(t, first, 5, 0, 0)

	// a signed-in user and an API key of the workspace share its limit
	for i := range 3 {
		require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code, "request %d by the user", i+1)
	}
	for i := range 2 {
		require.Equal(t, http.StatusOK, a.read(t, a.withAPIKey).Code, "request %d by the key", i+4)
	}

	assertTooManyRequests(t, a.read(t, a.asAdmin), "the user")
	assertTooManyRequests(t, a.read(t, a.withAPIKey), "the key")
}

func TestAPIRateLimit_AnotherWorkspaceIsNotHeldBack(t *testing.T) {
	first := newAppWithAdmin(t)
	a := limitedReplica(t, first, 2, 0, 0)
	elsewhere := a.createWorkspace(t, "Elsewhere")
	bob := a.userIn(t, "bob@example.com", elsewhere, workspace.RoleOwner)
	for range 2 {
		require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)
	}
	assertTooManyRequests(t, a.read(t, a.asAdmin), "the workspace over its limit")

	// the same user in the workspace that is not over, and a user of it
	assert.Equal(t, http.StatusOK, a.read(t, bob).Code)
	assert.Equal(t, http.StatusOK, a.read(t, bob).Code)
	assertTooManyRequests(t, a.read(t, bob), "the second workspace has the limit too, of its own")
}

func TestAPIRateLimit_RequestsPassAgainOnceTheWindowHasPassed(t *testing.T) {
	first := newAppWithAdmin(t)
	a := limitedReplica(t, first, 2, 0, time.Second)
	require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)
	require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)

	limited := a.read(t, a.asAdmin)

	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "1", limited.Header().Get("Retry-After"), "a wait under a second is one")
	time.Sleep(1100 * time.Millisecond)
	assert.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)
}

func TestAPIRateLimit_EachAPIKeyHasALimitAndCountsTowardTheWorkspacesToo(t *testing.T) {
	first := newAppWithAdmin(t)
	secondKey := first.newKey(t, "second")
	a := limitedReplica(t, first, 5, 3, 0)

	for i := range 3 {
		require.Equal(t, http.StatusOK, a.read(t, a.withAPIKey).Code, "request %d by the first key", i+1)
	}
	assertTooManyRequests(t, a.read(t, a.withAPIKey), "the first key is over its own limit")

	// the other key has a count of its own
	for i := range 2 {
		require.Equal(t, http.StatusOK, a.read(t, secondKey).Code, "request %d by the second key", i+1)
	}
	// and all five requests that passed are the workspace's, whoever made them
	assertTooManyRequests(t, a.read(t, secondKey), "the workspace is over its limit, the second key is not")
	assertTooManyRequests(t, a.read(t, a.asAdmin), "the signed-in user of the workspace")
}

func TestAPIRateLimit_AKeyOverItsLimitDoesNotUseUpTheWorkspacesShare(t *testing.T) {
	first := newAppWithAdmin(t)
	secondKey := first.newKey(t, "second")
	a := limitedReplica(t, first, 4, 2, 0)
	for range 2 {
		require.Equal(t, http.StatusOK, a.read(t, a.withAPIKey).Code)
	}

	for range 10 {
		assertTooManyRequests(t, a.read(t, a.withAPIKey), "a key that keeps trying")
	}

	assert.Equal(t, http.StatusOK, a.read(t, secondKey).Code)
	assert.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)
}

func TestAPIRateLimit_TheKeyLimitDoesNotHoldBackASignedInUser(t *testing.T) {
	first := newAppWithAdmin(t)
	a := limitedReplica(t, first, 0, 2, 0)
	for range 2 {
		require.Equal(t, http.StatusOK, a.read(t, a.withAPIKey).Code)
	}
	assertTooManyRequests(t, a.read(t, a.withAPIKey), "the key")

	for range 10 {
		require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code, "a signed-in user has no key limit, and the workspace has none")
	}
}

func TestAPIRateLimit_RequestsOutsideAWorkspaceAreNotCounted(t *testing.T) {
	first := newAppWithAdmin(t)
	a := limitedReplica(t, first, 1, 1, 0)
	require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code)
	assertTooManyRequests(t, a.read(t, a.asAdmin), "the workspace is over its limit")

	for range 5 {
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin).Code, "/me")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/auth/config", nil).Code, "/auth/config")
	}
}

func TestAPIRateLimit_UnsetCountsNothing(t *testing.T) {
	a := newAppWithAdmin(t)

	for i := range 150 {
		require.Equal(t, http.StatusOK, a.read(t, a.asAdmin).Code, "request %d by the user", i+1)
		require.Equal(t, http.StatusOK, a.read(t, a.withAPIKey).Code, "request %d by the key", i+1)
	}
}
