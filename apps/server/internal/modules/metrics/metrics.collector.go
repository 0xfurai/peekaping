// Package metrics exposes Peekaping monitor state as Prometheus metrics.
//
// The collector subscribes to the heartbeat/monitor events on the shared event
// bus and keeps a set of in-memory gauges up to date. Because it is fed from the
// event bus (Redis pub/sub in a multi-process deployment) it sees heartbeats
// regardless of which process actually ran the check, and scraping costs nothing
// at scrape time - no database round-trips, no polling of the worker.
package metrics

import (
	"context"
	"encoding/json"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Label names applied to every per-monitor metric.
const (
	labelMonitorID   = "monitor_id"
	labelMonitorName = "monitor_name"
	labelMonitorType = "monitor_type"
)

// monitorMeta is the bit of monitor metadata we need to label its metrics.
// We cache it so a heartbeat event does not require a database lookup once a
// monitor has been seen.
type monitorMeta struct {
	name        string
	monitorType string
}

func (m monitorMeta) labels(monitorID string) prometheus.Labels {
	return prometheus.Labels{
		labelMonitorID:   monitorID,
		labelMonitorName: m.name,
		labelMonitorType: m.monitorType,
	}
}

// Collector holds the Prometheus registry and the per-monitor gauges.
type Collector struct {
	monitorService monitor.Service
	logger         *zap.SugaredLogger

	registry *prometheus.Registry

	status        *prometheus.GaugeVec
	up            *prometheus.GaugeVec
	responseTime  *prometheus.GaugeVec
	lastHeartbeat *prometheus.GaugeVec

	mu    sync.RWMutex
	cache map[string]monitorMeta
}

// NewCollector builds a Collector with its own dedicated registry. It does not
// touch the event bus until Subscribe is called.
func NewCollector(monitorService monitor.Service, logger *zap.SugaredLogger) *Collector {
	registry := prometheus.NewRegistry()

	c := &Collector{
		monitorService: monitorService,
		logger:         logger.Named("[metrics-collector]"),
		registry:       registry,
		cache:          make(map[string]monitorMeta),
		status: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "peekaping_monitor_status",
			Help: "Current monitor status (0=down, 1=up, 2=pending, 3=maintenance).",
		}, []string{labelMonitorID, labelMonitorName, labelMonitorType}),
		up: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "peekaping_monitor_up",
			Help: "Whether the monitor is up (1) or not (0). Anything other than the up state reports 0.",
		}, []string{labelMonitorID, labelMonitorName, labelMonitorType}),
		responseTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "peekaping_monitor_response_seconds",
			Help: "Response time of the most recent heartbeat, in seconds.",
		}, []string{labelMonitorID, labelMonitorName, labelMonitorType}),
		lastHeartbeat: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "peekaping_monitor_last_heartbeat_timestamp_seconds",
			Help: "Unix timestamp of the most recent heartbeat. Useful for detecting stale monitors.",
		}, []string{labelMonitorID, labelMonitorName, labelMonitorType}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "peekaping_build_info",
		Help: "Peekaping build information. Always 1; the version is carried in the label.",
	}, []string{"version"})
	buildInfo.WithLabelValues(version.Version).Set(1)

	registry.MustRegister(
		c.status,
		c.up,
		c.responseTime,
		c.lastHeartbeat,
		buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return c
}

// Handler returns a gin handler serving the Prometheus exposition format.
func (c *Collector) Handler() gin.HandlerFunc {
	h := promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
	return gin.WrapH(h)
}

// Subscribe wires the collector up to the event bus. Call once at startup.
func (c *Collector) Subscribe(eventBus events.EventBus) {
	c.logger.Info("Subscribing to heartbeat and monitor events")

	eventBus.Subscribe(events.HeartbeatEvent, func(event events.Event) {
		c.handleHeartbeat(event)
	})
	eventBus.Subscribe(events.MonitorUpdated, func(event events.Event) {
		c.handleMonitorUpdated(event)
	})
	eventBus.Subscribe(events.MonitorDeleted, func(event events.Event) {
		c.handleMonitorDeleted(event)
	})
}

// Seed primes the gauges from the current state of every active monitor so the
// metrics are populated immediately on startup rather than only after the first
// heartbeat of each monitor. Failures here are non-fatal - the event stream will
// fill the gauges in regardless.
func (c *Collector) Seed(ctx context.Context) {
	monitors, err := c.monitorService.FindActive(ctx)
	if err != nil {
		c.logger.Warnw("Failed to seed metrics from active monitors", "error", err)
		return
	}

	for _, m := range monitors {
		meta := monitorMeta{name: m.Name, monitorType: m.Type}
		c.putMeta(m.ID, meta)
		c.setStatus(m.ID, meta, m.Status)
		if m.LastHeartbeat != nil {
			c.setHeartbeat(m.ID, meta, m.LastHeartbeat)
		}
	}
	c.logger.Infof("Seeded metrics for %d active monitors", len(monitors))
}

func (c *Collector) handleHeartbeat(event events.Event) {
	var hb shared.HeartBeatModel
	if err := unmarshalPayload(event.Payload, &hb); err != nil {
		c.logger.Errorw("Failed to unmarshal heartbeat event", "error", err)
		return
	}
	if hb.MonitorID == "" {
		return
	}

	meta := c.getMeta(hb.MonitorID)
	c.setStatus(hb.MonitorID, meta, hb.Status)
	c.setHeartbeat(hb.MonitorID, meta, &hb)
}

func (c *Collector) handleMonitorUpdated(event events.Event) {
	var m monitor.Model
	if err := unmarshalPayload(event.Payload, &m); err != nil {
		c.logger.Errorw("Failed to unmarshal monitor updated event", "error", err)
		return
	}
	if m.ID == "" {
		return
	}

	newMeta := monitorMeta{name: m.Name, monitorType: m.Type}

	// If the labels changed (e.g. a rename), drop the old series so we don't
	// leave a stale time series hanging around under the previous name.
	c.mu.RLock()
	old, existed := c.cache[m.ID]
	c.mu.RUnlock()
	if existed && old != newMeta {
		c.deleteSeries(m.ID, old)
	}

	c.putMeta(m.ID, newMeta)
}

func (c *Collector) handleMonitorDeleted(event events.Event) {
	var monitorID string
	if err := unmarshalPayload(event.Payload, &monitorID); err != nil {
		c.logger.Errorw("Failed to unmarshal monitor deleted event", "error", err)
		return
	}
	if monitorID == "" {
		return
	}

	c.mu.Lock()
	meta, ok := c.cache[monitorID]
	delete(c.cache, monitorID)
	c.mu.Unlock()

	if ok {
		c.deleteSeries(monitorID, meta)
	}
}

func (c *Collector) setStatus(monitorID string, meta monitorMeta, status shared.MonitorStatus) {
	labels := meta.labels(monitorID)
	c.status.With(labels).Set(float64(status))

	upVal := 0.0
	if status == shared.MonitorStatusUp {
		upVal = 1.0
	}
	c.up.With(labels).Set(upVal)
}

func (c *Collector) setHeartbeat(monitorID string, meta monitorMeta, hb *shared.HeartBeatModel) {
	labels := meta.labels(monitorID)
	// Ping is recorded in milliseconds; Prometheus convention is base units.
	c.responseTime.With(labels).Set(float64(hb.Ping) / 1000.0)
	if !hb.Time.IsZero() {
		c.lastHeartbeat.With(labels).Set(float64(hb.Time.Unix()))
	}
}

func (c *Collector) deleteSeries(monitorID string, meta monitorMeta) {
	labels := meta.labels(monitorID)
	c.status.Delete(labels)
	c.up.Delete(labels)
	c.responseTime.Delete(labels)
	c.lastHeartbeat.Delete(labels)
}

// getMeta returns the cached metadata for a monitor, falling back to a database
// lookup (and caching the result) on a miss. Lookups happen at event time, never
// at scrape time.
func (c *Collector) getMeta(monitorID string) monitorMeta {
	c.mu.RLock()
	meta, ok := c.cache[monitorID]
	c.mu.RUnlock()
	if ok {
		return meta
	}

	m, err := c.monitorService.FindByID(context.Background(), monitorID)
	if err != nil || m == nil {
		// Fall back to labelling by ID only so we still emit a metric.
		if err != nil {
			c.logger.Debugw("Failed to look up monitor for metrics labels", "monitor_id", monitorID, "error", err)
		}
		return monitorMeta{name: "", monitorType: ""}
	}

	meta = monitorMeta{name: m.Name, monitorType: m.Type}
	c.putMeta(monitorID, meta)
	return meta
}

func (c *Collector) putMeta(monitorID string, meta monitorMeta) {
	c.mu.Lock()
	c.cache[monitorID] = meta
	c.mu.Unlock()
}

// unmarshalPayload decodes an event payload that may arrive either as raw JSON
// (when it crossed the Redis bus) or as an already-constructed Go value (when it
// was published in-process). This mirrors the helper used by the other event
// listeners.
func unmarshalPayload(payload interface{}, target interface{}) error {
	switch p := payload.(type) {
	case json.RawMessage:
		return json.Unmarshal(p, target)
	case []byte:
		return json.Unmarshal(p, target)
	default:
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}
}
