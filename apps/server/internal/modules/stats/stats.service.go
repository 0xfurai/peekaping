package stats

import (
	"context"
	"fmt"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/shared"
	"time"

	"go.uber.org/zap"
)

type HeartbeatPayload struct {
	MonitorID string
	ProbeID   string // the probe that ran the check; empty means the local probe
	Status    int
	Ping      int
	Time      int64 // Unix seconds
}

type Service interface {
	AggregateHeartbeat(ctx context.Context, hb *HeartbeatPayload) error
	RegisterEventHandlers(eventBus events.EventBus)
	// The Find and Summarize methods take a probeID that narrows them to the checks one probe ran.
	// An empty probeID covers every probe.
	FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod) ([]*Stat, error)
	FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod, monitorInterval int) ([]*Stat, error)
	StatPointsSummary(statsList []*Stat) *Stats
	Summarize(ctx context.Context, monitorID, probeID string, since, until time.Time) (*Stats, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteExpired(ctx context.Context, now time.Time) (map[StatPeriod]int64, error)
}

type ServiceImpl struct {
	repo   Repository
	logger *zap.SugaredLogger
}

func NewService(repo Repository, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{repo, logger.Named("[stats-service]")}
}

func (s *ServiceImpl) flatStatus(status int) int {
	switch status {
	case 1, 3: // MonitorStatusUp, MonitorStatusMaintenance
		return 1 // MonitorStatusUp
	case 0, 2: // MonitorStatusDown, MonitorStatusPending
		return 0 // MonitorStatusDown
	default:
		return -1
	}
}

func (s *ServiceImpl) AggregateHeartbeat(ctx context.Context, hb *HeartbeatPayload) error {
	periods := []struct {
		Period StatPeriod
		Bucket time.Duration
	}{
		{StatMinutely, time.Minute},
		{StatHourly, time.Hour},
		{StatDaily, 24 * time.Hour},
	}

	// The heartbeat's counts and response time, which the repository adds to each period's stat
	add := Stat{MonitorID: hb.MonitorID, ProbeID: hb.ProbeID}
	if add.ProbeID == "" {
		add.ProbeID = shared.LocalProbeID
	}

	// Up/Down logic (flattened)
	if s.flatStatus(hb.Status) == 1 { // MonitorStatusUp
		add.Up = 1
		// Only update ping stats for true UP
		if hb.Status == 1 { // MonitorStatusUp
			add.PingCount = 1
			add.Ping = float64(hb.Ping)
			add.PingMin, add.PingMax = add.Ping, add.Ping
		}
	} else if s.flatStatus(hb.Status) == 0 { // MonitorStatusDown
		add.Down = 1
	}

	// Aggregate maintenance status separately
	if hb.Status == 3 { // MonitorStatusMaintenance
		add.Maintenance = 1
	}

	for _, p := range periods {
		add.Timestamp = time.Unix(hb.Time, 0).Truncate(p.Bucket)
		if err := s.repo.AddToStat(ctx, &add, p.Period); err != nil {
			return err
		}
	}
	return nil
}

func (s *ServiceImpl) RegisterEventHandlers(eventBus events.EventBus) {
	events.Subscribe(events.NewSubscriber(eventBus, "stats"), events.HeartbeatEvent, func(ctx context.Context, payload shared.HeartBeatModel) {
		hb := &HeartbeatPayload{
			MonitorID: payload.MonitorID,
			ProbeID:   payload.ProbeID,
			Status:    int(payload.Status),
			Ping:      payload.Ping,
			Time:      payload.Time.Unix(),
		}
		if err := s.AggregateHeartbeat(ctx, hb); err != nil {
			s.logger.Errorw("Failed to aggregate heartbeat into stats", "monitor_id", hb.MonitorID, "error", err)
		}
	})
}

func (s *ServiceImpl) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod) ([]*Stat, error) {
	return s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, probeID, since, until, period)
}

// FindStatsByMonitorIDAndTimeRangeWithInterval returns one point per bucket from the bucket holding
// since to the one holding until. A bucket is a minute, an hour or a day wide; the minute stats of a
// monitor checked less often than every minute are grouped into buckets of its interval, rounded up
// to whole minutes. Buckets start on a multiple of their width from the Unix epoch, so a chart read
// a minute later shows the same buckets. Buckets without stats are empty points.
func (s *ServiceImpl) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod, monitorInterval int) ([]*Stat, error) {
	width := bucketWidth(period, monitorInterval)
	first := alignToEpoch(since, width)

	stats, err := s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, probeID, first, until, period)
	if err != nil {
		return nil, err
	}

	// A stat belongs to the bucket that starts at or before its timestamp.
	count := 0
	if !until.Before(first) {
		count = int(until.Sub(first)/width) + 1
	}
	inBucket := make([][]*Stat, count)
	for _, stat := range stats {
		if i := int(stat.Timestamp.Sub(first) / width); i >= 0 && i < count {
			inBucket[i] = append(inBucket[i], stat)
		}
	}

	result := make([]*Stat, 0, count)
	for i, bucketStats := range inBucket {
		result = append(result, s.aggregateStats(bucketStats, first.Add(time.Duration(i)*width), monitorID))
	}
	return result, nil
}

// bucketWidth is how wide a chart point is: a minute, an hour or a day, or for the minute stats of a
// monitor checked less often than every minute its interval rounded up to whole minutes.
func bucketWidth(period StatPeriod, monitorInterval int) time.Duration {
	switch period {
	case StatHourly:
		return time.Hour
	case StatDaily:
		return 24 * time.Hour
	}
	if period == StatMinutely && monitorInterval > 60 {
		return (time.Duration(monitorInterval)*time.Second + time.Minute - 1).Truncate(time.Minute)
	}
	return time.Minute
}

// alignToEpoch returns the latest moment at or before t that is a multiple of width from the Unix
// epoch, in t's location. time.Time.Truncate counts from year 1, which differs from the epoch for
// widths that don't divide the time between them, such as 7 minutes.
func alignToEpoch(t time.Time, width time.Duration) time.Time {
	widthSeconds := int64(width / time.Second)
	remainder := t.Unix() % widthSeconds
	if remainder < 0 {
		remainder += widthSeconds
	}
	return t.Add(-(time.Duration(remainder)*time.Second + time.Duration(t.Nanosecond())))
}

// aggregateStats calculates the average and aggregated values for a bucket of stats
func (s *ServiceImpl) aggregateStats(stats []*Stat, timestamp time.Time, monitorID string) *Stat {
	if len(stats) == 0 {
		return &Stat{
			ID:          "",
			MonitorID:   monitorID,
			Timestamp:   timestamp,
			Ping:        0,
			PingMin:     0,
			PingMax:     0,
			Up:          0,
			Down:        0,
			Maintenance: 0,
		}
	}

	var totalPing, minPing, maxPing float64
	var totalUp, totalDown, totalMaintenance int
	var pingCount int
	var hasValidPing bool

	for _, stat := range stats {
		totalUp += stat.Up
		totalDown += stat.Down
		totalMaintenance += stat.Maintenance

		// Only buckets with response times count toward the response time stats
		if stat.PingCount > 0 {
			totalPing += stat.Ping * float64(stat.PingCount)
			pingCount += stat.PingCount

			if !hasValidPing {
				minPing = stat.PingMin
				maxPing = stat.PingMax
				hasValidPing = true
			} else {
				if stat.PingMin < minPing {
					minPing = stat.PingMin
				}
				if stat.PingMax > maxPing {
					maxPing = stat.PingMax
				}
			}
		}
	}

	// Calculate average ping
	avgPing := 0.0
	if pingCount > 0 {
		avgPing = totalPing / float64(pingCount)
	}

	// Set min/max ping to 0 if no valid ping data
	if !hasValidPing {
		minPing = 0
		maxPing = 0
	}

	return &Stat{
		ID:          "",
		MonitorID:   monitorID,
		Timestamp:   timestamp,
		Ping:        avgPing,
		PingMin:     minPing,
		PingMax:     maxPing,
		PingCount:   pingCount,
		Up:          totalUp,
		Down:        totalDown,
		Maintenance: totalMaintenance,
	}
}

// StatPointsSummary is a local struct for summary in stats package (avoid import cycle)
type Stats struct {
	MaxPing     *float64 `json:"maxPing"`
	MinPing     *float64 `json:"minPing"`
	AvgPing     *float64 `json:"avgPing"`
	Uptime      *float64 `json:"uptime"`
	Maintenance *float64 `json:"maintenance"`
}

// StatPointsSummary computes stat points and summary for a period using flatStatus logic
func (s *ServiceImpl) StatPointsSummary(statsList []*Stat) *Stats {
	var maxPing *float64
	var minPing *float64
	var sumPing float64
	var pingCount int
	var totalUp, totalDown, totalMaintenance int

	for _, s := range statsList {
		if s.PingCount > 0 {
			if maxPing == nil || s.PingMax > *maxPing {
				v := s.PingMax
				maxPing = &v
			}
			if minPing == nil || s.PingMin < *minPing {
				v := s.PingMin
				minPing = &v
			}
			sumPing += s.Ping * float64(s.PingCount)
			pingCount += s.PingCount
		}
		totalUp += s.Up
		totalDown += s.Down
		totalMaintenance += s.Maintenance
	}

	var avgPing *float64
	if pingCount > 0 {
		v := sumPing / float64(pingCount)
		avgPing = &v
	}

	var uptime *float64
	var maintenance *float64
	// Up already holds the maintenance checks, so adding totalMaintenance would count each twice.
	total := totalUp + totalDown
	if total > 0 {
		uptimeV := float64(totalUp) / float64(total) * 100
		uptime = &uptimeV

		maintenanceV := float64(totalMaintenance) / float64(total) * 100
		maintenance = &maintenanceV
	}

	return &Stats{
		MaxPing:     maxPing,
		MinPing:     minPing,
		AvgPing:     avgPing,
		Uptime:      uptime,
		Maintenance: maintenance,
	}
}

// summaryWindows lists the longest window served by minute and hour stats.
// Retention must cover the whole window; day stats serve longer windows indefinitely.
var summaryWindows = []struct {
	Period StatPeriod
	Window time.Duration
}{
	{StatMinutely, 24 * time.Hour},
	{StatHourly, 90 * 24 * time.Hour},
}

// summaryPeriod picks the buckets to read for a window. A bucket counts only when
// it starts inside the window, so the window's start is off by up to one bucket:
// a minute for windows up to 24h, an hour up to 90 days, a day beyond that.
// Stats retention must keep minute rows for at least 24h and hour rows for 90 days.
func summaryPeriod(window time.Duration) StatPeriod {
	for _, summary := range summaryWindows {
		if window <= summary.Window {
			return summary.Period
		}
	}
	return StatDaily
}

// Summarize returns uptime and response times for the monitor's heartbeats from
// since to until, read from the buckets that suit the window length.
func (s *ServiceImpl) Summarize(ctx context.Context, monitorID, probeID string, since, until time.Time) (*Stats, error) {
	statsList, err := s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, probeID, since, until, summaryPeriod(until.Sub(since)))
	if err != nil {
		return nil, err
	}
	return s.StatPointsSummary(statsList), nil
}

func (s *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return s.repo.DeleteByMonitorID(ctx, monitorID)
}

// retention is how long each period's stats are kept. Day stats are kept forever.
// Summarize reads minute stats for up to 24h and hour stats for up to 90 days,
// and charts read less, so neither may be kept for less than that.
var retention = []struct {
	Period StatPeriod
	Bucket time.Duration
	Keep   time.Duration
}{
	{StatMinutely, time.Minute, 48 * time.Hour},
	{StatHourly, time.Hour, 90 * 24 * time.Hour},
}

// DeleteExpired deletes the stats whose bucket started more than their period's
// retention before now, and returns how many rows it deleted per period.
func (s *ServiceImpl) DeleteExpired(ctx context.Context, now time.Time) (map[StatPeriod]int64, error) {
	deleted := make(map[StatPeriod]int64, len(retention))
	for _, r := range retention {
		n, err := s.repo.DeleteOlderThan(ctx, r.Period, now.Add(-r.Keep).Truncate(r.Bucket))
		if err != nil {
			return deleted, fmt.Errorf("delete expired %s stats: %w", r.Period, err)
		}
		deleted[r.Period] = n
	}
	return deleted, nil
}
