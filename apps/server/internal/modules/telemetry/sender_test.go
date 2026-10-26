package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/version"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// clock is a clock the test moves by hand
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)} }

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

// worker is a stand-in for the Worker: it records the pings it receives.
type worker struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
}

func newWorker(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *worker {
	t.Helper()
	wk := &worker{}
	wk.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wk.mu.Lock()
		wk.requests = append(wk.requests, r)
		wk.bodies = append(wk.bodies, body)
		wk.mu.Unlock()
		if respond != nil {
			respond(w, r)
		}
	}))
	t.Cleanup(wk.Close)
	return wk
}

func (wk *worker) received() int {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return len(wk.requests)
}

func (wk *worker) body(n int) []byte {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return wk.bodies[n]
}

func (wk *worker) request(n int) *http.Request {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return wk.requests[n]
}

// claimerFunc is a claimer the test controls
type claimerFunc func(context.Context) (bool, error)

func (f claimerFunc) Claim(ctx context.Context) (bool, error) { return f(ctx) }

func claimsAlways() Claimer { return AlwaysClaimer{} }

type senderTest struct {
	in     *install
	svc    Service
	clock  *clock
	logs   *observer.ObservedLogs
	sender *Sender
}

func newSenderTest(t *testing.T, endpoint string, claimer Claimer, opts ...SenderOption) *senderTest {
	t.Helper()
	in := newInstall()
	in.optIn(testInstanceID)
	core, logs := observer.New(zapcore.DebugLevel)
	st := &senderTest{in: in, svc: in.service(), clock: newClock(), logs: logs}
	opts = append([]SenderOption{WithClock(st.clock.Now)}, opts...)
	st.sender = NewSender(st.svc, claimer, endpoint, zap.New(core).Sugar(), opts...)
	return st
}

func (st *senderTest) lastSent(t *testing.T) string {
	t.Helper()
	stored, err := st.in.settings.GetByKey(context.Background(), LastSentKey)
	require.NoError(t, err)
	if stored == nil {
		return ""
	}
	return stored.Value
}

func TestSender_OncePerDay(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimsAlways())
	ctx := context.Background()

	st.sender.SendIfDue(ctx)

	require.Equal(t, 1, wk.received())
	request := wk.request(0)
	assert.Equal(t, http.MethodPost, request.Method)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Equal(t, "Peekaping/"+version.Version, request.Header.Get("User-Agent"))
	var sent Payload
	require.NoError(t, json.Unmarshal(wk.body(0), &sent))
	preview, err := st.svc.BuildPayload(ctx)
	require.NoError(t, err)
	assert.Equal(t, preview, sent, "the ping is the payload the preview shows")
	assert.Equal(t, testInstanceID, sent.InstanceID)
	assert.Equal(t, "2026-10-21T08:00:00Z", st.lastSent(t))

	st.sender.SendIfDue(ctx)
	st.clock.Advance(23*time.Hour + 59*time.Minute)
	st.sender.SendIfDue(ctx)
	assert.Equal(t, 1, wk.received(), "a day has not passed")

	st.clock.Advance(2 * time.Minute)
	st.sender.SendIfDue(ctx)
	assert.Equal(t, 2, wk.received(), "a day and a minute have")
	assert.Equal(t, "2026-10-22T08:01:00Z", st.lastSent(t))
}

func TestSender_CheckingEveryTickSendsOnceADay(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimsAlways())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := st.sender.Start(ctx, Schedule{FirstCheck: time.Millisecond, Every: time.Millisecond})

	require.Eventually(t, func() bool { return wk.received() == 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, wk.received(), "many checks in a day send one ping")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sender did not stop")
	}
}

func TestSender_WaitsForTheFirstCheck(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimsAlways())
	ctx, cancel := context.WithCancel(context.Background())

	done := st.sender.Start(ctx, Schedule{FirstCheck: time.Hour, Every: time.Hour})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 0, wk.received(), "nothing is sent at startup")
}

func TestSender_SendsNothingAfterAnOptOutOrWhileForcedOff(t *testing.T) {
	for name, setup := range map[string]func(*senderTest){
		"opted out": func(st *senderTest) {
			st.in.settings.put(EnabledKey, "false", "bool")
		},
		"TELEMETRY=off": func(st *senderTest) { st.in.cfg.Telemetry = "off" },
		"no choice stored, TELEMETRY=off": func(st *senderTest) {
			st.in.settings = newFakeSettings()
			st.in.cfg.Telemetry = "off"
		},
	} {
		t.Run(name, func(t *testing.T) {
			wk := newWorker(t, nil)
			st := newSenderTest(t, wk.URL, claimsAlways())
			setup(st)
			st.sender = NewSender(st.in.service(), claimsAlways(), wk.URL, zap.NewNop().Sugar(), WithClock(st.clock.Now))

			st.sender.SendIfDue(context.Background())

			assert.Equal(t, 0, wk.received())
		})
	}
}

// The ping is on by default: an install that never chose sends, with an ID the sender made
// itself, and an install whose ID went missing makes a new one.
func TestSender_SendsByDefaultWithAnIDOfItsOwn(t *testing.T) {
	for name, setup := range map[string]func(*senderTest){
		"no choice and no ID stored": func(st *senderTest) { st.in.settings = newFakeSettings() },
		"enabled but the ID is missing": func(st *senderTest) {
			_ = st.in.settings.DeleteByKey(context.Background(), InstanceIDKey)
		},
	} {
		t.Run(name, func(t *testing.T) {
			wk := newWorker(t, nil)
			st := newSenderTest(t, wk.URL, claimsAlways())
			setup(st)
			st.sender = NewSender(st.in.service(), claimsAlways(), wk.URL, zap.NewNop().Sugar(), WithClock(st.clock.Now))

			st.sender.SendIfDue(context.Background())

			require.Equal(t, 1, wk.received())
			var sent Payload
			require.NoError(t, json.Unmarshal(wk.body(0), &sent))
			assert.Regexp(t, lowerUUIDv4, sent.InstanceID)
			stored, err := st.in.settings.GetByKey(context.Background(), InstanceIDKey)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, sent.InstanceID, stored.Value, "the next ping carries the same ID")
			assert.Equal(t, "2026-10-21T08:00:00Z", st.lastSent(t))
		})
	}
}

func TestSender_OptingOutAndInSendsAtOnceWithTheNewID(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimsAlways())
	ctx := context.Background()
	st.sender.SendIfDue(ctx)
	require.Equal(t, 1, wk.received())

	_, err := st.svc.SetEnabled(ctx, false)
	require.NoError(t, err)
	st.sender.SendIfDue(ctx)
	assert.Equal(t, 1, wk.received(), "opted out")

	_, err = st.svc.SetEnabled(ctx, true)
	require.NoError(t, err)
	st.sender.SendIfDue(ctx)
	require.Equal(t, 2, wk.received(), "the new opt-in has no last ping")
	var first, second Payload
	require.NoError(t, json.Unmarshal(wk.body(0), &first))
	require.NoError(t, json.Unmarshal(wk.body(1), &second))
	assert.NotEqual(t, first.InstanceID, second.InstanceID)
}

// An admin who opts out while a ping is in flight gets no last-sent time back from it: the next
// opt-in would find a ping "sent" a moment ago and wait up to a day for its first one. The same
// goes for an admin who opts out and in again, and so has a new instance ID, since the ping that
// was in flight carried the old one.
func TestSender_ARecordedPingNeverOutlivesTheOptInItWasSentUnder(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, svc Service){
		"opted out": func(t *testing.T, svc Service) {
			_, err := svc.SetEnabled(context.Background(), false)
			require.NoError(t, err)
		},
		"opted out and in again": func(t *testing.T, svc Service) {
			_, err := svc.SetEnabled(context.Background(), false)
			require.NoError(t, err)
			_, err = svc.SetEnabled(context.Background(), true)
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			inFlight := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			wk := newWorker(t, func(http.ResponseWriter, *http.Request) {
				once.Do(func() { close(inFlight) })
				<-release
			})
			st := newSenderTest(t, wk.URL, claimsAlways())
			sending := make(chan struct{})
			go func() {
				defer close(sending)
				st.sender.SendIfDue(context.Background())
			}()
			<-inFlight

			change(t, st.svc)
			close(release)
			<-sending

			assert.Equal(t, "", st.lastSent(t), "the ping that was in flight is not recorded")
			if name == "opted out" {
				_, err := st.svc.SetEnabled(context.Background(), true)
				require.NoError(t, err)
			}
			st.sender.SendIfDue(context.Background())
			assert.Equal(t, 2, wk.received(), "the new opt-in sends its first ping at the next check")
		})
	}
}

func TestSender_OnlyTheReplicaThatClaimsSends(t *testing.T) {
	wk := newWorker(t, nil)
	claim := false
	st := newSenderTest(t, wk.URL, claimerFunc(func(context.Context) (bool, error) { return claim, nil }))
	ctx := context.Background()

	st.sender.SendIfDue(ctx)
	assert.Equal(t, 0, wk.received(), "another replica holds the claim")
	assert.Equal(t, "", st.lastSent(t), "the ping is still due")

	claim = true
	st.sender.SendIfDue(ctx)
	assert.Equal(t, 1, wk.received())
}

// Two api replicas read the same settings and are both told the ping is due while the first one's
// is still on its way. Only the one that wins the claim in Redis sends.
func TestSender_TwoClusterReplicasSendOneParallelPing(t *testing.T) {
	inFlight := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	wk := newWorker(t, func(http.ResponseWriter, *http.Request) {
		once.Do(func() { close(inFlight) })
		<-release
	})
	redisServer := miniredis.RunT(t)
	first := newSenderTest(t, wk.URL, replica(t, redisServer))
	second := NewSender(first.svc, replica(t, redisServer), wk.URL, zap.NewNop().Sugar(), WithClock(first.clock.Now))
	sending := make(chan struct{})
	go func() {
		defer close(sending)
		first.sender.SendIfDue(context.Background())
	}()
	<-inFlight

	second.SendIfDue(context.Background())
	close(release)
	<-sending

	assert.Equal(t, 1, wk.received(), "one replica sends the day's ping")
}

func TestSender_ClaimErrorsDropTheCheck(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimerFunc(func(context.Context) (bool, error) { return false, errors.New("redis is down") }))

	st.sender.SendIfDue(context.Background())

	assert.Equal(t, 0, wk.received())
	assert.Empty(t, st.logs.FilterLevelExact(zapcore.WarnLevel).All())
	assert.Empty(t, st.logs.FilterLevelExact(zapcore.ErrorLevel).All())
}

func TestSender_DropsFailures(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	for name, tc := range map[string]struct {
		endpoint func(t *testing.T) string
		timeout  time.Duration
	}{
		"the endpoint is down": {endpoint: func(*testing.T) string { return downURL }},
		"the endpoint answers 500": {endpoint: func(t *testing.T) string {
			return newWorker(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }).URL
		}},
		"the endpoint answers 400": {endpoint: func(t *testing.T) string {
			return newWorker(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid db"}`))
			}).URL
		}},
		"the endpoint is slower than the timeout": {timeout: 50 * time.Millisecond, endpoint: func(t *testing.T) string {
			return newWorker(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}).URL
		}},
		"the endpoint is not a URL": {endpoint: func(*testing.T) string { return "://not a url" }},
	} {
		t.Run(name, func(t *testing.T) {
			var opts []SenderOption
			if tc.timeout > 0 {
				opts = append(opts, WithTimeout(tc.timeout))
			}
			st := newSenderTest(t, tc.endpoint(t), claimsAlways(), opts...)
			started := time.Now()

			st.sender.SendIfDue(context.Background())

			assert.Less(t, time.Since(started), 3*time.Second, "a slow ping is cut off")
			assert.Equal(t, "2026-10-21T08:00:00Z", st.lastSent(t), "the attempt counts, whether or not it worked")
			assert.NotEmpty(t, st.logs.FilterLevelExact(zapcore.DebugLevel).All(), "the failure is logged at debug")
			assert.Empty(t, st.logs.FilterLevelExact(zapcore.InfoLevel).All())
			assert.Empty(t, st.logs.FilterLevelExact(zapcore.WarnLevel).All())
			assert.Empty(t, st.logs.FilterLevelExact(zapcore.ErrorLevel).All())

			// and dropped: it is not tried again until a day has passed
			st.clock.Advance(time.Hour)
			before := st.logs.Len()
			st.sender.SendIfDue(context.Background())
			assert.Equal(t, before, st.logs.Len())
		})
	}
}

func TestSender_DoesNotFollowRedirects(t *testing.T) {
	elsewhere := newWorker(t, nil)
	redirecting := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	})
	st := newSenderTest(t, redirecting.URL, claimsAlways())

	st.sender.SendIfDue(context.Background())

	assert.Equal(t, 1, redirecting.received())
	assert.Equal(t, 0, elsewhere.received(), "the ping goes only where TELEMETRY_URL says")
}

// panickingSource is a source whose first n reads panic
type panickingSource struct {
	Service
	panics int
	calls  atomic.Int32
}

func (p *panickingSource) NextPing(ctx context.Context, now time.Time) (*Payload, error) {
	if int(p.calls.Add(1)) <= p.panics {
		panic("nil pointer dereference")
	}
	return p.Service.NextPing(ctx, now)
}

func TestSender_SurvivesAPanic(t *testing.T) {
	wk := newWorker(t, nil)
	st := newSenderTest(t, wk.URL, claimsAlways())
	source := &panickingSource{Service: st.svc, panics: 1}
	core, logs := observer.New(zapcore.DebugLevel)
	st.logs = logs
	st.sender = NewSender(source, claimsAlways(), wk.URL, zap.New(core).Sugar(), WithClock(st.clock.Now))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := st.sender.Start(ctx, Schedule{FirstCheck: time.Millisecond, Every: time.Millisecond})

	require.Eventually(t, func() bool { return wk.received() == 1 }, 5*time.Second, time.Millisecond, "the checks go on after a panic")
	assert.Equal(t, 1, st.logs.FilterLevelExact(zapcore.ErrorLevel).Len(), "the panic is logged")
	cancel()
	<-done
}
