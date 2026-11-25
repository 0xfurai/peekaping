package ingester

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/uptrace/bun"
)

const (
	// MinBaselineSamples is the minimum number of Up checks with response times in the last 24h
	// before baseline slow-response alerting can trigger, preventing false alarms on new monitors.
	MinBaselineSamples = 10
	// BaselineWindow is the lookback window used to compute a monitor's usual average response time.
	BaselineWindow = 24 * time.Hour
)

// SlowResponseState tracks consecutive slow Up checks and alert status for one (monitor, probe).
type SlowResponseState struct {
	MonitorID        string
	ProbeID          string
	ConsecutiveCount int
	Alerting         bool
	LastPingMs       int
	ThresholdMs      int
	UpdatedAt        time.Time
}

// SlowResponseStore persists per-(monitor, probe) slow response tracking state.
type SlowResponseStore interface {
	Get(ctx context.Context, monitorID, probeID string) (*SlowResponseState, error)
	Upsert(ctx context.Context, state *SlowResponseState) error
	Reset(ctx context.Context, monitorID, probeID string) error
}

// BaselineStatsReader reads historical response time summaries for baseline slow-response alerts.
type BaselineStatsReader interface {
	Summarize(ctx context.Context, monitorID, probeID string, since, until time.Time) (*stats.Stats, error)
}

// ServiceOption configures optional dependencies on ingester.ServiceImpl.
type ServiceOption func(*ServiceImpl)

// WithSlowResponse enables slow response time detection using store and baselineStats.
func WithSlowResponse(store SlowResponseStore, baselineStats BaselineStatsReader) ServiceOption {
	return func(s *ServiceImpl) {
		s.slowResponseStore = store
		s.baselineStats = baselineStats
	}
}

type slowResponseSQLModel struct {
	bun.BaseModel `bun:"table:monitor_slow_response_states,alias:msrs"`

	MonitorID        string    `bun:"monitor_id,pk"`
	ProbeID          string    `bun:"probe_id,pk"`
	ConsecutiveCount int       `bun:"consecutive_count,notnull"`
	Alerting         bool      `bun:"alerting,notnull"`
	LastPingMs       int       `bun:"last_ping_ms,notnull"`
	ThresholdMs      int       `bun:"threshold_ms,notnull"`
	UpdatedAt        time.Time `bun:"updated_at,notnull"`
}

// SQLSlowResponseStore stores slow response alert state in monitor_slow_response_states.
type SQLSlowResponseStore struct {
	db *bun.DB
}

func NewSQLSlowResponseStore(db *bun.DB) SlowResponseStore {
	if db == nil {
		return nil
	}
	return &SQLSlowResponseStore{db: db}
}

func (r *SQLSlowResponseStore) Get(ctx context.Context, monitorID, probeID string) (*SlowResponseState, error) {
	sm := new(slowResponseSQLModel)
	err := r.db.NewSelect().
		Model(sm).
		Where("monitor_id = ? AND probe_id = ?", monitorID, probeID).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &SlowResponseState{
		MonitorID:        sm.MonitorID,
		ProbeID:          sm.ProbeID,
		ConsecutiveCount: sm.ConsecutiveCount,
		Alerting:         sm.Alerting,
		LastPingMs:       sm.LastPingMs,
		ThresholdMs:      sm.ThresholdMs,
		UpdatedAt:        sm.UpdatedAt,
	}, nil
}

func (r *SQLSlowResponseStore) Upsert(ctx context.Context, state *SlowResponseState) error {
	sm := &slowResponseSQLModel{
		MonitorID:        state.MonitorID,
		ProbeID:          state.ProbeID,
		ConsecutiveCount: state.ConsecutiveCount,
		Alerting:         state.Alerting,
		LastPingMs:       state.LastPingMs,
		ThresholdMs:      state.ThresholdMs,
		UpdatedAt:        state.UpdatedAt.UTC(),
	}
	_, err := r.db.NewInsert().
		Model(sm).
		On("CONFLICT (monitor_id, probe_id) DO UPDATE").
		Set("consecutive_count = EXCLUDED.consecutive_count").
		Set("alerting = EXCLUDED.alerting").
		Set("last_ping_ms = EXCLUDED.last_ping_ms").
		Set("threshold_ms = EXCLUDED.threshold_ms").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

func (r *SQLSlowResponseStore) Reset(ctx context.Context, monitorID, probeID string) error {
	_, err := r.db.NewDelete().
		Model((*slowResponseSQLModel)(nil)).
		Where("monitor_id = ? AND probe_id = ?", monitorID, probeID).
		Exec(ctx)
	return err
}

// prepareSlowResponseBaseline reads the 24h baseline summary before the current heartbeat is
// stored so the current check does not skew its own baseline.
func (s *ServiceImpl) prepareSlowResponseBaseline(ctx context.Context, r *Result, probeID string, storedStatus shared.MonitorStatus) *stats.Stats {
	if s.slowResponseStore == nil || s.baselineStats == nil {
		return nil
	}
	if r.MonitorSlowResponseMode != shared.SlowResponseModeBaseline || storedStatus != shared.MonitorStatusUp {
		return nil
	}
	until := r.StartTime
	if until.IsZero() {
		until = time.Now().UTC()
	}
	summary, err := s.baselineStats.Summarize(ctx, r.MonitorID, probeID, until.Add(-BaselineWindow), until)
	if err != nil {
		s.logger.Warnw("Failed to read 24h baseline stats for slow response check",
			"monitor_id", r.MonitorID,
			"probe_id", probeID,
			"error", err,
		)
		return nil
	}
	return summary
}

// evaluateSlowResponse updates the per-(monitor, probe) slow response state and publishes a
// SlowResponse or SlowResponseRecovered event when the state transitions.
func (s *ServiceImpl) evaluateSlowResponse(ctx context.Context, r *Result, probeID string, storedStatus shared.MonitorStatus, baseline *stats.Stats) {
	if s.slowResponseStore == nil {
		return
	}
	mode := r.MonitorSlowResponseMode
	if mode == "" || mode == shared.SlowResponseModeOff {
		return
	}

	// Any non-Up check (Down, Pending, Maintenance) resets slow tracking so a subsequent Up
	// starts fresh and never emits a spurious latency recovery right after an outage recovery.
	if storedStatus != shared.MonitorStatusUp {
		if err := s.slowResponseStore.Reset(ctx, r.MonitorID, probeID); err != nil {
			s.logger.Warnw("Failed to reset slow response state",
				"monitor_id", r.MonitorID,
				"probe_id", probeID,
				"error", err,
			)
		}
		return
	}

	thresholdMs, baselineAvgMs, ok := computeSlowThreshold(r, baseline)
	if !ok {
		return
	}

	state, err := s.slowResponseStore.Get(ctx, r.MonitorID, probeID)
	if err != nil {
		s.logger.Warnw("Failed to read slow response state",
			"monitor_id", r.MonitorID,
			"probe_id", probeID,
			"error", err,
		)
		return
	}
	if state == nil {
		state = &SlowResponseState{MonitorID: r.MonitorID, ProbeID: probeID}
	}

	eventTime := r.StartTime
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}
	required := r.MonitorSlowResponseConsecutive
	if required < 1 {
		required = shared.DefaultSlowResponseConsecutive
	}

	isSlow := r.PingMs > thresholdMs
	if isSlow {
		state.ConsecutiveCount++
		state.LastPingMs = r.PingMs
		state.ThresholdMs = thresholdMs
		state.UpdatedAt = eventTime

		shouldAlert := !state.Alerting && state.ConsecutiveCount >= required
		if shouldAlert {
			state.Alerting = true
		}
		if err := s.slowResponseStore.Upsert(ctx, state); err != nil {
			s.logger.Warnw("Failed to save slow response state",
				"monitor_id", r.MonitorID,
				"probe_id", probeID,
				"error", err,
			)
			return
		}
		if shouldAlert {
			s.publishSlowResponse(ctx, events.SlowResponse, shared.SlowResponseEvent{
				MonitorID:        r.MonitorID,
				MonitorName:      r.MonitorName,
				ProbeID:          probeID,
				Mode:             mode,
				PingMs:           r.PingMs,
				ThresholdMs:      thresholdMs,
				BaselineAvgMs:    baselineAvgMs,
				Multiplier:       r.MonitorSlowResponseMultiplier,
				ConsecutiveCount: state.ConsecutiveCount,
				Time:             eventTime,
			})
		}
		return
	}

	// Response time is within threshold.
	if state.Alerting {
		state.Alerting = false
		state.ConsecutiveCount = 0
		state.LastPingMs = r.PingMs
		state.ThresholdMs = thresholdMs
		state.UpdatedAt = eventTime
		if err := s.slowResponseStore.Upsert(ctx, state); err != nil {
			s.logger.Warnw("Failed to save recovered slow response state",
				"monitor_id", r.MonitorID,
				"probe_id", probeID,
				"error", err,
			)
			return
		}
		s.publishSlowResponse(ctx, events.SlowResponseRecovered, shared.SlowResponseEvent{
			MonitorID:        r.MonitorID,
			MonitorName:      r.MonitorName,
			ProbeID:          probeID,
			Mode:             mode,
			PingMs:           r.PingMs,
			ThresholdMs:      thresholdMs,
			BaselineAvgMs:    baselineAvgMs,
			Multiplier:       r.MonitorSlowResponseMultiplier,
			ConsecutiveCount: 0,
			Time:             eventTime,
		})
		return
	}

	if state.ConsecutiveCount > 0 {
		state.ConsecutiveCount = 0
		state.LastPingMs = r.PingMs
		state.ThresholdMs = thresholdMs
		state.UpdatedAt = eventTime
		if err := s.slowResponseStore.Upsert(ctx, state); err != nil {
			s.logger.Warnw("Failed to clear slow response streak",
				"monitor_id", r.MonitorID,
				"probe_id", probeID,
				"error", err,
			)
		}
	}
}

// computeSlowThreshold calculates the effective threshold in milliseconds for the result.
// Returns ok=false when the monitor's settings or historical baseline are not sufficient to evaluate.
func computeSlowThreshold(r *Result, baseline *stats.Stats) (thresholdMs int, baselineAvgMs *float64, ok bool) {
	switch r.MonitorSlowResponseMode {
	case shared.SlowResponseModeFixed:
		if r.MonitorSlowResponseThresholdMs == nil || *r.MonitorSlowResponseThresholdMs < 1 {
			return 0, nil, false
		}
		return *r.MonitorSlowResponseThresholdMs, nil, true
	case shared.SlowResponseModeBaseline:
		if r.MonitorSlowResponseMultiplier == nil || *r.MonitorSlowResponseMultiplier <= 1.0 {
			return 0, nil, false
		}
		if baseline == nil || baseline.AvgPing == nil || baseline.PingCount < MinBaselineSamples || *baseline.AvgPing <= 0 {
			return 0, nil, false
		}
		avg := *baseline.AvgPing
		dynamic := int(math.Round(avg * *r.MonitorSlowResponseMultiplier))
		minMs := 0
		if r.MonitorSlowResponseMinMs != nil && *r.MonitorSlowResponseMinMs > 0 {
			minMs = *r.MonitorSlowResponseMinMs
		}
		effective := max(dynamic, minMs)
		if effective < 1 {
			effective = 1
		}
		return effective, &avg, true
	default:
		return 0, nil, false
	}
}

func (s *ServiceImpl) publishSlowResponse(ctx context.Context, topic events.Topic[shared.SlowResponseEvent], event shared.SlowResponseEvent) {
	if err := events.Publish(ctx, s.eventBus, topic, event); err != nil {
		s.logger.Errorw("Failed to publish slow response event",
			"topic", topic.Name(),
			"monitor_id", event.MonitorID,
			"probe_id", event.ProbeID,
			"error", err,
		)
	}
}
