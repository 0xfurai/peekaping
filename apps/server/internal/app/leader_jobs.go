package app

import (
	"context"
	"sync"
	"time"

	"peekaping/internal/leader"
	"peekaping/internal/reporting"
)

// leaderCheckEvery is how often a replica looks at whether it holds the leader lock, to start or
// stop the jobs that run only under it
const leaderCheckEvery = time.Second

// LeaderJob is a job that runs on one api replica at a time, the one that holds the leader lock
type LeaderJob struct {
	// Name is what the log calls the job
	Name string
	// Run does the job until ctx is done, and returns then
	Run func(ctx context.Context)
}

// LeaderJobs runs the jobs that only one api replica may run at a time, on the replica that holds
// the leader lock in Redis: cluster mode's stale-monitor watchdog, probe sweep and push monitors'
// checks. When the replica stops, or loses the lock, its jobs stop, and another replica takes the
// lock within its TTL and starts them.
type LeaderJobs struct {
	elector *leader.Elector
	jobs    []LeaderJob
	every   time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// LeaderJobsOption changes how LeaderJobs behaves, for a caller that has a reason to
type LeaderJobsOption func(*LeaderJobs)

// WithLeaderCheckEvery sets how often the replica looks at the lock, instead of once a second. A
// test that waits for an election sets it shorter.
func WithLeaderCheckEvery(every time.Duration) LeaderJobsOption {
	return func(j *LeaderJobs) { j.every = every }
}

// NewLeaderJobs creates the jobs that run on the replica elector says is the leader
func NewLeaderJobs(elector *leader.Elector, jobs []LeaderJob, opts ...LeaderJobsOption) *LeaderJobs {
	j := &LeaderJobs{elector: elector, jobs: jobs, every: leaderCheckEvery}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// Start joins the election and starts looking at the lock for each job. It returns at once, and
// does nothing the second time.
func (j *LeaderJobs) Start() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.started {
		return
	}
	j.started = true

	// Only Stop ends the election, once the jobs have returned: the election ending with some
	// context would release the leadership while the jobs still run
	j.elector.Start(context.Background())
	var ctx context.Context
	ctx, j.cancel = context.WithCancel(context.Background())
	for _, job := range j.jobs {
		j.wg.Add(1)
		reporting.Go("leader-jobs", func() {
			defer j.wg.Done()
			j.elector.RunWhileLeader(ctx, job.Name, j.every, func(ctx context.Context) error {
				job.Run(ctx)
				return nil
			})
		})
	}
}

// Stop stops the jobs and waits for them, and then releases the leadership this replica held, so
// that another replica takes over at once and not when the lock expires. The leadership stays with
// this replica until nothing it runs is left: a watchdog that outlived its leadership could run
// beside the next leader's. It may be called again, and does nothing if Start was not called.
func (j *LeaderJobs) Stop() {
	j.mu.Lock()
	started, cancel := j.started, j.cancel
	j.mu.Unlock()
	if !started {
		return
	}
	cancel()
	j.wg.Wait()
	j.elector.Stop()
}
