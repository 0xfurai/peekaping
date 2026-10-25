//go:build soak

package apitest

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/dig"
	"golang.org/x/time/rate"
)

// TestIngestRate_TheAPIStoresWhatTheProbesSend measures how fast the cluster API stores what the
// probes send. Three probe instances post batches of results of 500 monitors, 20 rounds of them,
// 10,000 results in all, as fast as the API answers. The rate is the results over the time from the
// first post to the last heartbeat stored.
//
// It runs on SQLite, and on PostgreSQL too when PEE_TEST_POSTGRES_DSN names a server. A Redis of
// its own, PEE_TEST_REDIS_ADDR (host:port), keeps the duplicate window off the in-process
// miniredis, which is not what it runs against in production. From apps/server:
//
//	go test -tags soak -run TestIngestRate -count=1 -timeout 15m -v ./internal/apitest/
func TestIngestRate_TheAPIStoresWhatTheProbesSend(t *testing.T) {
	backends := map[string]func(*config.Config){
		"sqlite": func(*config.Config) {},
	}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends["postgres"] = usePostgres(t, dsn)
	}
	for name, backend := range backends {
		t.Run(name, func(t *testing.T) {
			api := measureIngest(t, backend)
			t.Logf("RESULT %s: the API stores %.0f results/s (%d results in %s)",
				name, api.rate(), api.results, api.stored.Round(time.Millisecond))
			assert.NotZero(t, api.rate())
		})
	}
}

const (
	rateMonitors = 500
	rateRounds   = 20
	rateSenders  = 3
)

// ingestRun is what one run of measureIngest saw
type ingestRun struct {
	results int
	// stored is the time until the last heartbeat was stored
	stored time.Duration
}

func (r ingestRun) rate() float64 { return float64(r.results) / r.stored.Seconds() }

// usePostgres points the app at a database of its own on the PostgreSQL server the DSN names
func usePostgres(t *testing.T, dsn string) func(*config.Config) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	return func(cfg *config.Config) {
		cfg.DBType, cfg.DBHost, cfg.DBPort = "postgres", parsed.Hostname(), parsed.Port()
		cfg.DBUser, cfg.DBPass = parsed.User.Username(), password
		cfg.DBName = "ingest_rate_" + uuid.NewString()[:8]
		admin := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
		defer admin.Close()
		// A Docker Desktop port forwarding resets the first connection now and then
		var err error
		for range 5 {
			if _, err = admin.Exec("CREATE DATABASE " + cfg.DBName); err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.NoError(t, err)
	}
}

// measureIngest posts the results through the API, which stores them itself, and waits for all of
// them to be stored
func measureIngest(t *testing.T, backend func(*config.Config)) ingestRun {
	t.Helper()
	options := []func(*config.Config){func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken }, backend}
	if addr := os.Getenv("PEE_TEST_REDIS_ADDR"); addr != "" {
		host, port, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		options = append(options, func(cfg *config.Config) { cfg.RedisHost, cfg.RedisPort = host, port })
	}
	decorate := func(container *dig.Container) error {
		// The measurement posts faster than a probe does: one batch a second is a probe's limit
		return container.Decorate(func() *protocol.RateLimiter { return protocol.NewRateLimiterOf(rate.Inf, 1<<20) })
	}
	a := newAppDecorated(t, decorate, options...)
	a.registerAdmin(t)
	// The stats count the heartbeats the API stored, as the API does
	a.countStatsOnTheLocalBus(t)
	// database/sql keeps two idle connections, and closes and opens the rest again and again; through
	// the port forwarding of a Docker Desktop PostgreSQL that churn resets connections. A pool that
	// keeps what it opens measures the ingest and not the forwarding.
	require.NoError(t, a.container.Invoke(func(db *bun.DB) {
		if db.Dialect().Name() != dialect.PG {
			return // SQLite is opened with one connection, which serializes its access
		}
		db.SetMaxIdleConns(60)
		db.SetMaxOpenConns(60)
	}))

	monitors := make([]string, rateMonitors)
	for i := range monitors {
		monitors[i] = a.createHTTPMonitor(t, fmt.Sprintf("Rate %03d", i), "")
	}
	instances := make([]string, rateSenders)
	for i := range instances {
		instances[i] = uuid.NewString()
		a.sayHello(t, localToken, instances[i])
	}

	// Each sender posts the results of its share of the monitors, one round at a time
	// The ids are this run's own: the duplicate window of a Redis that outlives the run still holds
	// the ids of the last one for an hour
	runID := uuid.NewString()[:8]
	base := time.Now().UTC().Add(-30 * time.Minute)
	var wg sync.WaitGroup
	type posted struct {
		status  int
		reasons map[string]int
	}
	answers := make(chan posted, rateSenders*rateRounds)
	started := time.Now()
	for s := range rateSenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range rateRounds {
				var batch []probeproto.Result
				for m := s; m < rateMonitors; m += rateSenders {
					start := base.Add(time.Duration(round) * time.Minute).Add(time.Duration(m) * time.Millisecond)
					batch = append(batch, checkResult(fmt.Sprintf("%s-s%d-r%d-m%d", runID, s, round, m), monitors[m], start))
				}
				w := a.sendResults(t, localToken, instances[s], batch...)
				answer := posted{status: w.Code, reasons: map[string]int{}}
				var reply probeproto.ResultsResponse
				if json.Unmarshal(w.Body.Bytes(), &reply) == nil {
					for _, outcome := range reply.Results {
						answer.reasons[cmp.Or(outcome.Reason, "accepted")]++
					}
				}
				answers <- answer
			}
		}()
	}
	wg.Wait()
	close(answers)
	outcomes := map[string]int{}
	for answer := range answers {
		require.Equal(t, http.StatusOK, answer.status)
		for reason, n := range answer.reasons {
			outcomes[reason] += n
		}
	}
	require.Equal(t, map[string]int{"accepted": rateMonitors * rateRounds}, outcomes, "every result is accepted")

	total := rateMonitors * rateRounds
	require.Eventually(t, func() bool { return storedHeartbeats(t, a) >= total }, 5*time.Minute, 5*time.Millisecond, "every result is stored")
	stored := time.Since(started)
	assert.Equal(t, total, storedHeartbeats(t, a), "none twice")
	return ingestRun{results: total, stored: stored}
}
