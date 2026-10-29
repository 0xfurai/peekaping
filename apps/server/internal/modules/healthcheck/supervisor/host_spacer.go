package supervisor

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	// maxTrackedHosts bounds how many hosts the spacer keeps a bucket for
	maxTrackedHosts = 10000
	// sweepEvery is how often the spacer looks for buckets that have been idle
	sweepEvery = 30 * time.Second
	// skipReportEvery is how often the spacer asks for the skipped checks of a host to be reported
	skipReportEvery = time.Minute
)

// HostSpacer spaces out the checks to one target host (CHECKS_PER_HOST_PER_MINUTE, PP-159). Each host
// has a token bucket of the cap per minute with a burst of one, so that the checks to it start at
// least a minute / cap apart however many monitors, workspaces and timers they come from. It counts
// for one checking process: the server's scheduler, or one remote probe. At 0 it is off, and costs
// a lock-free read.
type HostSpacer struct {
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu        sync.Mutex
	perMinute int
	hosts     map[string]*hostBucket
	lastSweep time.Time
	maxHosts  int
}

// hostBucket is the turns of one host. A bucket with a burst of one is kept as the time of the
// host's next turn: x/time/rate cannot take a turn later than now and still answer right the
// checks that come in before then, which a check with several hosts needs.
type hostBucket struct {
	// nextTurn is the earliest a check may start on the host. The bucket is full from then, so
	// forgetting it after that changes nothing.
	nextTurn time.Time
	// skipped counts the checks skipped since they were last reported, and reportedAt is when that was
	skipped    int
	reportedAt time.Time
}

// NewHostSpacer creates a spacer of perMinute checks per host per minute; 0 is off
func NewHostSpacer(perMinute int) *HostSpacer {
	return newHostSpacer(perMinute, time.Now)
}

// NewHostSpacerWithClock is NewHostSpacer over the given clock and wait, for tests of the code that
// calls the supervisor
func NewHostSpacerWithClock(perMinute int, now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) *HostSpacer {
	s := newHostSpacer(perMinute, now)
	s.sleep = sleep
	return s
}

func newHostSpacer(perMinute int, now func() time.Time) *HostSpacer {
	s := &HostSpacer{
		now:      now,
		sleep:    sleepContext,
		hosts:    map[string]*hostBucket{},
		maxHosts: maxTrackedHosts,
	}
	s.SetPerMinute(perMinute)
	return s
}

// sleepContext waits d, or until ctx ends, and reports why it stopped early
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetPerMinute changes the cap, which takes effect for the next check. A probe calls it at each
// hello. 0 turns the spacer off and forgets the hosts.
func (s *HostSpacer) SetPerMinute(perMinute int) {
	perMinute = max(perMinute, 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.perMinute
	s.perMinute = perMinute
	if perMinute == 0 {
		clear(s.hosts)
		return
	}
	now := s.now()
	for _, bucket := range s.hosts {
		// Turns already taken are repaid at the new rate
		if ahead := bucket.nextTurn.Sub(now); ahead > 0 && old > 0 {
			bucket.nextTurn = now.Add(time.Duration(float64(ahead) * float64(old) / float64(perMinute)))
		}
	}
}

// PerMinute is the cap now; 0 is off
func (s *HostSpacer) PerMinute() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perMinute
}

// Enabled reports whether the spacer limits anything
func (s *HostSpacer) Enabled() bool { return s.PerMinute() > 0 }

// tracked is how many hosts have a bucket now
func (s *HostSpacer) tracked() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts)
}

func intervalOf(perMinute int) time.Duration {
	return time.Minute / time.Duration(perMinute)
}

// Turn is what Reserve decided for one check
type Turn struct {
	// Wait is how long the check waits for its turn on every host it dials; zero runs it now
	Wait time.Duration
	// Denied is true when the turn is further away than the check may wait: the check is skipped
	// and has taken no turn
	Denied bool
	// Host is the host whose turn is the furthest away, when Denied
	Host string
	// ReportSkipped is how many checks of Host to report as skipped, when it is above zero: at most
	// once a minute per host, the checks skipped since the last report
	ReportSkipped int

	release func()
}

// Release gives the turns back, for a check that was cancelled while it waited for them
func (t Turn) Release() {
	if t.release != nil {
		t.release()
	}
}

// Reserve takes the check's turn on each of its hosts. The check waits for the latest of them if
// that is no more than maxWait, and otherwise takes none and is denied. A host that is listed twice
// counts once, whatever its case. With the spacer off every check has its turn now.
//
// The check dials all of its hosts once it starts, so it takes its turn on each of them at that
// start, not now. Each host's next turn is counted from the start too: a check of one of the hosts
// alone that comes in while this one waits does not get in just before it.
func (s *HostSpacer) Reserve(hosts []string, maxWait time.Duration) Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perMinute == 0 {
		return Turn{}
	}
	now := s.now()
	s.sweep(now)

	held := make(map[string]struct{}, len(hosts))
	names := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, dup := held[host]; dup {
			continue
		}
		held[host] = struct{}{}
		names = append(names, host)
	}
	if len(names) == 0 {
		return Turn{}
	}

	// All the hosts are held before any bucket is made, so making room forgets none of them
	buckets := make([]*hostBucket, len(names))
	var longest time.Duration
	farthest := 0
	for i, host := range names {
		buckets[i] = s.bucket(host, held)
		if wait := buckets[i].nextTurn.Sub(now); wait > longest {
			longest, farthest = wait, i
		}
	}

	if longest > maxWait {
		// No turn was taken yet, so there is none to give back
		bucket := buckets[farthest]
		bucket.skipped++
		turn := Turn{Denied: true, Host: names[farthest]}
		if now.Sub(bucket.reportedAt) >= skipReportEvery {
			turn.ReportSkipped, bucket.skipped, bucket.reportedAt = bucket.skipped, 0, now
		}
		return turn
	}

	next := now.Add(longest).Add(intervalOf(s.perMinute))
	before := make([]time.Time, len(buckets))
	for i, bucket := range buckets {
		before[i], bucket.nextTurn = bucket.nextTurn, next
	}
	return Turn{Wait: longest, release: func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, bucket := range buckets {
			// A turn taken on the host after this one keeps its place, so this one cannot be given back
			if bucket.nextTurn.Equal(next) {
				bucket.nextTurn = before[i]
			}
		}
	}}
}

// Wait sleeps for the turn's wait, or until ctx ends, and gives the turns back then
func (s *HostSpacer) Wait(ctx context.Context, turn Turn) error {
	if turn.Wait <= 0 {
		return nil
	}
	err := s.sleep(ctx, turn.Wait)
	if err != nil {
		turn.Release()
	}
	return err
}

// bucket finds the host's bucket, or makes it. Making room never forgets the buckets of held, the
// hosts of the check that is taking its turns. The caller holds mu.
func (s *HostSpacer) bucket(host string, held map[string]struct{}) *hostBucket {
	if bucket, ok := s.hosts[host]; ok {
		return bucket
	}
	if len(s.hosts) >= s.maxHosts {
		s.evictOldest(held)
	}
	bucket := &hostBucket{}
	s.hosts[host] = bucket
	return bucket
}

// sweep forgets the buckets that are full again, so that the map holds the hosts in use. A bucket
// with skipped checks not yet reported is kept for the minute they are held back. The caller holds mu.
func (s *HostSpacer) sweep(now time.Time) {
	if now.Sub(s.lastSweep) < sweepEvery {
		return
	}
	s.lastSweep = now
	for host, bucket := range s.hosts {
		if now.After(bucket.nextTurn) && (bucket.skipped == 0 || now.Sub(bucket.reportedAt) >= skipReportEvery) {
			delete(s.hosts, host)
		}
	}
}

// evictOldest forgets the bucket that has been idle the longest, to make room: the map is bounded
// even for a flood of hosts, at the price of letting that host through unspaced once. It keeps the
// buckets of held, whose turns a check is taking, so the map can outgrow maxHosts by the hosts of
// one check. The caller holds mu.
func (s *HostSpacer) evictOldest(held map[string]struct{}) {
	var oldest string
	var oldestIdle time.Time
	for host, bucket := range s.hosts {
		if _, ok := held[host]; ok {
			continue
		}
		if oldest == "" || bucket.nextTurn.Before(oldestIdle) {
			oldest, oldestIdle = host, bucket.nextTurn
		}
	}
	delete(s.hosts, oldest)
}

// normalizeHost makes the spelling of a host the key of its bucket: lower case, without spaces,
// the brackets of an IPv6 address or the dot that ends a fully qualified name
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}
