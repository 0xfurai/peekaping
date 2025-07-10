package stats

import (
	"context"
	"fmt"
	"peekaping/src/modules/events"
	"peekaping/src/modules/shared"
	"time"

	"go.uber.org/zap"
)

type HeartbeatPayload struct {
	MonitorID string
	Status    int
	Ping      int
	Time      int64 // Unix seconds
}

type Service interface {
	AggregateHeartbeat(ctx context.Context, hb *HeartbeatPayload) error
	RegisterEventHandlers(eventBus *events.EventBus)
	FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod) ([]*Stat, error)
	FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod, monitorInterval int) ([]*Stat, error)
	StatPointsSummary(statsList []*Stat) *Stats
	DeleteByMonitorID(ctx context.Context, monitorID string) error
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

	for _, p := range periods {
		bucketTime := time.Unix(hb.Time, 0).Truncate(p.Bucket)

		stat, err := s.repo.GetOrCreateStat(ctx, hb.MonitorID, bucketTime, p.Period)
		if err != nil {
			return err
		}

		statToUpsert := *stat // copy

		// Up/Down logic (flattened)
		if s.flatStatus(hb.Status) == 1 { // MonitorStatusUp
			statToUpsert.Up = stat.Up + 1
			// Only update ping stats for true UP
			if hb.Status == 1 { // MonitorStatusUp
				fPing := float64(hb.Ping)
				if stat.Up == 0 {
					statToUpsert.PingMin = fPing
					statToUpsert.Ping = fPing
					statToUpsert.PingMax = fPing
				} else {
					statToUpsert.Ping = (stat.Ping*float64(stat.Up) + fPing) / float64(stat.Up+1)

					// Update ping min if new ping is lower
					if fPing < stat.PingMin || stat.PingMin == 0 {
						statToUpsert.PingMin = fPing
					} else {
						statToUpsert.PingMin = stat.PingMin
					}

					// Update ping max if new ping is higher
					if fPing > stat.PingMax {
						statToUpsert.PingMax = fPing
					} else {
						statToUpsert.PingMax = stat.PingMax
					}
				}
			}
		} else if s.flatStatus(hb.Status) == 0 { // MonitorStatusDown
			statToUpsert.Down = stat.Down + 1
		}

		// Aggregate maintenance status separately
		if hb.Status == 3 { // MonitorStatusMaintenance
			statToUpsert.Maintenance = stat.Maintenance + 1
		}

		// Upsert stat
		if err := s.repo.UpsertStat(ctx, &statToUpsert, p.Period); err != nil {
			return err
		}
	}
	return nil
}

func (s *ServiceImpl) RegisterEventHandlers(eventBus *events.EventBus) {
	eventBus.Subscribe(events.HeartbeatEvent, func(event events.Event) {
		payload, ok := event.Payload.(*shared.HeartBeatModel)
		if !ok {
			return
		}
		hb := &HeartbeatPayload{
			MonitorID: payload.MonitorID,
			Status:    int(payload.Status),
			Ping:      payload.Ping,
			Time:      payload.Time.Unix(),
		}
		_ = s.AggregateHeartbeat(context.Background(), hb)
	})
}

func (s *ServiceImpl) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod) ([]*Stat, error) {
	return s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, since, until, period)
}

func (s *ServiceImpl) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod, monitorInterval int) ([]*Stat, error) {
	stats, err := s.repo.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, since, until, period)
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

		// Only include stats with valid ping values (> 0) for ping calculations
		if stat.Up > 0 && stat.Ping > 0 {
			totalPing += stat.Ping * float64(stat.Up)
			pingCount += stat.Up

			if !hasValidPing {
				minPing = stat.PingMin
				maxPing = stat.PingMax
				hasValidPing = true
			} else {
				if stat.PingMin > 0 && stat.PingMin < minPing {
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
	var upCount int
	var totalUp, totalDown, totalMaintenance int

	for _, s := range statsList {
		if s.Up > 0 {
			if maxPing == nil || s.PingMax > *maxPing {
				v := s.PingMax
				maxPing = &v
			}
			if s.PingMin > 0 && (minPing == nil || s.PingMin < *minPing) {
				v := s.PingMin
				minPing = &v
			}
			sumPing += s.Ping * float64(s.Up)
			upCount += s.Up
		}
		totalUp += s.Up
		totalDown += s.Down
		totalMaintenance += s.Maintenance
	}

	var avgPing *float64
	if upCount > 0 {
		v := sumPing / float64(upCount)
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

func (s *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return s.repo.DeleteByMonitorID(ctx, monitorID)
}
