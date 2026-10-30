package mongoimport

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// progressEvery is how many documents of a large collection are copied between lines of progress.
const progressEvery = 100000

func (c *copier) progressf(collection string, read int) {
	if c.progress != nil && read%progressEvery == 0 {
		fmt.Fprintf(c.progress, "%s: %d documents read\n", collection, read)
	}
}

type heartbeatRow struct {
	ID        string     `bun:"id"`
	MonitorID string     `bun:"monitor_id"`
	Status    int        `bun:"status"`
	Msg       string     `bun:"msg"`
	Ping      int        `bun:"ping"`
	Duration  int        `bun:"duration"`
	DownCount int        `bun:"down_count"`
	Retries   int        `bun:"retries"`
	Important bool       `bun:"important"`
	Time      time.Time  `bun:"time"`
	EndTime   *time.Time `bun:"end_time"`
	Notified  bool       `bun:"notified"`
}

// copyHeartbeats copies the heartbeats of the monitors that were copied. A
// heartbeat that never ended has no end time, which SQL keeps as NULL and
// MongoDB as the zero time.
func (c *copier) copyHeartbeats(ctx context.Context) error {
	report := c.report.collection("heartbeat", "heartbeats")
	out := newBatcher[heartbeatRow](c.tx, "heartbeats", report)
	err := c.src.each(ctx, "heartbeat", nil, func(d doc) error {
		report.Read++
		c.progressf("heartbeat", report.Read)
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		monitor, ok := d.objectID("monitor_id")
		if !ok || !c.monitors.has(monitor) {
			report.skip("monitor no longer exists")
			return nil
		}
		at, ok := d.when("time")
		if !ok {
			at = id.Timestamp().UTC()
			report.fix("time missing or zero: used the time the ObjectID was made")
		}
		return out.add(ctx, heartbeatRow{
			ID: uuidOf(id), MonitorID: uuidOf(monitor), Status: int(d.integer("status")), Msg: d.str("msg"), Ping: int(d.integer("ping")),
			Duration: int(d.integer("duration")), DownCount: int(d.integer("down_count")), Retries: int(d.integer("retries")),
			Important: d.boolean("important"), Time: at, EndTime: d.whenPtr("end_time"), Notified: d.boolean("notified"),
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type statRow struct {
	MonitorID   string    `bun:"monitor_id"`
	Timestamp   time.Time `bun:"timestamp"`
	Up          int       `bun:"up"`
	Down        int       `bun:"down"`
	Maintenance int       `bun:"maintenance"`
	PingSum     float64   `bun:"ping_sum"`
	PingCount   int       `bun:"ping_count"`
	PingMin     *float64  `bun:"ping_min"`
	PingMax     *float64  `bun:"ping_max"`
}

// add counts another document of the same bucket into the row.
func (r *statRow) add(o statRow) {
	r.Up += o.Up
	r.Down += o.Down
	r.Maintenance += o.Maintenance
	r.PingSum += o.PingSum
	r.PingCount += o.PingCount
	if o.PingMin != nil && (r.PingMin == nil || *o.PingMin < *r.PingMin) {
		r.PingMin = o.PingMin
	}
	if o.PingMax != nil && (r.PingMax == nil || *o.PingMax > *r.PingMax) {
		r.PingMax = o.PingMax
	}
}

// statFromDocument turns a MongoDB stat into a row of the SQL table. The two
// keep different numbers.
//
// MongoDB stores the average response time in ping, with the lowest and highest
// in ping_min and ping_max, and counts Maintenance checks inside up. Only Up
// checks have a response time, so the checks the average is over are up minus
// maintenance (the stats service reads them the same way: stats.toDomainModel).
// SQL stores the sum and the count: ping_count is that same count, and ping_sum
// is the average times the count. A bucket without response times has no
// minimum and maximum.
func statFromDocument(report *CollectionReport, d doc, monitor primitive.ObjectID, at time.Time) statRow {
	row := statRow{
		MonitorID: uuidOf(monitor), Timestamp: at,
		Up: int(d.integer("up")), Down: int(d.integer("down")), Maintenance: int(d.integer("maintenance")),
	}
	row.PingCount = row.Up - row.Maintenance
	if row.PingCount < 0 {
		report.fix("maintenance is more than up: ping_count set to 0")
		row.PingCount = 0
	}
	if row.PingCount > 0 {
		ping := d.float("ping")
		min, max := ping, ping
		if d.has("ping_min") {
			min = d.float("ping_min")
		}
		if d.has("ping_max") {
			max = d.float("ping_max")
		}
		row.PingSum = ping * float64(row.PingCount)
		row.PingMin, row.PingMax = &min, &max
	}
	return row
}

// copyStats copies the stats of one period. Daily stats outlive the heartbeats
// they were counted from, since heartbeats are cleaned up, so they are copied
// and not counted again. The documents are read in order of bucket, so that
// documents of the same monitor and time, which a race between the first two
// heartbeats of a bucket could leave, are next to each other and add up into
// one row.
func (c *copier) copyStats(ctx context.Context, collection string) error {
	report := c.report.collection(collection, collection)
	out := newBatcher[statRow](c.tx, collection, report)
	merger := &statMerger{out: out}
	err := c.src.each(ctx, collection, bson.D{{Key: "monitor_id", Value: 1}, {Key: "timestamp", Value: 1}}, func(d doc) error {
		report.Read++
		c.progressf(collection, report.Read)
		monitor, ok := d.objectID("monitor_id")
		if !ok || !c.monitors.has(monitor) {
			report.skip("monitor no longer exists")
			return nil
		}
		at, ok := d.when("timestamp")
		if !ok {
			report.skip("no timestamp")
			return nil
		}
		row := statFromDocument(report, d, monitor, at)
		if merger.merge(row, monitor, at) {
			report.skip("a document of the same monitor and time, merged into the one copied")
			return nil
		}
		return merger.hold(ctx, row, monitor, at)
	})
	if err != nil {
		return err
	}
	if err := merger.flush(ctx); err != nil {
		return err
	}
	return out.flush(ctx)
}

// statMerger holds the row last read, until a document of another monitor or time comes: the
// documents of the same monitor and time that follow it are added to it
type statMerger struct {
	out     *batcher[statRow]
	pending *statRow
	monitor primitive.ObjectID
	at      time.Time
}

// flush hands the held row, if any, to the batcher
func (m *statMerger) flush(ctx context.Context) error {
	if m.pending == nil {
		return nil
	}
	row := *m.pending
	m.pending = nil
	return m.out.add(ctx, row)
}

// merge adds row to the held row and reports true when it is of the same monitor and time
func (m *statMerger) merge(row statRow, monitor primitive.ObjectID, at time.Time) bool {
	if m.pending != nil && m.monitor == monitor && m.at.Equal(at) {
		m.pending.add(row)
		return true
	}
	return false
}

// hold flushes the held row and holds row in its place
func (m *statMerger) hold(ctx context.Context, row statRow, monitor primitive.ObjectID, at time.Time) error {
	if err := m.flush(ctx); err != nil {
		return err
	}
	m.pending, m.monitor, m.at = &row, monitor, at
	return nil
}

type tlsInfoRow struct {
	ID        string    `bun:"id"`
	MonitorID string    `bun:"monitor_id" max:"255"`
	InfoJSON  string    `bun:"info_json"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// copyTLSInfo copies the last TLS information of each monitor. Unlike the
// other links to a monitor, it refers to it by a string, which becomes the
// monitor's UUID.
//
// A monitor has one record, which each check replaces, and MongoDB's unique index on
// monitor_id keeps it so. If a collection has several for one monitor anyway, the one
// updated last is kept, since an older one holds a certificate that was renewed since.
// Records without an update time are told apart by their ObjectIDs, the later one being newer.
func (c *copier) copyTLSInfo(ctx context.Context) error {
	report := c.report.collection("monitor_tls_info", "monitor_tls_info")
	type candidate struct {
		d       doc
		id      primitive.ObjectID
		updated time.Time
	}
	newest := map[primitive.ObjectID]candidate{}
	var monitors []primitive.ObjectID
	err := c.src.each(ctx, "monitor_tls_info", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		monitor, ok := d.objectID("monitor_id")
		if !ok || !c.monitors.has(monitor) {
			report.skip("monitor no longer exists")
			return nil
		}
		updated, _ := d.when("updated_at")
		earlier, seen := newest[monitor]
		if !seen {
			monitors = append(monitors, monitor)
		} else {
			report.skip("an older TLS record of the same monitor")
			// The documents are read in ObjectID order, so this one is the later unless it was updated earlier
			if updated.Before(earlier.updated) {
				return nil
			}
		}
		newest[monitor] = candidate{d, id, updated}
		return nil
	})
	if err != nil {
		return err
	}
	out := newBatcher[tlsInfoRow](c.tx, "monitor_tls_info", report)
	for _, monitor := range monitors {
		winner := newest[monitor]
		created, updated := stamps(report, winner.d, winner.id, "created_at", "updated_at")
		if err := out.add(ctx, tlsInfoRow{ID: uuidOf(winner.id), MonitorID: uuidOf(monitor), InfoJSON: winner.d.str("info_json"), CreatedAt: created, UpdatedAt: updated}); err != nil {
			return err
		}
	}
	return out.flush(ctx)
}

type sentHistoryRow struct {
	ID        string    `bun:"id"`
	Type      string    `bun:"type" max:"50"`
	MonitorID string    `bun:"monitor_id" max:"255"`
	Days      int       `bun:"days"`
	CreatedAt time.Time `bun:"created_at"`
}

// copySentHistory copies which certificate expiry notifications were sent, so
// that they aren't sent again.
func (c *copier) copySentHistory(ctx context.Context) error {
	report := c.report.collection("notification_sent_history", "notification_sent_history")
	out := newBatcher[sentHistoryRow](c.tx, "notification_sent_history", report)
	type key struct {
		kind    string
		monitor primitive.ObjectID
		days    int
	}
	seen := map[key]bool{}
	err := c.src.each(ctx, "notification_sent_history", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		monitor, ok := d.objectID("monitor_id")
		if !ok || !c.monitors.has(monitor) {
			report.skip("monitor no longer exists")
			return nil
		}
		k := key{d.str("type"), monitor, int(d.integer("days"))}
		if seen[k] {
			report.skip("the same notification already copied")
			return nil
		}
		seen[k] = true
		created, ok := d.when("created_at")
		if !ok {
			created = id.Timestamp().UTC()
			report.fix("created_at missing or zero: used the time the ObjectID was made")
		}
		return out.add(ctx, sentHistoryRow{ID: uuidOf(id), Type: k.kind, MonitorID: uuidOf(monitor), Days: k.days, CreatedAt: created})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}
