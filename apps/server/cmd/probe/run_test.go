package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the probe does on its own
const wait = 10 * time.Second

// fakeRuntime stands in for the probe's runtime: what it does when it runs, is ready and is shut
// down is set by the test, and what it was asked is kept for the test to read
type fakeRuntime struct {
	ready atomic.Bool

	// runErr is what Run returns at once, instead of running until its context ends
	runErr error
	// runHangs makes Run ignore its context and return when this channel closes
	runHangs chan struct{}
	// onRun is called as Run starts
	onRun func()
	// shutdownErr is what Shutdown returns
	shutdownErr error
	// shutdownWaits makes Shutdown return only when its context ends
	shutdownWaits bool

	started  chan struct{} // closed as Run starts
	runEnded chan struct{} // closed as Run returns

	mu        sync.Mutex
	shutdowns []shutdownCall
}

// shutdownCall is what Shutdown was given
type shutdownCall struct {
	deadline    time.Time
	hasDeadline bool
	ctxErr      error // the context's error as Shutdown was called
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{started: make(chan struct{}), runEnded: make(chan struct{})}
}

func (f *fakeRuntime) Run(ctx context.Context) error {
	close(f.started)
	defer close(f.runEnded)
	if f.onRun != nil {
		f.onRun()
	}
	switch {
	case f.runErr != nil:
		return f.runErr
	case f.runHangs != nil:
		<-f.runHangs
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeRuntime) Ready() bool { return f.ready.Load() }

func (f *fakeRuntime) Shutdown(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	f.mu.Lock()
	f.shutdowns = append(f.shutdowns, shutdownCall{deadline: deadline, hasDeadline: hasDeadline, ctxErr: ctx.Err()})
	f.mu.Unlock()
	if f.shutdownWaits {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.shutdownErr
}

func (f *fakeRuntime) shutdownCalls() []shutdownCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]shutdownCall(nil), f.shutdowns...)
}

func (f *fakeRuntime) factory() newRuntimeFunc {
	return func(*Config, *zap.SugaredLogger) (runtime, error) { return f, nil }
}

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

// freeAddr is a local address nothing listens on
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func testConfig(addr string) *Config {
	return &Config{
		URL: "https://peekaping.example.com", Token: "secret-token", Concurrency: 4, ReadyAddr: addr, Mode: "test", LogLevel: "info",
	}
}

// instance is a probe running in the test
type instance struct {
	addr     string
	cancel   context.CancelFunc
	finished chan struct{} // closed as run returns
	err      error         // what run returned, once finished is closed
}

// startProbe runs the probe over rt and returns once its runtime has started. cancel is the signal.
func startProbe(t *testing.T, rt *fakeRuntime, logger *zap.SugaredLogger) *instance {
	t.Helper()
	return startProbeAt(t, freeAddr(t), rt, logger)
}

func startProbeAt(t *testing.T, addr string, rt *fakeRuntime, logger *zap.SugaredLogger) *instance {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{addr: addr, cancel: cancel, finished: make(chan struct{})}
	go func() {
		defer close(in.finished)
		in.err = run(ctx, testConfig(addr), logger, rt.factory())
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-in.finished:
		case <-time.After(wait):
		}
	})
	select {
	case <-rt.started:
	case <-in.finished:
		require.FailNow(t, "run returned before the runtime started", "%v", in.err)
	case <-time.After(wait):
		require.FailNow(t, "the runtime didn't start")
	}
	return in
}

// stop sends the signal and returns what run returned. It fails the test if run takes longer than wait.
func (in *instance) stop(t *testing.T) error {
	t.Helper()
	in.cancel()
	select {
	case <-in.finished:
		return in.err
	case <-time.After(wait):
		require.FailNow(t, "run didn't return after the signal")
		return nil
	}
}

// ready asks the ready endpoint, with the given method and path, and returns the status
func ready(t *testing.T, addr, method, path string) int {
	t.Helper()
	status, err := request(addr, method, path)
	require.NoError(t, err)
	return status
}

func request(addr, method, path string) (int, error) {
	req, err := http.NewRequest(method, "http://"+addr+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// waitForLog waits until message has been logged, and returns its entry
func waitForLog(t *testing.T, logs *observer.ObservedLogs, message string) observer.LoggedEntry {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if entries := logs.FilterMessage(message).All(); len(entries) > 0 {
			return entries[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNow(t, "not logged", message)
	return observer.LoggedEntry{}
}

func TestRun_SignalShutsTheRuntimeDownWithinTwentyFiveSecondsAndReturnsNil(t *testing.T) {
	rt := newFakeRuntime()
	logger, logs := observedLogger()
	in := startProbe(t, rt, logger)

	signalledAt := time.Now()
	err := in.stop(t)

	require.NoError(t, err, "run returns nil, so the probe exits 0")
	calls := rt.shutdownCalls()
	require.Len(t, calls, 1, "Shutdown is called once")
	require.True(t, calls[0].hasDeadline, "Shutdown is given a deadline")
	untilDeadline := calls[0].deadline.Sub(signalledAt)
	assert.Greater(t, untilDeadline, 24*time.Second, "the deadline is about 25 s from the signal")
	assert.Less(t, untilDeadline, 26*time.Second, "the deadline is about 25 s from the signal")
	assert.NoError(t, calls[0].ctxErr, "Shutdown gets a context of its own: the signal has ended the run's")
	select {
	case <-rt.runEnded:
	default:
		assert.Fail(t, "run returned before the runtime's Run did")
	}
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All(), "a clean shutdown logs no error")
}

func TestRun_StopsServingReadyOnceItHasShutDown(t *testing.T) {
	rt := newFakeRuntime()
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)
	require.Equal(t, http.StatusServiceUnavailable, ready(t, in.addr, http.MethodGet, "/ready"))

	require.NoError(t, in.stop(t))

	_, err := request(in.addr, http.MethodGet, "/ready")
	assert.Error(t, err, "nothing listens on the ready address any more")
}

func TestRun_ReadyAnswers503BeforeTheFirstAssignmentAndThen200ForGood(t *testing.T) {
	rt := newFakeRuntime()
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)

	assert.Equal(t, http.StatusServiceUnavailable, ready(t, in.addr, http.MethodGet, "/ready"), "no assignment yet")
	assert.Equal(t, http.StatusServiceUnavailable, ready(t, in.addr, http.MethodGet, "/ready"), "still none")

	rt.ready.Store(true)

	for i := 0; i < 5; i++ {
		assert.Equal(t, http.StatusOK, ready(t, in.addr, http.MethodGet, "/ready"), "the first assignment has arrived")
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRun_ReadyStaysAnsweringWhileTheRuntimeIsShuttingDown(t *testing.T) {
	rt := newFakeRuntime()
	rt.ready.Store(true)
	rt.shutdownWaits = true
	defer func(saved time.Duration) { shutdownTimeout = saved }(shutdownTimeout)
	shutdownTimeout = 300 * time.Millisecond
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)

	in.cancel()
	deadline := time.Now().Add(wait)
	for len(rt.shutdownCalls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, rt.shutdownCalls(), "the shutdown has begun")

	assert.Equal(t, http.StatusOK, ready(t, in.addr, http.MethodGet, "/ready"))
}

func TestRun_ReadyEndpointIsUpWhenTheRuntimeStartsRunning(t *testing.T) {
	rt := newFakeRuntime()
	addr := freeAddr(t)
	var statusAtRun atomic.Int64
	rt.onRun = func() {
		status, _ := request(addr, http.MethodGet, "/ready")
		statusAtRun.Store(int64(status))
	}
	logger, _ := observedLogger()
	in := startProbeAt(t, addr, rt, logger)

	require.NoError(t, in.stop(t))

	assert.EqualValues(t, http.StatusServiceUnavailable, statusAtRun.Load(), "listening before Run starts, and not ready yet")
}

func TestRun_ServesOnlyGetReady(t *testing.T) {
	rt := newFakeRuntime()
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)

	assert.Equal(t, http.StatusMethodNotAllowed, ready(t, in.addr, http.MethodPost, "/ready"))
	assert.Equal(t, http.StatusNotFound, ready(t, in.addr, http.MethodGet, "/"))
	assert.Equal(t, http.StatusNotFound, ready(t, in.addr, http.MethodGet, "/metrics"))
}

func TestRun_ARunErrorIsLoggedAndTheProbeKeepsRunning(t *testing.T) {
	rt := newFakeRuntime()
	rt.runErr = errors.New("the scheduler failed")
	rt.ready.Store(true)
	logger, logs := observedLogger()
	in := startProbe(t, rt, logger)

	entry := waitForLog(t, logs, "The probe's run ended with an error, and it keeps serving until it's told to stop")
	assert.Equal(t, zap.ErrorLevel, entry.Level)
	assert.Equal(t, "the scheduler failed", entry.ContextMap()["error"])
	select {
	case <-in.finished:
		require.FailNow(t, "run returned after the runtime's Run failed", "%v", in.err)
	case <-time.After(200 * time.Millisecond):
	}
	assert.Equal(t, http.StatusOK, ready(t, in.addr, http.MethodGet, "/ready"), "the ready endpoint still answers")

	require.NoError(t, in.stop(t))
	assert.Len(t, rt.shutdownCalls(), 1, "the signal still shuts the runtime down")
}

func TestRun_AShutdownErrorMakesRunFail(t *testing.T) {
	rt := newFakeRuntime()
	rt.shutdownErr = errors.New("the buffer couldn't be flushed")
	logger, logs := observedLogger()
	in := startProbe(t, rt, logger)

	err := in.stop(t)

	require.Error(t, err)
	assert.ErrorContains(t, err, "the buffer couldn't be flushed")
	assert.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
}

func TestRun_AShutdownThatOutlastsItsDeadlineFailsAtTheDeadline(t *testing.T) {
	defer func(saved time.Duration) { shutdownTimeout = saved }(shutdownTimeout)
	shutdownTimeout = 200 * time.Millisecond
	rt := newFakeRuntime()
	rt.shutdownWaits = true
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)

	stoppedAt := time.Now()
	err := in.stop(t)
	took := time.Since(stoppedAt)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.GreaterOrEqual(t, took, 200*time.Millisecond, "the shutdown got its whole deadline")
	assert.Less(t, took, 2*time.Second, "and no more")
}

func TestRun_ARunThatDoesNotStopFailsAtTheDeadline(t *testing.T) {
	defer func(saved time.Duration) { shutdownTimeout = saved }(shutdownTimeout)
	shutdownTimeout = 200 * time.Millisecond
	rt := newFakeRuntime()
	rt.runHangs = make(chan struct{})
	defer close(rt.runHangs)
	logger, _ := observedLogger()
	in := startProbe(t, rt, logger)

	stoppedAt := time.Now()
	err := in.stop(t)
	took := time.Since(stoppedAt)

	require.Error(t, err)
	assert.ErrorContains(t, err, "didn't stop in time")
	assert.Len(t, rt.shutdownCalls(), 1, "the runtime was shut down all the same")
	assert.Less(t, took, 2*time.Second)
}

func TestRun_FailsAtOnceWhenTheReadyAddressIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer taken.Close()
	logger, _ := observedLogger()
	built := false

	err = run(context.Background(), testConfig(taken.Addr().String()), logger, func(*Config, *zap.SugaredLogger) (runtime, error) {
		built = true
		return newFakeRuntime(), nil
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "PROBE_READY_ADDR")
	assert.ErrorContains(t, err, taken.Addr().String())
	assert.False(t, built, "no runtime was built")
}

func TestRun_FailsWhenTheRuntimeCannotBeBuiltAndFreesTheReadyAddress(t *testing.T) {
	addr := freeAddr(t)
	logger, _ := observedLogger()

	err := run(context.Background(), testConfig(addr), logger, func(*Config, *zap.SugaredLogger) (runtime, error) {
		return nil, errors.New("no way to reach the server")
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "no way to reach the server")
	l, listenErr := net.Listen("tcp", addr)
	require.NoError(t, listenErr, "the ready address is free again")
	require.NoError(t, l.Close())
}

func TestRun_LogsTheVersionItStartsWith(t *testing.T) {
	rt := newFakeRuntime()
	logger, logs := observedLogger()
	in := startProbe(t, rt, logger)
	require.NoError(t, in.stop(t))

	entries := logs.FilterMessage("Starting the probe").All()

	require.Len(t, entries, 1)
	assert.Equal(t, version.Version, entries[0].ContextMap()["version"])
	assert.Equal(t, "https://peekaping.example.com", entries[0].ContextMap()["server"])
}

func TestRun_NeverLogsTheTokenOrAPasswordInTheURL(t *testing.T) {
	rt := newFakeRuntime()
	logger, logs := observedLogger()
	cfg := testConfig(freeAddr(t))
	cfg.URL = "https://admin:hunter2@peekaping.example.com"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, logger, rt.factory()) }()
	<-rt.started
	cancel()
	require.NoError(t, <-done)

	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, "hunter2")
		assert.NotContains(t, entry.Message, "secret-token")
		for _, value := range entry.ContextMap() {
			assert.NotContains(t, fmt.Sprint(value), "hunter2")
			assert.NotContains(t, fmt.Sprint(value), "secret-token")
		}
	}
}
