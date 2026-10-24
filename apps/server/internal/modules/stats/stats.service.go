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

func (s *ServiceImpl) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod, monitorInterval int) ([]*Stat, error) {
	stats, err := s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, probeID, since, until, period)
	if err != nil {
		return nil, err
	}

	// Determine bucket size
	var bucket time.Duration
	switch period {
	case StatMinutely:
		bucket = time.Minute
	case StatHourly:
		bucket = time.Hour
	case StatDaily:
		bucket = 24 * time.Hour
	default:
		bucket = time.Minute
	}

	// For minute-level stats, check if we need to group by monitor interval
	if period == StatMinutely && monitorInterval > 60 {
		// Monitor interval is greater than 1 minute, group minute stats
		monitorIntervalDuration := time.Duration(monitorInterval) * time.Second
		// calculate closest bigger interval that is divisible by minute (60)
		monitorIntervalDuration = monitorIntervalDuration.Round(time.Minute)
		return s.groupStatsByInterval(stats, since, until, monitorIntervalDuration, monitorID)
	}

	// Build a map for quick lookup
	statMap := make(map[int64]*Stat)
	for _, stat := range stats {
		statMap[stat.Timestamp.Unix()] = stat
	}

	// Fill missing intervals
	targetBucketLength := int(until.Sub(since)/bucket) + 1
	result := make([]*Stat, 0, targetBucketLength)
	for t := since.Truncate(bucket); !t.After(until); t = t.Add(bucket) {
		key := t.Unix()
		if stat, ok := statMap[key]; ok {
			result = append(result, stat)
		} else {
			result = append(result, &Stat{
				ID:          "", // TODO: check if this brake ui (react key)
				MonitorID:   monitorID,
				Timestamp:   t,
				Ping:        0,
				PingMin:     0,
				PingMax:     0,
				Up:          0,
				Down:        0,
				Maintenance: 0,
			})
		}
	}

	return result, nil
}

// groupStatsByInterval groups minute-level stats into monitor interval buckets
func (s *ServiceImpl) groupStatsByInterval(minuteStats []*Stat, since, until time.Time, interval time.Duration, monitorID string) ([]*Stat, error) {
	if len(minuteStats) == 0 {
		return []*Stat{}, nil
	}

	// check minuteStats are sorted
	for i := 1; i < len(minuteStats); i++ {
		if minuteStats[i].Timestamp.Before(minuteStats[i-1].Timestamp) {
			return nil, fmt.Errorf("minuteStats are not sorted")
		}
	}

	periods := int((until.Sub(since) + interval - 1) / interval)
	result := make([]*Stat, 0, periods)
	minuteStatsPointer := 0

	for i := 0; i < periods; i++ {
		bucketStart := since.Add(time.Duration(i) * interval)
		bucketEnd := bucketStart.Add(interval)

		bucketStats := make([]*Stat, 0)

		// Skip stats that are before the current bucket start
		for minuteStatsPointer < len(minuteStats) && minuteStats[minuteStatsPointer].Timestamp.Before(bucketStart) {
			minuteStatsPointer++
		}

		// Include stats that are within the current bucket (inclusive of bucketStart, exclusive of bucketEnd)
		// For the last bucket, also include stats exactly at the until timestamp
		for minuteStatsPointer < len(minuteStats) &&
			(minuteStats[minuteStatsPointer].Timestamp.Before(bucketEnd) ||
				(i == periods-1 && minuteStats[minuteStatsPointer].Timestamp.Equal(until))) {
			bucketStats = append(bucketStats, minuteStats[minuteStatsPointer])
			minuteStatsPointer++
		}

		if len(bucketStats) > 0 {
			aggregated := s.aggregateStats(bucketStats, bucketStart, monitorID)
			result = append(result, aggregated)
		} else {
			result = append(result, &Stat{
				ID:          "",
				MonitorID:   monitorID,
				Timestamp:   bucketStart,
				Ping:        0,
				PingMin:     0,
				PingMax:     0,
				Up:          0,
				Down:        0,
				Maintenance: 0,
			})
		}
	}

	return result, nil
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
	total := totalUp + totalDown + totalMaintenance
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
