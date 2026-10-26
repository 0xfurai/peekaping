// Package metrics exposes the Status of Peekaping's Monitors as Prometheus metrics.
//
// The collector keeps the latest Heartbeat of every active Monitor in memory, fed
// by the event bus (Redis pub/sub in cluster mode), so it sees Heartbeats whichever api
// replica stored them. A scrape reads that memory only: no database round-trips.
package metrics

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting"
	"peekaping/internal/version"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// SyncInterval is how often the collector re-checks the Monitors it exports against the active ones
const SyncInterval = 5 * time.Minute

// LookupTimeout is how long a Heartbeat event waits to find its Monitor
const LookupTimeout = 10 * time.Second

var monitorLabels = []string{"monitor_id", "monitor_name", "monitor_type"}

var (
	statusDesc = prometheus.NewDesc(
		"peekaping_monitor_status",
		"Status of the Monitor's latest Heartbeat (0=Down, 1=Up, 2=Pending, 3=Maintenance).",
		monitorLabels, nil)
	heartbeatTimeDesc = prometheus.NewDesc(
		"peekaping_monitor_last_heartbeat_timestamp_seconds",
		"Unix time of the Monitor's latest Heartbeat. Alert on it to catch a Stale monitor.",
		monitorLabels, nil)
	responseDesc = prometheus.NewDesc(
		"peekaping_monitor_response_seconds",
		"Response time of the Monitor's latest Heartbeat, in seconds. Only present while that Heartbeat is Up.",
		monitorLabels, nil)
)

// latestHeartbeat is what an active Monitor exports: its latest Heartbeat, and the name and type to label it with
type latestHeartbeat struct {
	name        string
	monitorType string
	status      shared.MonitorStatus
	responseMs  int // Response time in milliseconds
	heartbeatAt time.Time
}

// Collector is a prometheus.Collector over the latest Heartbeat of each active Monitor.
// A series exists exactly while its Monitor has one in memory.
//
// Events race a sync pass, and the event bus handles events in separate goroutines. A pause, rename
// or delete that lands while a pass runs, or two MonitorUpdated events handled out of order, can
// leave a Monitor's old series or name until the next pass, which repairs it.
type Collector struct {
	monitorService   monitor.Service
	heartbeatService heartbeat.Service
	logger           *zap.SugaredLogger
	syncInterval     time.Duration
	lookupTimeout    time.Duration

	mu       sync.RWMutex
	monitors map[string]latestHeartbeat
}

// NewCollector builds a Collector. It does not touch the event bus until Start is called.
func NewCollector(monitorService monitor.Service, heartbeatService heartbeat.Service, logger *zap.SugaredLogger) *Collector {
	return &Collector{
		monitorService:   monitorService,
		heartbeatService: heartbeatService,
		logger:           logger.Named("[metrics-collector]"),
		syncInterval:     SyncInterval,
		lookupTimeout:    LookupTimeout,
		monitors:         make(map[string]latestHeartbeat),
	}
}

// Handler returns an http handler serving the Prometheus exposition format from a registry
// of its own: the Monitors, the build info, and the Go runtime and process collectors.
// It strips the CORS headers the router adds, since Prometheus doesn't need them and they
// would let a page on an allowed origin (any origin, with CORS_ORIGINS=*) read the Monitors'
// names.
func (c *Collector) Handler() http.HandlerFunc {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		c,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "peekaping_build_info",
			Help:        "Peekaping build information. Always 1; the version is carried in the label.",
			ConstLabels: prometheus.Labels{"version": version.Version},
		}, func() float64 { return 1 }),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	exposition := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
	return func(w http.ResponseWriter, r *http.Request) {
		for name := range w.Header() {
			if strings.HasPrefix(name, "Access-Control-") {
				w.Header().Del(name)
			}
		}
		exposition.ServeHTTP(w, r)
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- statusDesc
	ch <- heartbeatTimeDesc
	ch <- responseDesc
}

// Collect reads memory only. Label values are made valid UTF-8, since one bad name
// would otherwise fail the whole scrape.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	snapshot := maps.Clone(c.monitors)
	c.mu.RUnlock()

	for id, hb := range snapshot {
		labels := []string{id, strings.ToValidUTF8(hb.name, "\uFFFD"), strings.ToValidUTF8(hb.monitorType, "\uFFFD")}
		send(ch, statusDesc, float64(hb.status), labels)
		send(ch, heartbeatTimeDesc, float64(hb.heartbeatAt.Unix()), labels)
		if hb.status == shared.MonitorStatusUp {
			send(ch, responseDesc, float64(hb.responseMs)/1000, labels)
		}
	}
}

func send(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64, labels []string) {
	metric, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	if err != nil {
		metric = prometheus.NewInvalidMetric(desc, err)
	}
	ch <- metric
}

// Start subscribes to the event bus and, in the background, syncs with the active Monitors
// now and every SyncInterval until ctx is done. The channel it returns is closed once the
// background sync has stopped, so that the database can close after it.
func (c *Collector) Start(ctx context.Context, eventBus events.EventBus) <-chan struct{} {
	subscriber := events.NewSubscriber(eventBus, "metrics")
	events.Subscribe(subscriber, events.HeartbeatEvent, c.handleHeartbeat)
	events.Subscribe(subscriber, events.MonitorUpdated, c.handleMonitorUpdated)
	events.Subscribe(subscriber, events.MonitorDeleted, c.handleMonitorDeleted)

	stopped := make(chan struct{})
	reporting.Go("metrics", func() {
		defer close(stopped)
		c.runSync(ctx)
	})
	return stopped
}

func (c *Collector) runSync(ctx context.Context) {
	ticker := time.NewTicker(c.syncInterval)
	defer ticker.Stop()

	for {
		c.sync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sync checks the Monitors the collector exports against the active ones, which repairs a pause,
// rename or delete whose event never arrived, and loads the latest Heartbeat of an active Monitor
// that has none in memory, which is all of them after a restart. It only drops Monitors that were
// exported before it read the active ones, so a Monitor that got its first Heartbeat meanwhile stays.
func (c *Collector) sync(ctx context.Context) {
	c.mu.RLock()
	tracked := slices.Collect(maps.Keys(c.monitors))
	c.mu.RUnlock()

	active, err := c.monitorService.FindActive(ctx)
	if err != nil {
		c.logger.Errorw("Failed to find the active monitors", "error", err)
		return
	}

	isActive := make(map[string]bool, len(active))
	for _, mon := range active {
		if ctx.Err() != nil {
			return
		}
		isActive[mon.ID] = true
		if c.rename(mon) {
			continue
		}
		newest, err := c.heartbeatService.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
		if err != nil {
			c.logger.Errorw("Failed to find the latest heartbeat of a monitor", "monitor_id", mon.ID, "error", err)
			continue
		}
		if len(newest) > 0 {
			c.record(newest[0], mon)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range tracked {
		if !isActive[id] {
			delete(c.monitors, id)
		}
	}
}

// rename gives an exported Monitor the name and type in mon and reports whether it is exported.
func (c *Collector) rename(mon *monitor.Model) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	latest, tracked := c.monitors[mon.ID]
	if tracked {
		latest.name = mon.Name
		latest.monitorType = mon.Type
		c.monitors[mon.ID] = latest
	}
	return tracked
}

// handleMonitorUpdated drops a Monitor that was paused and moves one that was renamed to its new labels.
func (c *Collector) handleMonitorUpdated(_ context.Context, mon monitor.Model) {
	if !mon.Active {
		c.drop(mon.ID)
		return
	}
	c.rename(&mon)
}

func (c *Collector) handleMonitorDeleted(_ context.Context, monitorID string) {
	c.drop(monitorID)
}

func (c *Collector) drop(monitorID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.monitors, monitorID)
}

func (c *Collector) handleHeartbeat(ctx context.Context, hb heartbeat.Model) {
	c.mu.RLock()
	_, tracked := c.monitors[hb.MonitorID]
	c.mu.RUnlock()
	if tracked {
		c.record(&hb, nil)
		return
	}

	// The first Heartbeat of a Monitor has to name it
	ctx, cancel := context.WithTimeout(ctx, c.lookupTimeout)
	defer cancel()
	mon, err := c.monitorService.FindByID(ctx, hb.MonitorID)
	if err != nil {
		c.logger.Errorw("Failed to find the monitor of a heartbeat", "monitor_id", hb.MonitorID, "error", err)
		return
	}
	if mon == nil || !mon.Active {
		c.logger.Debugw("Ignoring a heartbeat of a monitor that is missing or paused", "monitor_id", hb.MonitorID)
		return
	}
	c.record(&hb, mon)
}

// record makes hb the Monitor's latest Heartbeat, unless the Monitor has a newer one already.
// A Monitor that isn't exported yet starts with the name and type in mon; without mon it isn't
// exported, as it was dropped since the caller looked.
func (c *Collector) record(hb *heartbeat.Model, mon *monitor.Model) {
	c.mu.Lock()
	defer c.mu.Unlock()

	latest, tracked := c.monitors[hb.MonitorID]
	switch {
	case tracked && hb.Time.Before(latest.heartbeatAt):
		return
	case !tracked && mon == nil:
		return
	case !tracked:
		latest = latestHeartbeat{name: mon.Name, monitorType: mon.Type}
	}
	latest.status = hb.Status
	latest.responseMs = hb.Ping
	latest.heartbeatAt = hb.Time
	c.monitors[hb.MonitorID] = latest
}
