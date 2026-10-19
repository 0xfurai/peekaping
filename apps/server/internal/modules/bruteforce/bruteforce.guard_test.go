package bruteforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockService implements the Service interface for Guard tests
// Only methods used by Guard are implemented
type MockService struct {
	mock.Mock
}

func (m *MockService) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Get(1).(time.Time), args.Error(2)
}
func (m *MockService) OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error) {
	args := m.Called(ctx, key, now, window, max, lockout)
	return args.Bool(0), args.Get(1).(time.Time), args.Error(2)
}
func (m *MockService) Reset(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func TestGuard_isFailure(t *testing.T) {
	guard := &Guard{cfg: Config{FailureStatuses: []int{401, 403}}}
	assert.True(t, guard.isFailure(401))
	assert.True(t, guard.isFailure(403))
	assert.False(t, guard.isFailure(200))
}

func TestGuard_block_default(t *testing.T) {
	guard := &Guard{cfg: Config{}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	guard.block(w, req, 10*time.Second)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Contains(t, resp["message"], "too many attempts")
	assert.Equal(t, float64(10), resp["retry_after"])
}

func TestGuard_block_negative_retry(t *testing.T) {
	guard := &Guard{cfg: Config{}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	guard.block(w, req, -5*time.Second) // Negative retry time
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Contains(t, resp["message"], "too many attempts")
	assert.Equal(t, float64(0), resp["retry_after"]) // Should be clamped to 0
}

func TestGuard_block_custom(t *testing.T) {
	called := false
	guard := &Guard{cfg: Config{OnBlocked: func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
		called = true
		httpx.Text(w, 444, "blocked")
	}}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	guard.block(w, req, 5*time.Second)
	assert.True(t, called)
	assert.Equal(t, 444, w.Code)
	assert.Equal(t, "blocked", w.Body.String())
}

func TestKeyByIPAndBodyField_JSON(t *testing.T) {

	body := map[string]string{"email": "User@EXAMPLE.com"}
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "1.2.3.4:5678"
	key, err := KeyByIPAndBodyField("email")(r)
	assert.NoError(t, err)
	assert.Equal(t, "1.2.3.4:user@example.com", key)
}

func TestKeyByIPAndBodyField_JSON_no_field(t *testing.T) {

	body := map[string]string{"other": "value"}
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "5.6.7.8:1234"
	key, err := KeyByIPAndBodyField("email")(r)
	assert.NoError(t, err)
	assert.Equal(t, "5.6.7.8", key)
}

func TestKeyByIPAndBodyField_Form(t *testing.T) {

	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("email=TestUser")))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "9.8.7.6:4321"
	r.ParseForm()
	r.PostForm.Set("email", "TestUser")
	key, err := KeyByIPAndBodyField("email")(r)
	assert.NoError(t, err)
	assert.Equal(t, "9.8.7.6:testuser", key)
}

func TestKeyByIPAndBodyField_Error(t *testing.T) {

	r := httptest.NewRequest("POST", "/", &errReader{})
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "1.1.1.1:1111"
	key, err := KeyByIPAndBodyField("email")(r)
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", key)
}

type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("fail")
}

func (e *errReader) Close() error { return nil }

func TestNew_defaults(t *testing.T) {
	guard := New(Config{}, &MockService{}, func(*http.Request) (string, error) { return "k", nil }, zap.NewNop().Sugar())
	assert.Equal(t, 5, guard.cfg.MaxAttempts)
	assert.Equal(t, time.Minute, guard.cfg.Window)
	assert.Equal(t, 1*time.Minute, guard.cfg.Lockout)
	assert.Equal(t, []int{401, 403}, guard.cfg.FailureStatuses)
}

// serveLogin sends one request through the guard to a login handler that
// succeeds, and reports whether the handler ran.
func serveLogin(guard *Guard) (*httptest.ResponseRecorder, bool) {
	ran := false
	router := httpx.NewRouter()
	router.Handle("POST", "/login", func(w http.ResponseWriter, r *http.Request) {
		ran = true
		w.WriteHeader(http.StatusOK)
	}, guard.Middleware())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/login", nil))
	return w, ran
}

func TestGuard_Middleware(t *testing.T) {

	mockSvc := &MockService{}
	logger := zap.NewNop().Sugar()
	key := "ip:user"
	cfg := Config{MaxAttempts: 2, Window: time.Minute, Lockout: 2 * time.Minute, FailureStatuses: []int{401}}
	guard := New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)

	// Not locked, success (should call Reset)
	mockSvc.On("IsLocked", mock.Anything, key).Return(false, time.Time{}, nil)
	mockSvc.On("Reset", mock.Anything, key).Return(nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.Background())
	guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, req)
	mockSvc.AssertCalled(t, "IsLocked", mock.Anything, key)
	mockSvc.AssertCalled(t, "Reset", mock.Anything, key)

	// Not locked, non-success status (should NOT call Reset)
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(false, time.Time{}, nil)
	// Note: NOT setting up Reset expectation
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.Background())
	guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) })).ServeHTTP(w, req) // Non-success status
	mockSvc.AssertCalled(t, "IsLocked", mock.Anything, key)
	mockSvc.AssertNotCalled(t, "Reset") // Should NOT call Reset

	// Not locked, 500 status (should NOT call Reset)
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(false, time.Time{}, nil)
	// Note: NOT setting up Reset expectation
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.Background())
	guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })).ServeHTTP(w, req) // Non-success status
	mockSvc.AssertCalled(t, "IsLocked", mock.Anything, key)
	mockSvc.AssertNotCalled(t, "Reset") // Should NOT call Reset

	// Locked, should block
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(true, time.Now().Add(time.Minute), nil)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.Background())
	guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Locked but expired (race condition fix), should allow request
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(true, time.Now().Add(-time.Second), nil) // Expired lock
	mockSvc.On("Reset", mock.Anything, key).Return(nil).Maybe()
	w, ran := serveLogin(guard)
	assert.True(t, ran, "the login handler runs")
	assert.Equal(t, http.StatusOK, w.Code)

	// IsLocked error, should log and continue
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(false, time.Time{}, errors.New("fail"))
	mockSvc.On("Reset", mock.Anything, key).Return(nil).Maybe()
	w, ran = serveLogin(guard)
	assert.True(t, ran, "the login handler runs")
	assert.Equal(t, http.StatusOK, w.Code)

	// Failure status, should call OnFailure
	mockSvc = &MockService{}
	guard = New(cfg, mockSvc, func(r *http.Request) (string, error) { return key, nil }, logger)
	mockSvc.On("IsLocked", mock.Anything, key).Return(false, time.Time{}, nil)
	mockSvc.On("OnFailure", mock.Anything, key, mock.Anything, cfg.Window, cfg.MaxAttempts, cfg.Lockout).Return(true, time.Now().Add(cfg.Lockout), nil)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.Background())
	guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })).ServeHTTP(w, req)
	mockSvc.AssertCalled(t, "OnFailure", mock.Anything, key, mock.Anything, cfg.Window, cfg.MaxAttempts, cfg.Lockout)
}

func TestNewGuard(t *testing.T) {
	// Test the NewGuard function from dig.go
	mockService := &MockService{}
	logger := zap.NewNop().Sugar()
	cfg := &config.Config{
		BruteforceMaxAttempts: 10,
		BruteforceWindow:      2 * time.Minute,
		BruteforceLockout:     30 * time.Minute,
	}

	guard := NewGuard(mockService, logger, cfg)
	assert.NotNil(t, guard)
	assert.Equal(t, 10, guard.cfg.MaxAttempts)
	assert.Equal(t, 2*time.Minute, guard.cfg.Window)
	assert.Equal(t, 30*time.Minute, guard.cfg.Lockout)
	assert.Equal(t, []int{401, 403}, guard.cfg.FailureStatuses)
}
