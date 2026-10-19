// Package metrics exposes Peekaping monitor state as Prometheus metrics.
//
// The collector keeps the latest Heartbeat of every active Monitor in memory, fed
// by the event bus (Redis pub/sub in a multi-process deployment), so it sees
// Heartbeats whichever process ran the check. A scrape reads that memory only:
// no database round-trips, no polling of the worker.
package metrics

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// SyncInterval is how often the collector checks its state against the active Monitors
const SyncInterval = 5 * time.Minute

var monitorLabels = []string{"monitor_id", "monitor_name", "monitor_type"}

var (
	statusDesc = prometheus.NewDesc(
		"peekaping_monitor_status",
		"Status of the Monitor's latest Heartbeat (0=Down, 1=Up, 2=Pending, 3=Maintenance).",
		monitorLabels, nil)
	heartbeatTimeDesc = prometheus.NewDesc(
		"peekaping_monitor_last_heartbeat_timestamp_seconds",
		"Unix time of the Monitor's latest Heartbeat. Alert on it to catch a Monitor that stopped being checked.",
		monitorLabels, nil)
	responseDesc = prometheus.NewDesc(
		"peekaping_monitor_response_seconds",
		"Response time of the Monitor's latest Heartbeat, in seconds. Only present while that Heartbeat is Up.",
		monitorLabels, nil)
)

// monitorState is what the latest Heartbeat of an active Monitor exports
type monitorState struct {
	name        string
	monitorType string
	status      shared.MonitorStatus
	ping        int // Response time in milliseconds
	heartbeatAt time.Time
}

// Collector is a prometheus.Collector over the latest Heartbeat of each active Monitor.
// A series exists exactly while its Monitor has state.
type Collector struct {
	monitorService   monitor.Service
	heartbeatService heartbeat.Service
	logger           *zap.SugaredLogger
	syncInterval     time.Duration

	mu       sync.RWMutex
	monitors map[string]monitorState
}

// NewCollector builds a Collector. It does not touch the event bus until Start is called.
func NewCollector(monitorService monitor.Service, heartbeatService heartbeat.Service, logger *zap.SugaredLogger) *Collector {
	return &Collector{
		monitorService:   monitorService,
		heartbeatService: heartbeatService,
		logger:           logger.Named("[metrics-collector]"),
		syncInterval:     SyncInterval,
		monitors:         make(map[string]monitorState),
	}
}

// Handler returns an http handler serving the Prometheus exposition format from a registry
// of its own: the Monitors, the build info, and the Go runtime and process collectors.
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
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}).ServeHTTP
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

	for id, s := range snapshot {
		labels := []string{id, strings.ToValidUTF8(s.name, "\uFFFD"), strings.ToValidUTF8(s.monitorType, "\uFFFD")}
		send(ch, statusDesc, float64(s.status), labels)
		send(ch, heartbeatTimeDesc, float64(s.heartbeatAt.Unix()), labels)
		if s.status == shared.MonitorStatusUp {
			send(ch, responseDesc, float64(s.ping)/1000, labels)
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

// Start subscribes to the event bus.
func (c *Collector) Start(ctx context.Context, eventBus events.EventBus) {
	eventBus.Subscribe(events.HeartbeatEvent, c.handleHeartbeat)
}

func (c *Collector) handleHeartbeat(event events.Event) {
	hb, ok := infra.UnmarshalEventPayload[heartbeat.Model](event)
	if !ok {
		c.logger.Errorf("Failed to unmarshal heartbeat event payload")
		return
	}

	c.mu.RLock()
	_, tracked := c.monitors[hb.MonitorID]
	c.mu.RUnlock()
	if tracked {
		c.record(hb, nil)
		return
	}

	// The first Heartbeat of a Monitor has to name it
	mon, err := c.monitorService.FindByID(context.Background(), hb.MonitorID)
	if err != nil {
		c.logger.Errorw("Failed to find the monitor of a heartbeat", "monitor_id", hb.MonitorID, "error", err)
		return
	}
	if mon == nil || !mon.Active {
		c.logger.Debugw("Ignoring a heartbeat of a monitor that is missing or paused", "monitor_id", hb.MonitorID)
		return
	}
	c.record(hb, mon)
}

// record makes hb the Monitor's latest Heartbeat, unless the Monitor has a newer one already.
// A Monitor with no state starts with the name and type in mon; without mon it gets none, as
// it was dropped since the caller looked.
func (c *Collector) record(hb *heartbeat.Model, mon *monitor.Model) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, tracked := c.monitors[hb.MonitorID]
	switch {
	case tracked && hb.Time.Before(s.heartbeatAt):
		return
	case !tracked && mon == nil:
		return
	case !tracked:
		s = monitorState{name: mon.Name, monitorType: mon.Type}
	}
	s.status = hb.Status
	s.ping = hb.Ping
	s.heartbeatAt = hb.Time
	c.monitors[hb.MonitorID] = s
}
