// Package stale finds and records monitors that Peekaping has stopped checking. The
// watchdog writes the records; the API reads them for the health endpoint and the UI.
package stale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"peekaping/internal/modules/events"

	"github.com/redis/go-redis/v9"
)

// Redis keys shared by the api replicas
const (
	MonitorsKey = "peekaping:watchdog:stale"  // HASH: monitor_id -> Entry JSON
	RanAtKey    = "peekaping:watchdog:ran_at" // STRING: unix ms of the watchdog's last run
)

// Entry is one stale monitor
type Entry struct {
	MonitorID string `json:"monitor_id"`
	// Since is when the monitor was last checked, or last saved or resumed if that's later
	Since time.Time `json:"since"`
	// DetectedAt is when the watchdog found the monitor stale and asked for a check right away
	DetectedAt time.Time `json:"detected_at"`
	// Notified is set once the stale notification has been sent
	Notified bool `json:"notified"`
}

// Event is the payload of the MonitorStale and MonitorStaleRecovered topics
type Event struct {
	MonitorID   string    `json:"monitor_id"`
	MonitorName string    `json:"monitor_name"`
	Since       time.Time `json:"since"`
	// UnsupportedProbes are the names of the monitor's probes that don't run its type, which is why
	// no probe checks it. Only MonitorStale carries them, and only when there are any.
	UnsupportedProbes []string `json:"unsupported_probes,omitempty"`
}

// The topics of Event. The watchdog publishes them and the notification channels listen.
var (
	// MonitorStale is published when a monitor is still not checked after the watchdog asked for a check
	MonitorStale = events.NewTopic("monitor.stale", func(e Event) string { return e.MonitorID })
	// MonitorStaleRecovered is published when a monitor that was reported stale is checked again
	MonitorStaleRecovered = events.NewTopic("monitor.stale.recovered", func(e Event) string { return e.MonitorID })
)

// Threshold is how long a monitor can go without a heartbeat before it counts as stale:
// max(3 × check interval, check interval + timeout + 60s)
func Threshold(intervalSeconds, timeoutSeconds int) time.Duration {
	interval := time.Duration(intervalSeconds) * time.Second
	timeout := time.Duration(timeoutSeconds) * time.Second
	return max(3*interval, interval+timeout+time.Minute)
}

// Store holds the stale monitors the watchdog finds and when it last ran. The watchdog
// writes it; the API reads it for the health endpoint and the monitor list.
type Store interface {
	// All returns every stale monitor, keyed by monitor ID
	All(ctx context.Context) (map[string]Entry, error)
	// Put records a stale monitor, replacing any earlier record for it
	Put(ctx context.Context, e Entry) error
	// Delete removes the records of monitors that are no longer stale
	Delete(ctx context.Context, monitorIDs ...string) error
	// Count returns how many monitors are stale
	Count(ctx context.Context) (int64, error)
	// SetRanAt records when the watchdog last ran
	SetRanAt(ctx context.Context, t time.Time) error
	// RanAt returns when the watchdog last ran, or nil if it never has
	RanAt(ctx context.Context) (*time.Time, error)
}

// RedisStore keeps the stale monitors in Redis, so the api replicas share them
type RedisStore struct {
	rdb *redis.Client
}

var _ Store = (*RedisStore)(nil)

func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb}
}

// ProvideRedisStore provides the Redis store as the Store that the api replicas take
func ProvideRedisStore(rdb *redis.Client) Store {
	return NewRedisStore(rdb)
}

// All returns every stale monitor, keyed by monitor ID
func (s *RedisStore) All(ctx context.Context) (map[string]Entry, error) {
	raw, err := s.rdb.HGetAll(ctx, MonitorsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read stale monitors: %w", err)
	}
	entries := make(map[string]Entry, len(raw))
	for id, v := range raw {
		var e Entry
		if err := json.Unmarshal([]byte(v), &e); err != nil {
			continue
		}
		entries[id] = e
	}
	return entries, nil
}

// Put records a stale monitor, replacing any earlier record for it
func (s *RedisStore) Put(ctx context.Context, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.rdb.HSet(ctx, MonitorsKey, e.MonitorID, b).Err()
}

// Delete removes the records of monitors that are no longer stale
func (s *RedisStore) Delete(ctx context.Context, monitorIDs ...string) error {
	if len(monitorIDs) == 0 {
		return nil
	}
	return s.rdb.HDel(ctx, MonitorsKey, monitorIDs...).Err()
}

// Count returns how many monitors are stale
func (s *RedisStore) Count(ctx context.Context) (int64, error) {
	return s.rdb.HLen(ctx, MonitorsKey).Result()
}

// SetRanAt records when the watchdog last ran
func (s *RedisStore) SetRanAt(ctx context.Context, t time.Time) error {
	return s.rdb.Set(ctx, RanAtKey, t.UnixMilli(), 0).Err()
}

// RanAt returns when the watchdog last ran, or nil if it never has
func (s *RedisStore) RanAt(ctx context.Context) (*time.Time, error) {
	ms, err := s.rdb.Get(ctx, RanAtKey).Int64()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := time.UnixMilli(ms).UTC()
	return &t, nil
}

// MemoryStore keeps the stale monitors in the process, for single mode, where the watchdog
// and the API run together. It is safe for concurrent use.
type MemoryStore struct {
	mu    sync.RWMutex
	stale map[string]Entry
	ranAt *time.Time
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{stale: make(map[string]Entry)}
}

// ProvideMemoryStore provides the in-memory store as the Store that the watchdog and the API take
func ProvideMemoryStore() Store {
	return NewMemoryStore()
}

// All returns every stale monitor, keyed by monitor ID
func (s *MemoryStore) All(_ context.Context) (map[string]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.stale), nil
}

// Put records a stale monitor, replacing any earlier record for it
func (s *MemoryStore) Put(_ context.Context, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stale[e.MonitorID] = e
	return nil
}

// Delete removes the records of monitors that are no longer stale
func (s *MemoryStore) Delete(_ context.Context, monitorIDs ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range monitorIDs {
		delete(s.stale, id)
	}
	return nil
}

// Count returns how many monitors are stale
func (s *MemoryStore) Count(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.stale)), nil
}

// SetRanAt records when the watchdog last ran. Like the Redis store, it keeps milliseconds.
func (s *MemoryStore) SetRanAt(_ context.Context, t time.Time) error {
	t = t.UTC().Truncate(time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ranAt = &t
	return nil
}

// RanAt returns when the watchdog last ran, or nil if it never has
func (s *MemoryStore) RanAt(_ context.Context) (*time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ranAt == nil {
		return nil, nil
	}
	t := *s.ranAt
	return &t, nil
}
