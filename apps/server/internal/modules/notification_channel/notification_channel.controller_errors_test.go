package notification_channel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/netguard"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// failingStore is a store whose partial update fails with updateErr
type failingStore struct {
	*channelStore
	updateErr error
}

func (s failingStore) UpdatePartial(context.Context, workspace.ID, string, *PartialUpdateDto) (*Model, error) {
	return nil, s.updateErr
}

// doOutsideAWorkspace sends a request that runs in no workspace
func (f *secretsFixture) doOutsideAWorkspace(method, path, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestControllerUpdatePartialRefuses(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"a body of the wrong shape", `{"name":5}`, http.StatusBadRequest, "cannot unmarshal number"},
		{"a type that has no provider, without a config", `{"type":"carrier-pigeon"}`, http.StatusBadRequest, "Unsupported notification type"},
		{"a type that has no provider, with a config", `{"type":"carrier-pigeon","config":"{}"}`, http.StatusBadRequest, "Unsupported notification type"},
		{"a config the provider refuses", `{"config":"{}"}`, http.StatusBadRequest, "Invalid config: token is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)

			rec := f.do(http.MethodPatch, "/channel-1", tc.body)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.wantBody)
			assert.Equal(t, storedChannelConfig, f.stored("channel-1"))
		})
	}

	t.Run("a request outside a workspace", func(t *testing.T) {
		f := newSecretsFixture(t, storedChannelConfig)

		rec := f.doOutsideAWorkspace(http.MethodPatch, "/channel-1", `{"name":"Renamed"}`)

		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.Equal(t, "Alerts", f.store.channels["channel-1"].Name)
	})
}

func TestControllerUpdatePartialWhenTheStoreFails(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"a channel that was deleted meanwhile", ErrNotFound, http.StatusNotFound, "Notification channel not found"},
		{"a failure", errors.New("disk full"), http.StatusInternalServerError, "Internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)
			providers, _ := newSecretTestRegistry()
			controller := NewController(failingStore{channelStore: f.store, updateErr: tc.err}, providers, zap.NewNop().Sugar())
			f.router = httpx.NewRouter()
			httpx.Register(f.router, "PATCH", "/{id}", huma.Operation{Errors: []int{400, 404, 500}}, controller.UpdatePartial)

			rec := f.do(http.MethodPatch, "/channel-1", `{"name":"Renamed"}`)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.wantBody)
			assert.NotContains(t, rec.Body.String(), "disk full")
		})
	}
}

func TestControllerTestRefuses(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"a body of the wrong shape", `{"name":5}`, http.StatusBadRequest, "Invalid request body"},
		{"a type that has no provider", testBody("", "carrier-pigeon", `{}`), http.StatusBadRequest, "Unsupported notification type"},
		{"a config the provider refuses", testBody("", "test-secrets", `{"server":"https://server.example"}`), http.StatusBadRequest, "Invalid config: token is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)

			rec := f.do(http.MethodPost, "/test", tc.body)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.wantBody)
			assert.Empty(t, f.provider.sent, "nothing is sent")
		})
	}

	t.Run("a request outside a workspace", func(t *testing.T) {
		f := newSecretsFixture(t, storedChannelConfig)

		rec := f.doOutsideAWorkspace(http.MethodPost, "/test", testBody("", "test-secrets", `{"token":"fresh-token"}`))

		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.Empty(t, f.provider.sent, "nothing is sent")
	})
}

// A send that fails because of the address of the channel, or of the recipient, is the caller's to
// fix, so the test answers 400 with the reason; any other failure is a 500
func TestControllerTestAnswersAFailureTheCallerCanFix(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"an address the install blocks", fmt.Errorf("Post: %w: 10.0.0.5", netguard.ErrBlocked), http.StatusBadRequest, "Failed to send test notification: Post: " + netguard.ErrBlocked.Error() + ": 10.0.0.5"},
		{"a recipient who has not verified their address", providers.ErrRecipientNotVerified, http.StatusBadRequest, "Failed to send test notification: " + providers.ErrRecipientNotVerified.Error()},
		{"any other failure", errors.New("connection refused"), http.StatusInternalServerError, "Failed to send test notification: connection refused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, storedChannelConfig)
			f.provider.failWith = func(string) error { return tc.err }

			rec := f.do(http.MethodPost, "/test", testBody("", "test-secrets", `{"server":"https://server.example","token":"fresh-token"}`))

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, responseMessage(t, rec), tc.wantBody)
			assert.Len(t, f.provider.sent, 1)
		})
	}
}
