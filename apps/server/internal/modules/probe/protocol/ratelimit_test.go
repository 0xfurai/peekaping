package protocol

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// clock is a time the test moves by hand
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// as is a request the way the bearer middleware leaves it: the probe and the instance are on its context
func as(method, path, probeID, instanceID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Pattern = method + " " + path
	req = httpx.WithValue(req, contextProbe, &probe.Model{ID: probeID})
	return httpx.WithValue(req, contextInstance, instanceID)
}

// limited is a limiter in front of a handler that counts the requests it lets through
func limited(l *RateLimiter) (http.Handler, *atomic.Int64) {
	var served atomic.Int64
	return l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.WriteHeader(http.StatusOK)
	})), &served
}

func status(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestRateLimiter_AllowsABurstOfFiveThenAnswers429(t *testing.T) {
	clk := newClock()
	handler, served := limited(newRateLimiter(1, 5, clk.Now))

	for n := 1; n <= 5; n++ {
		assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p1", "i1")).Code, "request %d", n)
	}
	w := status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"message":"too many requests","data":null}`, w.Body.String())
	assert.EqualValues(t, 5, served.Load(), "the denied request never reaches the handler")
}

func TestRateLimiter_RefillsOneRequestASecond(t *testing.T) {
	clk := newClock()
	handler, _ := limited(newRateLimiter(1, 5, clk.Now))
	for range 5 {
		status(handler, as("GET", "/probe/v1/assignments", "p1", "i1"))
	}
	require.Equal(t, http.StatusTooManyRequests, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code)

	clk.Advance(999 * time.Millisecond)
	assert.Equal(t, http.StatusTooManyRequests, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code, "not yet a second")

	clk.Advance(time.Millisecond)
	assert.Equal(t, http.StatusOK, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code, "a second since the burst ran out")
	assert.Equal(t, http.StatusTooManyRequests, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code, "and one request only")

	clk.Advance(time.Hour)
	for n := 1; n <= 5; n++ {
		assert.Equal(t, http.StatusOK, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code, "the burst is back after a rest, request %d", n)
	}
	assert.Equal(t, http.StatusTooManyRequests, status(handler, as("GET", "/probe/v1/assignments", "p1", "i1")).Code, "but never more than the burst")
}

func TestRateLimiter_KeepsABucketForEachProbeInstanceAndRoute(t *testing.T) {
	clk := newClock()
	handler, _ := limited(newRateLimiter(1, 5, clk.Now))
	for range 5 {
		status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))
	}
	require.Equal(t, http.StatusTooManyRequests, status(handler, as("POST", "/probe/v1/hello", "p1", "i1")).Code)

	for name, req := range map[string]*http.Request{
		"another instance of the probe": as("POST", "/probe/v1/hello", "p1", "i2"),
		"another route":                 as("POST", "/probe/v1/bye", "p1", "i1"),
		"another method on the route":   as("GET", "/probe/v1/hello", "p1", "i1"),
		// An instance id is the probe's own to choose, so another probe's must not drain this one's budget
		"another probe with the same instance id": as("POST", "/probe/v1/hello", "p2", "i1"),
	} {
		assert.Equal(t, http.StatusOK, status(handler, req).Code, name)
	}
}

func TestRateLimit_HelloIsAlsoLimitedPerProbe(t *testing.T) {
	clk := newClock()
	handler, served := limited(newRateLimiter(1, 5, clk.Now))

	// Hello takes any new instance id, so a probe that rotates them gets a fresh instance bucket
	// each time. Its own bucket for the route is what stops it.
	for n := 1; n <= 10; n++ {
		assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p1", "instance-"+strconv.Itoa(n))).Code, "hello %d is within the burst of 10", n)
	}
	w := status(handler, as("POST", "/probe/v1/hello", "p1", "instance-11"))

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"message":"too many requests","data":null}`, w.Body.String())
	assert.EqualValues(t, 10, served.Load(), "the denied hello never reaches the handler")

	assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p2", "instance-1")).Code, "another probe has a limit of its own")
	for n := 1; n <= 11; n++ {
		assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/bye", "p1", "instance-"+strconv.Itoa(n))).Code, "only hello has the limit per probe, bye %d", n)
	}

	clk.Advance(time.Second)
	assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p1", "instance-12")).Code, "one hello a second once the burst is used")
	assert.Equal(t, http.StatusTooManyRequests, status(handler, as("POST", "/probe/v1/hello", "p1", "instance-13")).Code, "and one only")
}

func TestRateLimit_ADeniedHelloDoesNotDrainTheProbesBucket(t *testing.T) {
	clk := newClock()
	handler, _ := limited(newRateLimiter(1, 5, clk.Now))
	for range 5 {
		require.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p1", "i1")).Code)
	}

	// The instance's own bucket is empty, and the hellos it denies must not take from the probe's:
	// five of the probe's ten are left for its other instances
	for range 20 {
		require.Equal(t, http.StatusTooManyRequests, status(handler, as("POST", "/probe/v1/hello", "p1", "i1")).Code)
	}
	for n := 1; n <= 5; n++ {
		assert.Equal(t, http.StatusOK, status(handler, as("POST", "/probe/v1/hello", "p1", "other-"+strconv.Itoa(n))).Code, "hello %d of another instance", n)
	}
	assert.Equal(t, http.StatusTooManyRequests, status(handler, as("POST", "/probe/v1/hello", "p1", "other-6")).Code)
}

func TestRateLimit_AProbesHelloBucketIsForgottenWithTheRest(t *testing.T) {
	clk := newClock()
	limiter := newRateLimiter(1, 5, clk.Now)
	handler, _ := limited(limiter)
	status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))
	require.Equal(t, 2, limiter.size(), "the instance's bucket and the probe's")

	clk.Advance(2 * time.Hour)
	status(handler, as("POST", "/probe/v1/hello", "p2", "i1"))

	assert.Equal(t, 2, limiter.size(), "p1's two are gone, and p2's two are left")
	assert.False(t, limiter.has("p1", "i1", "POST /probe/v1/hello"))
}

func TestRateLimiter_KeysByThePathOfARequestTheRouterDidNotMatch(t *testing.T) {
	clk := newClock()
	handler, _ := limited(newRateLimiter(1, 1, clk.Now))
	hello := as("POST", "/probe/v1/hello", "p1", "i1")
	hello.Pattern = ""
	bye := as("POST", "/probe/v1/bye", "p1", "i1")
	bye.Pattern = ""

	assert.Equal(t, http.StatusOK, status(handler, hello).Code)
	assert.Equal(t, http.StatusOK, status(handler, bye).Code, "a route of its own")
	assert.Equal(t, http.StatusTooManyRequests, status(handler, hello).Code)
}

func TestRateLimiter_RetryAfterIsTheSecondsUntilTheNextRequest(t *testing.T) {
	for _, tc := range []struct {
		limit rate.Limit
		want  string
	}{{1, "1"}, {2, "1"}, {0.5, "2"}, {0.1, "10"}, {0.3, "4"}} {
		clk := newClock()
		handler, _ := limited(newRateLimiter(tc.limit, 1, clk.Now))
		status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))

		w := status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))

		require.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Equal(t, tc.want, w.Header().Get("Retry-After"), "at %v requests a second", tc.limit)
	}
}

func TestRateLimiter_ForgetsAnInstanceAfterAnHourWithoutRequests(t *testing.T) {
	clk := newClock()
	limiter := newRateLimiter(1, 5, clk.Now)
	handler, _ := limited(limiter)
	status(handler, as("GET", "/probe/v1/assignments", "p1", "old"))
	clk.Advance(30 * time.Minute)
	status(handler, as("GET", "/probe/v1/assignments", "p1", "recent"))
	require.Equal(t, 2, limiter.size())

	// "old" has been idle for 59 minutes: not yet
	clk.Advance(29 * time.Minute)
	status(handler, as("GET", "/probe/v1/assignments", "p1", "new-1"))
	assert.Equal(t, 3, limiter.size())

	// "old" has been idle for 70 minutes, "recent" for 40
	clk.Advance(11 * time.Minute)
	status(handler, as("GET", "/probe/v1/assignments", "p1", "new-2"))
	assert.Equal(t, 3, limiter.size(), "old is gone, and recent, new-1 and new-2 are left")

	clk.Advance(2 * time.Hour)
	status(handler, as("GET", "/probe/v1/assignments", "p1", "new-3"))
	assert.Equal(t, 1, limiter.size(), "what has been idle for two hours is gone")
}

func TestRateLimiter_AnInstanceInUseIsNotForgotten(t *testing.T) {
	clk := newClock()
	limiter := newRateLimiter(1, 5, clk.Now)
	handler, _ := limited(limiter)

	// "busy" is heard from every half minute for two hours, while a new instance appears each time
	// and makes the limiter look for what to forget
	for n := range 240 {
		status(handler, as("GET", "/probe/v1/assignments", "p1", "busy"))
		status(handler, as("GET", "/probe/v1/assignments", "p1", "passer-by-"+strconv.Itoa(n)))
		clk.Advance(30 * time.Second)
	}

	// The passers-by of the last hour are left, 120 of them and "busy"
	assert.InDelta(t, 121, limiter.size(), 25, "busy stays and the passers-by of the last hour or so with it")
	assert.True(t, limiter.has("p1", "busy", "GET /probe/v1/assignments"))
	assert.False(t, limiter.has("p1", "passer-by-0", "GET /probe/v1/assignments"))
}

func TestRateLimiter_IsSafeForConcurrentRequests(t *testing.T) {
	clk := newClock()
	handler, served := limited(newRateLimiter(1, 5, clk.Now))

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 5, served.Load(), "exactly the burst got through")
}

func TestNewRateLimiter_AllowsABurstOfFiveThenOneASecond(t *testing.T) {
	handler, served := limited(NewRateLimiter())

	for range 8 {
		status(handler, as("POST", "/probe/v1/hello", "p1", "i1"))
	}

	assert.EqualValues(t, 5, served.Load())
}
