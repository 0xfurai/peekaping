package stats

import (
	"context"
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
