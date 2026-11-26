package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

//go:noinline
func failingHandler(http.ResponseWriter, *http.Request) {
	panic(errors.New("monitor https://secret.example.com/health?token=abc is broken"))
}

func serve(t *testing.T, handler http.HandlerFunc) (*httptest.ResponseRecorder, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.ErrorLevel)
	h := httpx.Recovery(zap.New(core).Sugar())(handler)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/monitors", nil))
	return w, logs
}

func TestRecovery_ReportsAPanicAndStillAnswers500(t *testing.T) {
	recorder := reportingtest.Install(t)

	w, logs := serve(t, failingHandler)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "the panic is still recovered")
	events := recorder.Events()
	require.Len(t, events, 1)
	assert.Equal(t, "http", events[0].Tags["component"])
	assert.Equal(t, "*errors.errorString", events[0].Exception[0].Type)
	frames := events[0].Exception[0].Stacktrace.Frames
	assert.Equal(t, "failingHandler", frames[len(frames)-1].Function)
	assert.Equal(t, 1, logs.Len())
}

func TestRecovery_ReportsNothingWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)

	w, _ := serve(t, failingHandler)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, recorder.Events())
}

func TestRecovery_LogsTheStackOfThePanic(t *testing.T) {
	_, logs := serve(t, failingHandler)

	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Contains(t, fields["stack"], "failingHandler", "a panic that is only a message can't be found in the code")
	assert.Contains(t, fields["error"], "is broken")
}

// net/http answers http.ErrAbortHandler with an aborted connection and no log. A reverse proxy
// panics with it when the upstream breaks off, so recovering it would answer 500 on top.
func TestRecovery_PanicsAgainWithErrAbortHandler(t *testing.T) {
	recorder := reportingtest.Install(t)
	core, logs := observer.New(zap.ErrorLevel)
	h := httpx.Recovery(zap.New(core).Sugar())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	w := httptest.NewRecorder()

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	})

	assert.Empty(t, recorder.Events(), "an aborted response is not a crash")
	assert.Zero(t, logs.Len())
	assert.False(t, w.Flushed)
	assert.Equal(t, http.StatusOK, w.Code, "nothing was written")
}

// AC5: only a panic is reported
func TestRecovery_ReportsNothingForResponsesThatAreNotPanics(t *testing.T) {
	recorder := reportingtest.Install(t)

	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
	} {
		w, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", status) })
		assert.Equal(t, status, w.Code)
	}

	assert.Empty(t, recorder.Events())
}
