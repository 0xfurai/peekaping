//go:build soak

package apitest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// TestProbeSoak_ThreeInstancesOneStops runs 1,000 monitors at 20 seconds on one probe, checked by
// three instances of it that share its token, and stops one of them in the middle with Shutdown,
// which says bye. The probes are the real client, the API the real one in process, and the target
// notes the time of every check. From apps/server:
//
//	go test -tags soak -run TestProbeSoak -count=1 -timeout 15m -v ./internal/apitest/
//
// It fails if, outside the settling at the start and the redeal window after the stop:
//   - a monitor is checked twice within half its interval, or
//   - a monitor goes longer than one and a half intervals between two checks, which includes the
//     stretch across the stop.
//
// The redeal window is one interval, and two seconds, after the stop: the monitors of the instance
// that stopped are dealt to the others, which check each at once, so each of those monitors has a
// check close to its last.
func TestProbeSoak_ThreeInstancesOneStops(t *testing.T) {
	runProbeSoak(t, newSingleApp(t))
}

// TestProbeSoak_ThreeInstancesOneStopsAgainstTheClusterAPI is the same run against the cluster API
// on SQLite: an api replica that stores what the probes send itself, in order, where the ingester
// used to, and counts the stats of it. From apps/server:
//
//	go test -tags soak -run TestProbeSoak_ThreeInstancesOneStopsAgainstTheClusterAPI -count=1 -timeout 15m -v ./internal/apitest/
func TestProbeSoak_ThreeInstancesOneStopsAgainstTheClusterAPI(t *testing.T) {
	a := newAppWithAdmin(t)
	a.countStatsOnTheLocalBus(t)
	runProbeSoak(t, a)
}

// runProbeSoak is the soak, against the app
func runProbeSoak(t *testing.T, a *app) {
	const (
		monitors = 1000
		interval = 20 * time.Second
		// minGap and maxGap bound the time between two checks of a monitor: half the interval, and
		// one and a half
		minGap = interval / 2
		maxGap = interval + interval/2
		// concurrency is how many checks one instance runs at once. The checks of all the monitors fall
		// due on the same second, 333 for each instance; at the default of 128 the three instances
		// dial the target with more connections at once than a socket's backlog (128 on macOS)
		// holds, and the target resets some. A real probe is limited the same way by its network.
		concurrency = 32
		// settle is how long the three instances take to share the monitors out and start the
		// timers they keep from then on
		settle = 2 * interval
		// judged is how long each of the two judged spans lasts
		judged = 2 * interval
		// redealWindow is how long after the stop a monitor may be checked twice
		redealWindow = interval + 2*time.Second
	)
	target := newCheckTarget(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})

	created0 := time.Now()
	paths := make([]string, monitors)
	for i := range monitors {
		paths[i] = fmt.Sprintf("/m/%04d", i)
		body := with(monitorOfTarget(fmt.Sprintf("Soak %04d", i), target.URL+paths[i], frankfurt.ID), "interval", int(interval/time.Second))
		w := a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	}
	t.Logf("created %d monitors in %s", monitors, time.Since(created0).Round(time.Millisecond))

	batches := &batchTimes{}
	probes := make([]interface {
		Shutdown(context.Context) error
		InstanceID() string
	}, 3)
	started := time.Now()
	for i := range probes {
		probes[i] = a.startProbe(t, frankfurt.Token, func(c *probeclient.Config) {
			c.Concurrency = concurrency
			c.HTTPClient = &http.Client{Transport: timedTransport{inner: inProcessTransport{router: a.router}, batches: batches}}
		})
	}

	checksOf := func(path string) []time.Time {
		times := target.requests(path)
		slices.SortFunc(times, func(x, y time.Time) int { return x.Compare(y) })
		return times
	}
	checked := func() (checks int, monitorsChecked int) {
		for _, path := range paths {
			if n := len(target.requests(path)); n > 0 {
				monitorsChecked++
				checks += n
			}
		}
		return checks, monitorsChecked
	}

	require.Eventually(t, func() bool { _, n := checked(); return n == monitors }, 2*time.Minute, time.Second,
		"every monitor is checked at least once")
	t.Logf("every monitor was checked %s after the probes started", time.Since(started).Round(time.Millisecond))
	time.Sleep(time.Until(started.Add(settle)))

	// judge fails the test for each check that comes too soon after, or too late after, the one
	// before it, from the first span's start. exempt says which checks may come early.
	judge := func(name string, from, to time.Time, exempt func(at time.Time) bool) (early int) {
		var problems []string
		for _, path := range paths {
			times := checksOf(path)
			var before time.Time
			var inSpan int
			for _, at := range times {
				if at.Before(from.Add(-maxGap)) {
					continue
				}
				if at.After(to) {
					break
				}
				if !before.IsZero() && !at.Before(from) {
					gap := at.Sub(before)
					switch {
					case gap > maxGap:
						problems = append(problems, fmt.Sprintf("%s went %s without a check, up to %s", path, gap.Round(time.Millisecond), at.Sub(from).Round(time.Millisecond)))
					case gap < minGap && !exempt(at):
						problems = append(problems, fmt.Sprintf("%s was checked twice within %s", path, gap.Round(time.Millisecond)))
					case gap < minGap:
						early++
					}
				}
				before = at
				if !at.Before(from) {
					inSpan++
				}
			}
			if inSpan == 0 || to.Sub(before) > maxGap {
				problems = append(problems, fmt.Sprintf("%s was not checked between %s and %s (last check %v)", path, from.Format("15:04:05"), to.Format("15:04:05"), before.Format("15:04:05")))
			}
		}
		if len(problems) > 10 {
			problems = append(problems[:10], fmt.Sprintf("... and %d more", len(problems)-10))
		}
		assert.Empty(t, problems, "%s: monitors checked at the wrong times", name)
		return early
	}

	// Span A: the three instances, settled
	heapSettled := heapAfterGC()
	spanA := time.Now()
	time.Sleep(judged)
	endA := time.Now()
	heapA := heapAfterGC()
	judge("before the stop", spanA, endA, func(time.Time) bool { return false })
	checksA, _ := checked()
	t.Logf("before the stop: %d checks of %d monitors in all, none twice within %s", checksA, monitors, minGap)

	// The stop: the instance says bye and its monitors are dealt to the other two
	stoppedAt := time.Now()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	require.NoError(t, probes[2].Shutdown(shutdown))
	cancel()
	t.Logf("instance %s shut down in %s", probes[2].InstanceID(), time.Since(stoppedAt).Round(time.Millisecond))
	time.Sleep(time.Until(stoppedAt.Add(redealWindow)))

	// The redeal: a monitor may be checked twice, and none may be missed
	early := judge("across the stop", endA, time.Now(), func(at time.Time) bool { return !at.Before(stoppedAt) })
	moved := early
	t.Logf("monitors checked early because they were dealt to another instance: about %d of %d", moved, monitors)
	assert.Less(t, moved, monitors/2, "only the share of the instance that stopped is dealt again, not every monitor")

	// Span B: the two that are left
	spanB := time.Now()
	time.Sleep(judged)
	judge("after the redeal", spanB, time.Now(), func(time.Time) bool { return false })
	heapB := heapAfterGC()
	checksB, _ := checked()
	t.Logf("after the redeal: %d checks in all, %d since the stop", checksB, checksB-checksA)

	// The results of all those checks are stored, those of the instance that stopped among them: it
	// flushed before it said bye, and the others send theirs every two seconds
	require.Eventually(t, func() bool { return storedHeartbeats(t, a) >= checksB }, 15*time.Second, 500*time.Millisecond,
		"a heartbeat is stored for each check")

	// The two that are left say bye, flushing what they hold first, and then there is one heartbeat
	// for each check that was made: none lost, none stored twice
	for _, p := range probes[:2] {
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		require.NoError(t, p.Shutdown(shutdown))
		cancel()
	}
	checksTotal, _ := checked()
	require.Eventually(t, func() bool { return storedHeartbeats(t, a) >= checksTotal }, 15*time.Second, 200*time.Millisecond,
		"a heartbeat is stored for each check")
	time.Sleep(time.Second)
	t.Logf("%d heartbeats stored for %d checks", storedHeartbeats(t, a), checksTotal)
	assert.Equal(t, checksTotal, storedHeartbeats(t, a), "one heartbeat for each check")

	// The API's memory: the live heap after a collection, when the instances settled, after the first
	// span and after the last. A run of a few minutes shows a leak that grows with the checks.
	t.Logf("API heap after a GC: %s settled, %s after the first span, %s after the last", mib(heapSettled), mib(heapA), mib(heapB))
	assert.Less(t, heapB, heapSettled*3/2+64<<20, "the API's live heap does not grow with the checks it stores")

	// The rate at which the API stores a batch: results in the batch over the time the request took
	batches.report(t)
	t.Logf("total duration of the run: %s", time.Since(created0).Round(time.Second))
}

// storedHeartbeats counts the heartbeats of all the monitors
func storedHeartbeats(t *testing.T, a *app) int {
	t.Helper()
	var count int
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("heartbeats").ColumnExpr("COUNT(*)").Scan(context.Background(), &count)
	}))
	return count
}
