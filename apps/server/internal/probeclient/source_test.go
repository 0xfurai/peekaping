package probeclient_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The source is what the scheduler reads its monitors from
var _ scheduler.Source = (*probeclient.Source)(nil)

// newSource creates a source of the server's assignment. Its waits are the sleeper's.
func newSource(server *assignmentServer, pause *sleeper) *probeclient.Source {
	session := newSession(server.URL, func(c *probeclient.SessionConfig) { c.Sleep = pause.Sleep })
	return probeclient.NewSource(session)
}

// run runs the source's poll until the test ends
func run(t *testing.T, source *probeclient.Source) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		source.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// startSource runs a new source against the server until the test ends
func startSource(t *testing.T, server *assignmentServer, pause *sleeper) *probeclient.Source {
	t.Helper()
	source := newSource(server, pause)
	run(t, source)
	return source
}

// signals collects what a source tells the scheduler that watches it
func signals(source *probeclient.Source) <-chan string {
	got := make(chan string, 100)
	source.Watch(
		func(id string) { got <- "created " + id },
		func(id string) { got <- "updated " + id },
		func(id string) { got <- "deleted " + id },
	)
	return got
}

// take reads n signals
func take(t *testing.T, from <-chan string, n int) []string {
	t.Helper()
	var out []string
	for range n {
		select {
		case signal := <-from:
			out = append(out, signal)
		case <-time.After(5 * time.Second):
			require.Failf(t, "signal missing", "got %v, wanted %d signals", out, n)
		}
	}
	return out
}

// assignedTo waits until the source holds the monitors
func assignedTo(t *testing.T, source *probeclient.Source, ids ...string) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, id := range ids {
			if mon, _ := source.Find(context.Background(), id); mon == nil {
				return false
			}
		}
		return true
	}, 5*time.Second, time.Millisecond, "monitors %v are assigned", ids)
}

func TestSource_DiffSignals(t *testing.T) {
	later := monitorTime.Add(time.Minute)
	movedOnly := func(m *probeproto.Monitor) { m.UpdatedAt = later }
	server := newAssignmentServer(t,
		snapshot("v1",
			httpMonitor("a"),
			httpMonitor("b"),
			httpMonitor("c", behindProxy("egress-1.example.com")),
			httpMonitor("d"),
			httpMonitor("e"),
		),
		snapshot("v2",
			httpMonitor("a", movedOnly),
			httpMonitor("b", func(m *probeproto.Monitor) { m.Interval = 30 }),
			httpMonitor("c", behindProxy("egress-2.example.com")),
			// d is gone
			httpMonitor("e", func(m *probeproto.Monitor) { m.Config = `{"url":"https://example.org"}` }),
			httpMonitor("f"),
		),
		snapshot("v3",
			httpMonitor("a", movedOnly),
			httpMonitor("b", func(m *probeproto.Monitor) { m.Interval = 30; m.UpdatedAt = later }),
			httpMonitor("c", behindProxy("egress-2.example.com"), func(m *probeproto.Monitor) { m.Proxy.UpdatedAt = later }),
			httpMonitor("e", func(m *probeproto.Monitor) { m.Config = `{"url":"https://example.org"}` }),
			httpMonitor("f", movedOnly),
		),
	)
	source := newSource(server, &sleeper{})
	got := signals(source)
	run(t, source)

	assert.ElementsMatch(t,
		[]string{"created a", "created b", "created c", "created d", "created e"},
		take(t, got, 5), "a monitor the probe has not seen is new")

	assert.ElementsMatch(t,
		[]string{"updated b", "updated c", "updated e", "deleted d", "created f"},
		take(t, got, 5), "a monitor whose interval, proxy or config changed is changed, a monitor that is not listed any more is gone, and a is neither: only its updated_at moved")

	require.Eventually(t, func() bool {
		mon, _ := source.Find(context.Background(), "f")
		return mon != nil && mon.UpdatedAt.Equal(later)
	}, 5*time.Second, time.Millisecond, "the third assignment is taken")
	assert.Empty(t, got, "a monitor whose updated_at alone moved, or its proxy's, is not a change")
}

func TestSource_PollsAgainOn304(t *testing.T) {
	server := newAssignmentServer(t, snapshot("v1", httpMonitor("a")), unchanged(), unchanged(), snapshot("v2", httpMonitor("a"), httpMonitor("b")))
	pause := &sleeper{}
	source := startSource(t, server, pause)

	assignedTo(t, source, "a", "b")

	server.expectSequence(t, []string{"hello", "poll", "poll", "poll", "poll", "poll"}, "one hello, and a 304 is a poll again; the last poll is the one held")
	assert.Equal(t, []string{"", "v1", "v1", "v1", "v2"}, server.polledVersions(), "each poll names the version the probe holds")
	assert.Empty(t, pause.waits(), "and the probe doesn't wait")
}

func TestSource_HelloAgainOn409(t *testing.T) {
	server := newAssignmentServer(t,
		failing(http.StatusConflict, "unknown instance: send hello first", 0),
		snapshot("v1", httpMonitor("a")),
	)
	pause := &sleeper{}
	source := startSource(t, server, pause)

	assignedTo(t, source, "a")

	server.expectSequence(t, []string{"hello", "poll", "hello", "poll", "poll"}, "the last poll is the one held")
	assert.Empty(t, pause.waits(), "the second hello is said at once")
}

// The first 409 is a server that forgot the instance, which hello puts right at once. A 409 again
// after that hello is a server that keeps forgetting it, which must not be said hello to in a tight loop.
func TestSource_BacksOffOnA409AfterHello(t *testing.T) {
	conflict := failing(http.StatusConflict, "unknown instance: send hello first", 0)
	server := newAssignmentServer(t,
		conflict, conflict, conflict, conflict, conflict,
		snapshot("v1", httpMonitor("a")),
		conflict,
		snapshot("v2", httpMonitor("a"), httpMonitor("b")),
	)
	pause := &sleeper{}
	source := startSource(t, server, pause)

	assignedTo(t, source, "a", "b")

	waits := pause.waits()
	require.Len(t, waits, 4, "the first 409 is followed by no wait, each one after it by a wait, and the answer to poll 6 puts the next 409 back to the first")
	for i, step := range []time.Duration{1, 2, 4, 8} {
		assert.GreaterOrEqual(t, waits[i], step*time.Second/2, "wait %d", i+1)
		assert.LessOrEqual(t, waits[i], step*time.Second, "wait %d: the backoff starts at its minimum", i+1)
	}
	server.expectSequence(t, []string{
		"hello", "poll", // 409: hello again at once
		"hello", "poll", // 409: wait 1
		"hello", "poll", // 409: wait 2
		"hello", "poll", // 409: wait 3
		"hello", "poll", // 409: wait 4
		"hello", "poll", // v1
		"poll",          // 409: hello again at once
		"hello", "poll", // v2
		"poll", // the one held
	}, "every 409 is followed by a hello")
}

func TestSource_HonoursRetryAfter(t *testing.T) {
	server := newAssignmentServer(t,
		failing(http.StatusTooManyRequests, "too many requests", 7*time.Second),
		failing(http.StatusServiceUnavailable, "starting up", 3*time.Second),
		snapshot("v1", httpMonitor("a")),
	)
	pause := &sleeper{}
	source := startSource(t, server, pause)

	assignedTo(t, source, "a")

	assert.Equal(t, []time.Duration{7 * time.Second, 3 * time.Second}, pause.waits())
	server.expectSequence(t, []string{"hello", "poll", "poll", "hello", "poll", "poll"},
		"a 429 only waits, and after a 503 the probe says hello before it polls again")
}

func TestSource_BacksOffFromOneSecondAndSaysHelloAfterEachReconnect(t *testing.T) {
	server := newAssignmentServer(t,
		failing(http.StatusInternalServerError, "boom", 0),
		dropped(),
		failing(http.StatusBadGateway, "boom", 0),
		snapshot("v1", httpMonitor("a")),
		failing(http.StatusInternalServerError, "boom", 0),
		snapshot("v2", httpMonitor("a"), httpMonitor("b")),
	)
	pause := &sleeper{}
	source := startSource(t, server, pause)

	assignedTo(t, source, "a", "b")

	waits := pause.waits()
	require.Len(t, waits, 4)
	for i, step := range []time.Duration{1, 2, 4, 1} {
		assert.GreaterOrEqual(t, waits[i], step*time.Second/2, "wait %d", i+1)
		assert.LessOrEqual(t, waits[i], step*time.Second, "wait %d: the answer to poll 4 reset the backoff", i+1)
	}
	server.expectSequence(t, []string{
		"hello", "poll", // 500
		"hello", "poll", // a dropped connection
		"hello", "poll", // 502
		"hello", "poll", // 200
		"poll",          // 500
		"hello", "poll", // 200
		"poll", // held
	}, "a lost server costs a hello before the next poll")
}

func TestSource_ServesTheAssignmentItHolds(t *testing.T) {
	server := newAssignmentServer(t, snapshot("v1",
		httpMonitor("c", func(m *probeproto.Monitor) { m.Config = `{"url":"https://c.example.com","check_cert_expiry":true}` }),
		httpMonitor("a", behindProxy("egress.example.com")),
		httpMonitor("b"),
		httpMonitor("d"),
		httpMonitor("e"),
	))
	source := startSource(t, server, &sleeper{})
	ctx := context.Background()

	assignedTo(t, source, "a", "b", "c", "d", "e")

	t.Run("ListActive pages the monitors by id", func(t *testing.T) {
		ids := func(page, limit int) []string {
			monitors, err := source.ListActive(ctx, page, limit)
			require.NoError(t, err)
			var out []string
			for _, m := range monitors {
				out = append(out, m.ID)
			}
			return out
		}

		assert.Equal(t, []string{"a", "b"}, ids(0, 2))
		assert.Equal(t, []string{"c", "d"}, ids(1, 2))
		assert.Equal(t, []string{"e"}, ids(2, 2), "the last page is short")
		assert.Empty(t, ids(3, 2))
	})

	t.Run("Find returns the monitor as the scheduler checks it", func(t *testing.T) {
		mon, err := source.Find(ctx, "a")

		require.NoError(t, err)
		assert.Equal(t, &shared.Monitor{
			ID: "a", Type: "http", Name: "Monitor a", Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 60, ResendInterval: 10,
			Active: true, Config: `{"url":"https://example.com"}`, ProxyId: "px1", UpdatedAt: monitorTime,
		}, mon)
		assert.True(t, mon.RunsOn(shared.LocalProbeID), "it is this process's to check")
	})

	t.Run("Find returns nil for a monitor that is not assigned", func(t *testing.T) {
		mon, err := source.Find(ctx, "zzz")

		require.NoError(t, err)
		assert.Nil(t, mon)
	})

	t.Run("Load returns the monitor with its proxy", func(t *testing.T) {
		job, err := source.Load(ctx, "a")

		require.NoError(t, err)
		assert.Equal(t, &check.Job{
			Monitor: &shared.Monitor{
				ID: "a", Type: "http", Name: "Monitor a", Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 60, ResendInterval: 10,
				Active: true, Config: `{"url":"https://example.com"}`, ProxyId: "px1", UpdatedAt: monitorTime,
			},
			Proxy: &shared.Proxy{
				ID: "px1", Protocol: "socks5", Host: "egress.example.com", Port: 1080, Auth: true, Username: "scout", Password: "s3cret", UpdatedAt: monitorTime,
			},
		}, job, "maintenance is the server's to judge, so it is never set here")
	})

	t.Run("Load reads the monitor's config for the certificate expiry check", func(t *testing.T) {
		job, err := source.Load(ctx, "c")

		require.NoError(t, err)
		assert.True(t, job.CheckCertExpiry)
		assert.Nil(t, job.Proxy)
	})

	t.Run("Load returns ErrMonitorNotFound for a monitor that is not assigned", func(t *testing.T) {
		job, err := source.Load(ctx, "zzz")

		assert.ErrorIs(t, err, check.ErrMonitorNotFound)
		assert.Nil(t, job)
	})

	t.Run("each read is a copy, so a check that changes its monitor changes no one else's", func(t *testing.T) {
		first, err := source.Load(ctx, "b")
		require.NoError(t, err)
		first.Monitor.Name = "changed by a check"

		second, err := source.Load(ctx, "b")

		require.NoError(t, err)
		assert.Equal(t, "Monitor b", second.Monitor.Name)
	})
}

func TestSource_ListsNothingBeforeTheFirstAssignment(t *testing.T) {
	server := newAssignmentServer(t) // every poll is held
	source := startSource(t, server, &sleeper{})

	monitors, err := source.ListActive(context.Background(), 0, 100)

	require.NoError(t, err)
	assert.Empty(t, monitors)
	assert.False(t, source.Ready(), "the probe has no assignment yet")
}

func TestSource_KeepsTheLastAssignmentWhileTheServerIsUnreachable(t *testing.T) {
	server := newAssignmentServer(t,
		snapshot("v1", httpMonitor("a"), httpMonitor("b")),
		failing(http.StatusServiceUnavailable, "down for maintenance", 0),
	)
	pause := holdingSleeper() // parked in its first backoff
	source := newSource(server, pause)
	got := signals(source)
	run(t, source)
	assignedTo(t, source, "a", "b")
	require.Eventually(t, func() bool { return len(pause.waits()) > 0 }, 5*time.Second, time.Millisecond, "the server is lost")

	server.Close()

	monitors, err := source.ListActive(context.Background(), 0, 100)
	require.NoError(t, err)
	assert.Len(t, monitors, 2, "the probe keeps checking what it was last assigned")
	job, err := source.Load(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "a", job.Monitor.ID)
	assert.True(t, source.Ready(), "and it stays ready")
	assert.Equal(t, []string{"created a", "created b"}, take(t, got, 2), "nothing is signalled gone")
}
