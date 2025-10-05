package producer

import (
	"fmt"
	"time"
)

// nextAligned calculates the next aligned time based on interval
func nextAligned(after time.Time, period time.Duration) time.Time {
	ms := after.UnixMilli()
	p := period.Milliseconds()
	return time.UnixMilli(((ms / p) + 1) * p).UTC()
}

// redisNowMs returns the current time in milliseconds from Redis
func (p *Producer) redisNowMs() int64 {
	// Prefer Redis TIME to keep a single clock for all producers
	t, err := p.rdb.Time(p.ctx).Result()
	if err != nil {
		p.logger.Warnw("Failed to get Redis time, using local time", "error", err)
		return time.Now().UTC().UnixMilli()
	}
	return t.UnixMilli()
}

// toStringSlice converts Redis result to string slice
func toStringSlice(v any) []string {
	as, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(as))
	for _, x := range as {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case []byte:
			out = append(out, string(t))
		default:
			out = append(out, fmt.Sprint(t))
		}
	}
	return out
}
