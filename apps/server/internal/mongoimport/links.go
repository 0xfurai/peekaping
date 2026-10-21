package mongoimport

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// A link collection joins monitors to another kind of document. MongoDB has no
// cascading deletes, so deleting a monitor, channel, tag, maintenance window or
// status page leaves its links behind, and SQL's foreign keys reject them. A
// link is copied when both of its ends were.
type linkSpec[T any] struct {
	collection, table string
	// otherKey is the field of the link that refers to the other end, which is one of other.
	otherKey string
	other    idSet
	// otherGone is the reason to give when the other end isn't there.
	otherGone string
	row       func(id, monitor, other string, created, updated time.Time) T
	// seen, if set, is called with every link that is copied.
	seen func(d doc, other primitive.ObjectID)
}

func copyLinks[T any](ctx context.Context, c *copier, spec linkSpec[T]) error {
	report := c.report.collection(spec.collection, spec.table)
	out := newBatcher[T](c.tx, spec.table, report)
	pairs := map[[2]primitive.ObjectID]bool{}
	err := c.src.each(ctx, spec.collection, byID, func(d doc) error {
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
		other, ok := d.objectID(spec.otherKey)
		if !ok || !spec.other.has(other) {
			report.skip(spec.otherGone)
			return nil
		}
		pair := [2]primitive.ObjectID{monitor, other}
		if pairs[pair] {
			report.skip("a link between the same two documents already copied")
			return nil
		}
		pairs[pair] = true
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		if spec.seen != nil {
			spec.seen(d, other)
		}
		return out.add(ctx, spec.row(uuidOf(id), uuidOf(monitor), uuidOf(other), created, updated))
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type monitorNotificationRow struct {
	ID                    string    `bun:"id"`
	MonitorID             string    `bun:"monitor_id"`
	NotificationChannelID string    `bun:"notification_channel_id"`
	CreatedAt             time.Time `bun:"created_at"`
	UpdatedAt             time.Time `bun:"updated_at"`
}

// copyMonitorNotifications copies the links between monitors and their notification
// channels. The channel is named notification_id in MongoDB.
func (c *copier) copyMonitorNotifications(ctx context.Context) error {
	return copyLinks(ctx, c, linkSpec[monitorNotificationRow]{
		collection: "monitor_notification", table: "monitor_notifications",
		otherKey: "notification_id", other: c.channels, otherGone: "notification channel no longer exists",
		row: func(id, monitor, other string, created, updated time.Time) monitorNotificationRow {
			return monitorNotificationRow{id, monitor, other, created, updated}
		},
	})
}

type monitorTagRow struct {
	ID        string    `bun:"id"`
	MonitorID string    `bun:"monitor_id"`
	TagID     string    `bun:"tag_id"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

func (c *copier) copyMonitorTags(ctx context.Context) error {
	return copyLinks(ctx, c, linkSpec[monitorTagRow]{
		collection: "monitor_tags", table: "monitor_tags",
		otherKey: "tag_id", other: c.tags, otherGone: "tag no longer exists",
		row: func(id, monitor, other string, created, updated time.Time) monitorTagRow {
			return monitorTagRow{id, monitor, other, created, updated}
		},
	})
}

type monitorMaintenanceRow struct {
	ID            string    `bun:"id"`
	MonitorID     string    `bun:"monitor_id"`
	MaintenanceID string    `bun:"maintenance_id"`
	CreatedAt     time.Time `bun:"created_at"`
	UpdatedAt     time.Time `bun:"updated_at"`
}

func (c *copier) copyMonitorMaintenances(ctx context.Context) error {
	return copyLinks(ctx, c, linkSpec[monitorMaintenanceRow]{
		collection: "monitor_maintenance", table: "monitor_maintenances",
		otherKey: "maintenance_id", other: c.maintenances, otherGone: "maintenance window no longer exists",
		row: func(id, monitor, other string, created, updated time.Time) monitorMaintenanceRow {
			return monitorMaintenanceRow{id, monitor, other, created, updated}
		},
	})
}

type monitorStatusPageRow struct {
	ID           string    `bun:"id"`
	MonitorID    string    `bun:"monitor_id"`
	StatusPageID string    `bun:"status_page_id"`
	CreatedAt    time.Time `bun:"created_at"`
	UpdatedAt    time.Time `bun:"updated_at"`
}

// pageLinks is what the links to a status page held that SQL has no column for.
type pageLinks struct {
	orders map[int64]bool
	count  int
	hidden int
}

// copyMonitorStatusPages copies the links between status pages and their monitors.
// MongoDB also keeps, for each link, the monitor's position on the page and
// whether it's shown. SQL keeps neither, so the report says which pages had a
// position or a hidden monitor.
func (c *copier) copyMonitorStatusPages(ctx context.Context) error {
	links := map[primitive.ObjectID]*pageLinks{}
	err := copyLinks(ctx, c, linkSpec[monitorStatusPageRow]{
		collection: "monitor_status_page", table: "monitor_status_pages",
		otherKey: "status_page_id", other: c.statusPageIDs(), otherGone: "status page no longer exists",
		row: func(id, monitor, other string, created, updated time.Time) monitorStatusPageRow {
			return monitorStatusPageRow{id, monitor, other, created, updated}
		},
		seen: func(d doc, page primitive.ObjectID) {
			l := links[page]
			if l == nil {
				l = &pageLinks{orders: map[int64]bool{}}
				links[page] = l
			}
			l.count++
			l.orders[d.integer("order")] = true
			if !d.boolean("active") {
				l.hidden++
			}
		},
	})
	if err != nil {
		return err
	}
	for _, page := range c.statusPageOrder {
		if l := links[page.id]; l != nil && l.count > 1 && len(l.orders) > 1 {
			c.report.note("status page %q: the order of its monitors is not kept (SQL has no column for it)", page.slug)
		}
		if l := links[page.id]; l != nil && l.hidden > 0 {
			verb := "is"
			noun := "monitor"
			if l.hidden != 1 {
				verb, noun = "are", "monitors"
			}
			c.report.note("status page %q: %d %s %s hidden on the page (active is false) and will show (SQL has no column for it)", page.slug, l.hidden, noun, verb)
		}
		if len(page.unused) > 0 {
			verb := "is"
			if len(page.unused) > 1 {
				verb = "are"
			}
			c.report.note("status page %q: %s %s not kept (Peekaping does not use them)", page.slug, joinAnd(page.unused), verb)
		}
	}
	return nil
}

func (c *copier) statusPageIDs() idSet {
	ids := idSet{}
	for id := range c.statusPages {
		ids.add(id)
	}
	return ids
}

// joinAnd lists names the way a sentence does: "a", "a and b", "a, b and c".
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	list := ""
	for i, name := range names[:len(names)-1] {
		if i > 0 {
			list += ", "
		}
		list += name
	}
	return fmt.Sprintf("%s and %s", list, names[len(names)-1])
}

type domainRow struct {
	ID           string    `bun:"id"`
	StatusPageID string    `bun:"status_page_id"`
	Domain       string    `bun:"domain"`
	CreatedAt    time.Time `bun:"created_at"`
	UpdatedAt    time.Time `bun:"updated_at"`
}

// copyDomains copies the custom domains of the status pages.
func (c *copier) copyDomains(ctx context.Context) error {
	report := c.report.collection("domain_status_page", "domain_status_page")
	out := newBatcher[domainRow](c.tx, "domain_status_page", report)
	seen := map[[2]string]bool{}
	err := c.src.each(ctx, "domain_status_page", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		page, ok := d.objectID("status_page_id")
		if _, exists := c.statusPages[page]; !ok || !exists {
			report.skip("status page no longer exists")
			return nil
		}
		key := [2]string{page.Hex(), d.str("domain")}
		if seen[key] {
			report.skip("the domain of this status page already copied")
			return nil
		}
		seen[key] = true
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		return out.add(ctx, domainRow{ID: uuidOf(id), StatusPageID: uuidOf(page), Domain: d.str("domain"), CreatedAt: created, UpdatedAt: updated})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}
