// Package redissync carries the admin's choice to the processes of a cluster. The api writes the
// choice to a Redis key, and every cluster process reads the key once a minute and turns error
// reporting on or off to match, so a change in Settings takes effect in the api, the producer, the
// worker and the ingester without a restart. The worker has no database, so the key is how it
// learns the setting.
package redissync

import (
	"context"
	"time"

	"peekaping/internal/reporting"

	"github.com/redis/go-redis/v9"
)

// Key is the Redis key that holds "true" or "false"
const Key = "peekaping:error_reporting:enabled"

// DefaultEvery is how often a process reads the key
const DefaultEvery = time.Minute

// Publish writes the choice for the cluster to read
func Publish(ctx context.Context, client *redis.Client, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	return client.Set(ctx, Key, value, 0).Err()
}

// Refresher reads the key and applies it to this process
type Refresher struct {
	client *redis.Client
	every  time.Duration
}

func NewRefresher(client *redis.Client, every time.Duration) *Refresher {
	return &Refresher{client: client, every: every}
}

// Refresh reads the key once. A missing key, an unknown value and a Redis error leave the process
// as it is, which is off when it starts: reporting never turns on for want of an answer.
func (r *Refresher) Refresh(ctx context.Context) {
	value, err := r.client.Get(ctx, Key).Result()
	if err != nil { // redis.Nil, a missing key, is one
		return
	}
	switch value {
	case "true":
		reporting.SetEnabled(true)
	case "false":
		reporting.SetEnabled(false)
	}
}

// Run reads the key now and then every interval until ctx is done
func (r *Refresher) Run(ctx context.Context) {
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()
	for {
		r.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
