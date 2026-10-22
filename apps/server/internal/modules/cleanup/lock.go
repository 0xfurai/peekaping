package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RunLockKeyPrefix starts the Redis key of each hour's claim, which ends in the hour in UTC,
// such as peekaping:cleanup:2026-10-22T12
const RunLockKeyPrefix = "peekaping:cleanup:"

// runLockTTL is how long a claim lasts. It outlasts the hour it is for, so that a replica
// whose clock is behind still finds the hour claimed.
const runLockTTL = 2 * time.Hour

// RunLock decides which Peekaping process runs the cleanup of an hour. Cluster mode keeps it
// in Redis, so that of the api replicas that fire at the top of the hour only one runs the
// cleanup; single mode has one process, which always runs it.
type RunLock interface {
	// Claim reports whether the caller runs the cleanup of the hour, which is the start of an
	// hour. It is true for the first caller of an hour and false for every later one. It fails
	// when the lock can't be reached, and then the caller must not run the cleanup.
	Claim(ctx context.Context, hour time.Time) (bool, error)
}

// RedisRunLock keeps the claims in Redis, so that the api replicas share them
type RedisRunLock struct {
	rdb *redis.Client
}

var _ RunLock = (*RedisRunLock)(nil)

func NewRedisRunLock(rdb *redis.Client) *RedisRunLock {
	return &RedisRunLock{rdb: rdb}
}

// ProvideRedisRunLock provides the Redis lock as the RunLock that the cleanup takes
func ProvideRedisRunLock(rdb *redis.Client) RunLock {
	return NewRedisRunLock(rdb)
}

// Claim reports whether the caller runs the cleanup of the hour. It sets the hour's key if it
// isn't set yet, and leaves it for the claim to expire: deleting it when the cleanup is done
// would let a replica whose clock lags claim the same hour again.
func (l *RedisRunLock) Claim(ctx context.Context, hour time.Time) (bool, error) {
	claimed, err := l.rdb.SetNX(ctx, runLockKey(hour), "1", runLockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("failed to claim the cleanup of %s: %w", hour.UTC().Format(time.RFC3339), err)
	}
	return claimed, nil
}

// runLockKey is the Redis key that holds the claim of an hour's cleanup
func runLockKey(hour time.Time) string {
	return RunLockKeyPrefix + hour.UTC().Format("2006-01-02T15")
}

// MemoryRunLock is the run lock of single mode, where the one process runs every hour's cleanup
type MemoryRunLock struct{}

var _ RunLock = MemoryRunLock{}

func NewMemoryRunLock() MemoryRunLock {
	return MemoryRunLock{}
}

// ProvideMemoryRunLock provides the in-process lock as the RunLock that the cleanup takes
func ProvideMemoryRunLock() RunLock {
	return NewMemoryRunLock()
}

// Claim always grants the run: nobody else could run it
func (MemoryRunLock) Claim(context.Context, time.Time) (bool, error) {
	return true, nil
}
