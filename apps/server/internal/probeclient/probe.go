package probeclient

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/scheduler"
)

// DefaultFlushTimeout is how long Shutdown gives the last flush of the results, and then bye
const DefaultFlushTimeout = 10 * time.Second

// Config is what a Probe needs
type Config struct {
	// URL is the server's origin, such as https://peekaping.example.com
	URL string
	// Token is the probe's token
	Token string
	// Version is the version of the probe's program, which hello states
	Version string
	// HTTPClient makes the requests to the server; http.DefaultClient when nil. Tests give it a
	// transport that serves the requests in process.
	HTTPClient *http.Client
	// Concurrency is how many checks run at the same time; scheduler.DefaultConcurrency when zero
	Concurrency int
	// FlushTimeout is how long Shutdown gives the last flush, and again the bye;
	// DefaultFlushTimeout when zero
	FlushTimeout time.Duration
	Logger       *zap.SugaredLogger

	// Sleep and Now stand in for the probe's waits and clock in tests; see SessionConfig
	Sleep func(ctx context.Context, d time.Duration) error
	Now   func() time.Time
}

// Probe is the program that runs checks for a remote probe: it follows its assignment over the
// server's long poll, checks the monitors in it with the scheduler of single mode, and sends the
// results back in batches. While the server can't be reached it goes on checking the last
// assignment it has, and keeps the results to send when it can.
//
// New it, Run it for as long as the process lives, and Shutdown it when the process is asked to
// stop.
type Probe struct {
	session   *Session
	source    *Source
	sink      *Sink
	scheduler *scheduler.Scheduler
	logger    *zap.SugaredLogger

	flushTimeout time.Duration

	mu         sync.Mutex
	started    bool
	stopSource context.CancelFunc
	stopSink   context.CancelFunc
	loops      sync.WaitGroup

	shutdownOnce sync.Once
	shutdownErr  error
}

// New creates a probe with every executor the program has. It makes no call: Run does.
func New(cfg Config) *Probe {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop().Sugar()
	}
	registry := executor.NewExecutorRegistry(cfg.Logger)
	executors := NewExecutors(registry)

	session := NewSession(SessionConfig{
		URL:            cfg.URL,
		Token:          cfg.Token,
		ProbeVersion:   cfg.Version,
		SupportedTypes: executors.SupportedTypes(),
		HTTPClient:     cfg.HTTPClient,
		Logger:         cfg.Logger,
		Sleep:          cfg.Sleep,
		Now:            cfg.Now,
	})
	source := NewSource(session)
	sink := NewSink(session)
	flushTimeout := cfg.FlushTimeout
	if flushTimeout <= 0 {
		flushTimeout = DefaultFlushTimeout
	}
	return &Probe{
		session: session,
		source:  source,
		sink:    sink,
		scheduler: scheduler.New(scheduler.Config{
			Source:      source,
			Executors:   executors,
			Supervisor:  healthcheck.NewHealthCheck(nil, registry, cfg.Logger), // it needs neither the event bus nor the registry to run a tick
			Ingester:    sink,
			Concurrency: cfg.Concurrency,
			Logger:      cfg.Logger,
		}),
		logger:       cfg.Logger.With("component", "probe"),
		flushTimeout: flushTimeout,
	}
}

// InstanceID is the random id this process goes by at the server
func (p *Probe) InstanceID() string { return p.session.InstanceID() }

// Ready reports whether the probe has received its first assignment. It stays true while the
// server can't be reached, since the probe keeps checking.
func (p *Probe) Ready() bool { return p.source.Ready() }

// Run starts the probe and returns when ctx ends. It starts the scheduler, which checks what the
// assignment lists, the poll that keeps the assignment up to date, and the flusher that sends the
// results, and it blocks while they run. It says hello itself, and again after every reconnect, and
// does not return for a server it can't reach.
//
// Run's ctx ends the poll and the flusher, not the scheduler: the checks go on until Shutdown, so
// that a stop lets the running ones finish. Run returns an error only when the scheduler can't start.
func (p *Probe) Run(ctx context.Context) error {
	sourceCtx, stopSource := context.WithCancel(ctx)
	sinkCtx, stopSink := context.WithCancel(ctx)
	defer stopSource()
	defer stopSink()

	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return errors.New("probe already started")
	}
	p.started = true
	p.stopSource, p.stopSink = stopSource, stopSink
	p.loops.Add(2)
	p.mu.Unlock()

	// The scheduler listens to the source before the first assignment arrives
	if err := p.scheduler.Start(context.WithoutCancel(ctx)); err != nil {
		p.loops.Add(-2)
		return err
	}
	go func() {
		defer p.loops.Done()
		p.source.Run(sourceCtx)
	}()
	go func() {
		defer p.loops.Done()
		p.sink.Run(sinkCtx)
	}()
	p.logger.Infow("Probe started", "instance_id", p.session.InstanceID())

	p.loops.Wait()
	return nil
}

// Shutdown stops the probe: the scheduler stops its timers and lets the checks that are running
// finish, for as long as ctx lasts, and cancels what is left; the poll stops; the results still in
// the buffer are sent; and the server is told that this instance is gone, so that it deals the
// instance's monitors to the others at once. The flush and the bye each get the flush timeout of
// the config, whether ctx has run out or not. It returns what failed of the last two, joined.
func (p *Probe) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() { p.shutdownErr = p.shutdown(ctx) })
	return p.shutdownErr
}

func (p *Probe) shutdown(ctx context.Context) error {
	p.scheduler.Drain(ctx)

	// No more polls: a poll after bye would count the instance in again
	p.mu.Lock()
	stopSource, stopSink := p.stopSource, p.stopSink
	p.mu.Unlock()
	if stopSource != nil {
		stopSource()
	}

	tail := context.WithoutCancel(ctx)
	flushCtx, cancelFlush := context.WithTimeout(tail, p.flushTimeout)
	defer cancelFlush()
	var flushErr, byeErr error
	if err := p.sink.Flush(flushCtx); err != nil {
		flushErr = err
		p.logger.Errorw("Failed to send the buffered results; they are lost", "buffered", p.sink.Buffered(), "error", err)
	}
	if stopSink != nil {
		stopSink()
	}
	p.loops.Wait()

	// An instance the server never heard from has nothing to leave
	if p.session.Welcome() != nil {
		byeCtx, cancelBye := context.WithTimeout(tail, p.flushTimeout)
		defer cancelBye()
		if err := p.session.Bye(byeCtx); err != nil {
			byeErr = err
			p.logger.Warnw("Failed to tell the server this instance is stopping", "error", err)
		}
	}
	p.logger.Info("Probe stopped")
	return errors.Join(flushErr, byeErr)
}
