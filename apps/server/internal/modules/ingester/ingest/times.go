package ingest

import "time"

// UTCSpan is the start and end of a check, in UTC. The executors keep the monotonic reading of
// their times, so end.Sub(start) is the time that passed even if the wall clock stepped during the
// check. UTC drops that reading, so the end is the start plus that difference: it can't come out
// before the start. A zero time stands for a time that was never set and passes through unchanged,
// since the difference to it means nothing.
func UTCSpan(start, end time.Time) (time.Time, time.Time) {
	if start.IsZero() || end.IsZero() {
		return start.UTC(), end.UTC()
	}
	duration := end.Sub(start)
	start = start.UTC()
	return start, start.Add(duration)
}
