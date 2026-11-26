package bruteforce

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/httpx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestKeyByIPAndBodyField_PicksTheKeyFromTheBodyOrFallsBackToTheAddress(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, want string
	}{
		{"JSON with a charset", "application/json; charset=utf-8", `{"email":"User@EXAMPLE.com"}`, "1.2.3.4:user@example.com"},
		{"JSON that is not valid", "application/json", `{"email":`, "1.2.3.4"},
		{"JSON whose field is not a string", "application/json", `{"email":42}`, "1.2.3.4"},
		{"JSON whose field is empty", "application/json", `{"email":""}`, "1.2.3.4"},
		{"JSON that is not an object", "application/json", `["a@example.com"]`, "1.2.3.4"},
		{"JSON with the field in another case", "application/json", `{"Email":"a@example.com"}`, "1.2.3.4"},
		{"a form", "application/x-www-form-urlencoded", `email=Form%40Example.com`, "1.2.3.4:form@example.com"},
		{"a form without the field", "application/x-www-form-urlencoded", `other=x`, "1.2.3.4"},
		{"a form with an empty field", "application/x-www-form-urlencoded", `email=`, "1.2.3.4"},
		{"a form with a charset, which is not the exact type", "application/x-www-form-urlencoded; charset=utf-8", `email=a%40example.com`, "1.2.3.4"},
		{"no Content-Type", "", `{"email":"a@example.com"}`, "1.2.3.4"},
		{"another Content-Type", "text/plain", `{"email":"a@example.com"}`, "1.2.3.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			r.RemoteAddr = "1.2.3.4:5678"

			key, err := KeyByIPAndBodyField("email", nil)(r)

			assert.NoError(t, err)
			assert.Equal(t, tc.want, key)
		})
	}

	t.Run("a JSON body is still there for the handler", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"email":"a@example.com"}`))
		r.Header.Set("Content-Type", "application/json")
		_, err := KeyByIPAndBodyField("email", nil)(r)
		assert.NoError(t, err)
		rest, readErr := io.ReadAll(r.Body)
		assert.NoError(t, readErr)
		assert.Equal(t, `{"email":"a@example.com"}`, string(rest))
	})
	t.Run("a JSON request without a body", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.Body = nil
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "1.2.3.4:5678"
		key, err := KeyByIPAndBodyField("email", nil)(r)
		assert.NoError(t, err)
		assert.Equal(t, "1.2.3.4", key)
	})
	t.Run("a form request without a body", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.Body = nil
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = "1.2.3.4:5678"
		key, err := KeyByIPAndBodyField("email", nil)(r)
		assert.NoError(t, err)
		assert.Equal(t, "1.2.3.4", key)
	})
}

// guardWith is a guard over a mock service whose extractor returns key and err, with an observed log
func guardWith(key string, err error) (*Guard, *MockService, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	service := &MockService{}
	cfg := Config{MaxAttempts: 2, Window: time.Minute, Lockout: 2 * time.Minute, FailureStatuses: []int{401}}
	return New(cfg, service, func(*http.Request) (string, error) { return key, err }, zap.New(core).Sugar()), service, logs
}

// serveStatus sends one request from 9.9.9.9 through the guard to a handler that answers status
func serveStatus(guard *Guard, status int) *httptest.ResponseRecorder {
	handler := guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "9.9.9.9:1000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestGuard_Middleware_CountsAnUnkeyedRequestUnderTheClientAddress(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		err  error
	}{
		{"an extractor that fails", "ignored", errors.New("boom")},
		{"an empty key", "", nil},
		{"an empty key and an error", "", errors.New("boom")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, service, _ := guardWith(tc.key, tc.err)
			service.On("IsLocked", mock.Anything, "9.9.9.9").Return(false, time.Time{}, nil)
			service.On("OnFailure", mock.Anything, "9.9.9.9", mock.Anything, time.Minute, 2, 2*time.Minute).Return(false, time.Time{}, nil)

			w := serveStatus(guard, http.StatusUnauthorized)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			service.AssertExpectations(t)
		})
	}
}

func TestGuard_Middleware_AnswersWhateverTheHandlerAnswersWhenCountingFails(t *testing.T) {
	t.Run("a failure that can't be counted", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, nil)
		service.On("OnFailure", mock.Anything, "k", mock.Anything, time.Minute, 2, 2*time.Minute).Return(false, time.Time{}, errors.New("redis down"))

		w := serveStatus(guard, http.StatusUnauthorized)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		service.AssertNotCalled(t, "Reset")
		entries := logs.FilterMessage("failed to record failure").All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
		assert.Equal(t, "k", entries[0].ContextMap()["key"])
		assert.Equal(t, "redis down", entries[0].ContextMap()["error"])
	})
	t.Run("a failure that locks the key", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, nil)
		service.On("OnFailure", mock.Anything, "k", mock.Anything, time.Minute, 2, 2*time.Minute).Return(true, time.Now().Add(time.Hour), nil)

		w := serveStatus(guard, http.StatusUnauthorized)

		assert.Equal(t, http.StatusUnauthorized, w.Code, "the client still gets the handler's answer")
		service.AssertNotCalled(t, "Reset")
		entries := logs.FilterMessage("account locked due to too many failures").All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
	})
	t.Run("a failure that does not lock the key", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, nil)
		service.On("OnFailure", mock.Anything, "k", mock.Anything, time.Minute, 2, 2*time.Minute).Return(false, time.Time{}, nil)

		serveStatus(guard, http.StatusUnauthorized)

		assert.Zero(t, logs.Len())
	})
	t.Run("a success that can't be reset", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, nil)
		service.On("Reset", mock.Anything, "k").Return(errors.New("redis down"))

		w := serveStatus(guard, http.StatusOK)

		assert.Equal(t, http.StatusOK, w.Code)
		entries := logs.FilterMessage("failed to reset bruteforce state").All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	})
}

func TestGuard_Middleware_ResetsOnlyOnA2xxAndCountsOnlyTheFailureStatuses(t *testing.T) {
	for _, tc := range []struct {
		status    int
		resets    bool
		countsAsF bool
	}{
		{http.StatusOK, true, false},
		{http.StatusNoContent, true, false},
		{299, true, false},
		{http.StatusMultipleChoices, false, false},
		{http.StatusBadRequest, false, false},
		{http.StatusUnauthorized, false, true},
		{http.StatusForbidden, false, false}, // the guard's failure statuses are 401 only here
		{http.StatusInternalServerError, false, false},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			guard, service, _ := guardWith("k", nil)
			service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, nil)
			service.On("Reset", mock.Anything, "k").Return(nil).Maybe()
			service.On("OnFailure", mock.Anything, "k", mock.Anything, time.Minute, 2, 2*time.Minute).Return(false, time.Time{}, nil).Maybe()

			w := serveStatus(guard, tc.status)

			assert.Equal(t, tc.status, w.Code)
			if tc.resets {
				service.AssertCalled(t, "Reset", mock.Anything, "k")
			} else {
				service.AssertNotCalled(t, "Reset", mock.Anything, "k")
			}
			if tc.countsAsF {
				service.AssertCalled(t, "OnFailure", mock.Anything, "k", mock.Anything, time.Minute, 2, 2*time.Minute)
			} else {
				service.AssertNotCalled(t, "OnFailure", mock.Anything, "k", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

func TestGuard_Middleware_ReadsALockInOrder(t *testing.T) {
	t.Run("a lock that has not ended blocks with its Retry-After and never runs the handler", func(t *testing.T) {
		guard, service, _ := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(true, time.Now().Add(90*time.Second), nil)
		ran := false
		handler := guard.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.False(t, ran)
		assert.NotEmpty(t, w.Header().Get("Retry-After"))
		service.AssertNotCalled(t, "OnFailure")
		service.AssertNotCalled(t, "Reset")
	})
	t.Run("a lock that ended passes the request without counting it", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(true, time.Now().Add(-time.Second), nil)

		w := serveStatus(guard, http.StatusUnauthorized)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		service.AssertNotCalled(t, "OnFailure")
		service.AssertNotCalled(t, "Reset")
		assert.Equal(t, 1, logs.FilterMessage("lock expired, allowing request").Len())
	})
	t.Run("a lock that can't be read passes the request without counting it", func(t *testing.T) {
		guard, service, logs := guardWith("k", nil)
		service.On("IsLocked", mock.Anything, "k").Return(false, time.Time{}, errors.New("redis down"))

		w := serveStatus(guard, http.StatusUnauthorized)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		service.AssertNotCalled(t, "OnFailure")
		service.AssertNotCalled(t, "Reset")
		assert.Equal(t, 1, logs.FilterMessage("failed to check if locked").Len())
	})
	t.Run("a key without a lock error blocks through a custom answer", func(t *testing.T) {
		guard, service, _ := guardWith("k", nil)
		guard.cfg.OnBlocked = func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
			w.WriteHeader(http.StatusTeapot)
		}
		service.On("IsLocked", mock.Anything, "k").Return(true, time.Now().Add(time.Minute), nil)

		w := serveStatus(guard, http.StatusOK)

		assert.Equal(t, http.StatusTeapot, w.Code)
	})
}

func TestGuard_Middleware_TrustsTheProxiesItIsGivenForTheFallbackAddress(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies("10.0.0.0/8")
	require.NoError(t, err)
	guard, service, _ := guardWith("", nil)
	guard.cfg.TrustedProxies = trusted
	service.On("IsLocked", mock.Anything, "198.51.100.7").Return(false, time.Time{}, nil)
	service.On("Reset", mock.Anything, "198.51.100.7").Return(nil)
	handler := guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")

	handler.ServeHTTP(httptest.NewRecorder(), r)

	service.AssertExpectations(t)
}
