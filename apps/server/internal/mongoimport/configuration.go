package mongoimport

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type proxyRow struct {
	ID          string    `bun:"id"`
	WorkspaceID string    `bun:"workspace_id"`
	Protocol    string    `bun:"protocol" max:"10"`
	Host        string    `bun:"host" max:"255"`
	Port        int       `bun:"port"`
	Auth        bool      `bun:"auth"`
	Username    string    `bun:"username" max:"255"`
	Password    string    `bun:"password" max:"255"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

func (c *copier) copyProxies(ctx context.Context) error {
	report := c.report.collection("proxies", "proxies")
	out := newBatcher[proxyRow](c.tx, "proxies", report)
	err := c.src.each(ctx, "proxies", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		c.proxies.add(id)
		return out.add(ctx, proxyRow{
			ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Protocol: d.str("protocol"), Host: d.str("host"), Port: int(d.integer("port")), Auth: d.boolean("auth"),
			Username: d.str("username"), Password: d.str("password"), CreatedAt: created, UpdatedAt: updated,
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type monitorRow struct {
	ID             string    `bun:"id"`
	WorkspaceID    string    `bun:"workspace_id"`
	Type           string    `bun:"type" max:"20"`
	Name           string    `bun:"name" max:"150"`
	Interval       int       `bun:"interval"`
	Timeout        int       `bun:"timeout"`
	MaxRetries     int       `bun:"max_retries"`
	RetryInterval  int       `bun:"retry_interval"`
	ResendInterval int       `bun:"resend_interval"`
	Active         bool      `bun:"active"`
	Status         int       `bun:"status"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
	Config         string    `bun:"config"`
	ProxyID        *string   `bun:"proxy_id"`
	PushToken      *string   `bun:"push_token" max:"255"` // NULL for a monitor without one: the column is unique
}

// jsonConfig returns a config that PostgreSQL's JSONB column accepts. An empty
// or missing config, which the monitors and channels MongoDB holds can have,
// becomes {}. A config that isn't JSON can't be kept as it is on PostgreSQL, so
// it becomes {} too, and the report says so. JSONB also rejects the escape
// \u0000, which is valid JSON, so the escapes are removed, from SQLite's configs
// too, so that both databases hold the same data.
func jsonConfig(report *CollectionReport, config *string) string {
	switch {
	case config == nil:
		report.fix("config is missing: used {}")
	case *config == "":
		report.fix("config is empty: used {}")
	case !json.Valid([]byte(*config)):
		report.fix("config is not valid JSON: used {}")
	default:
		stripped, changed := removeNULEscapes(*config)
		if changed {
			report.fix(`config had \u0000 escapes, which PostgreSQL rejects: removed`)
		}
		return stripped
	}
	return "{}"
}

// removeNULEscapes removes the escape \u0000 from JSON text. A backslash escapes the
// character after it, so in \\u0000, which is a backslash and the text u0000, nothing is removed.
func removeNULEscapes(text string) (string, bool) {
	if !strings.Contains(text, `\u0000`) {
		return text, false
	}
	var without strings.Builder
	changed := false
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			if text[i+1] == 'u' && strings.HasPrefix(text[i+2:], "0000") {
				i += 5
				changed = true
				continue
			}
			without.WriteByte(text[i])
			i++
		}
		without.WriteByte(text[i])
	}
	return without.String(), changed
}

// pushToken is the token to store for a monitor, NULL for none. SQL keeps push tokens unique, and
// MongoDB did not: a monitor without one has an empty string, and two monitors may hold the same
// token. The first monitor keeps a token, and a later one with the same token goes without, which
// the report counts. A push to a shared token reached just one of them anyway.
//
// A push monitor made through the API once held its token in its config only, with an empty
// push_token. The upgrade fills push_token from config.pushToken for such a monitor (migration
// 20261101120300), and so does the import: pushToken is the document's push_token, or else, for a
// push monitor, the one in its config.
func (c *copier) pushToken(report *CollectionReport, typ, token, config string) *string {
	if token == "" && typ == "push" {
		var parsed struct {
			PushToken string `json:"pushToken"`
		}
		// A config that isn't an object of this shape has no token
		if json.Unmarshal([]byte(config), &parsed) == nil && parsed.PushToken != "" {
			token = parsed.PushToken
			report.fix("push_token is empty: used the one in the config")
		}
	}
	if token == "" {
		return nil
	}
	// PostgreSQL cuts the token to the width of its column, so two tokens that differ only after it
	// are one token there: the duplicate check is on the cut token, which is also the one stored
	const width = 255 // the max tag of monitorRow.PushToken
	cut := false
	if c.cutsText && utf8.RuneCountInString(token) > width {
		token = string([]rune(token)[:width])
		cut = true
	}
	if c.pushTokens[token] {
		report.fix("push_token is held by an earlier monitor: cleared")
		return nil
	}
	c.pushTokens[token] = true
	if cut {
		report.fix("push_token cut to 255 characters")
	}
	return &token
}

func (c *copier) copyMonitors(ctx context.Context) error {
	report := c.report.collection("monitor", "monitors")
	out := newBatcher[monitorRow](c.tx, "monitors", report)
	err := c.src.each(ctx, "monitor", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		created, updated := stamps(report, d, id, "created_at", "updated_at")

		// A proxy that was deleted leaves its monitors pointing at it; SQL clears the reference
		var proxyID *string
		if proxy, ok := d.objectID("proxy_id"); ok {
			if c.proxies.has(proxy) {
				mapped := uuidOf(proxy)
				proxyID = &mapped
			} else {
				report.fix("proxy_id names a proxy that no longer exists: cleared")
			}
		}
		c.monitors.add(id)
		config := jsonConfig(report, d.strPtr("config"))
		return out.add(ctx, monitorRow{
			ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Type: d.str("type"), Name: d.str("name"),
			Interval: int(d.integer("interval")), Timeout: int(d.integer("timeout")), MaxRetries: int(d.integer("max_retries")),
			RetryInterval: int(d.integer("retry_interval")), ResendInterval: int(d.integer("resend_interval")),
			Active: d.boolean("active"), Status: int(d.integer("status")), CreatedAt: created, UpdatedAt: updated,
			Config: config, ProxyID: proxyID, PushToken: c.pushToken(report, d.str("type"), d.str("push_token"), config),
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type channelRow struct {
	WorkspaceID string    `bun:"workspace_id"`
	ID          string    `bun:"id"`
	Name        string    `bun:"name" max:"255"`
	Type        string    `bun:"type" max:"50"`
	Active      bool      `bun:"active"`
	IsDefault   bool      `bun:"is_default"`
	Config      string    `bun:"config"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// copyChannels copies the notification channels. A full update of a channel in
// MongoDB writes the model with no field tags, so it sets name, type, active
// and config, but writes is_default and updated_at under the keys isdefault and
// updatedat, and the channel keeps showing the old values. The update is what
// the user last saved, so when a channel has those keys and they are no older
// than updated_at, they are its is_default and updated_at.
func (c *copier) copyChannels(ctx context.Context) error {
	report := c.report.collection("notification_channel", "notification_channels")
	out := newBatcher[channelRow](c.tx, "notification_channels", report)
	err := c.src.each(ctx, "notification_channel", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		isDefault := d.boolean("is_default")
		if saved, ok := d.when("updatedat"); ok && !saved.Before(updated) {
			updated = saved
			isDefault = d.boolean("isdefault")
			report.fix("is_default and updated_at taken from the keys of a full update")
		}
		c.channels.add(id)
		return out.add(ctx, channelRow{
			ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Name: d.str("name"), Type: d.str("type"), Active: d.boolean("active"), IsDefault: isDefault,
			Config: jsonConfig(report, d.strPtr("config")), CreatedAt: created, UpdatedAt: updated,
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type tagRow struct {
	ID          string    `bun:"id"`
	WorkspaceID string    `bun:"workspace_id"`
	Name        string    `bun:"name" max:"100"`
	Color       string    `bun:"color" max:"7"`
	Description *string   `bun:"description"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

func (c *copier) copyTags(ctx context.Context) error {
	report := c.report.collection("tags", "tags")
	out := newBatcher[tagRow](c.tx, "tags", report)
	names, stored := map[string]bool{}, map[string]bool{}
	err := c.src.each(ctx, "tags", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		name := d.str("name")
		if names[name] {
			report.skip("name already used by an earlier tag")
			return nil
		}
		names[name] = true
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		c.tags.add(id)
		return out.add(ctx, tagRow{ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Name: c.uniqueText(report, "name", stored, name, 100), Color: d.str("color"), Description: d.strPtr("description"), CreatedAt: created, UpdatedAt: updated})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type maintenanceRow struct {
	ID            string    `bun:"id"`
	WorkspaceID   string    `bun:"workspace_id"`
	Title         string    `bun:"title" max:"255"`
	Description   string    `bun:"description"`
	Active        bool      `bun:"active"`
	Strategy      string    `bun:"strategy" max:"50"`
	StartDateTime *string   `bun:"start_date_time" max:"50"`
	EndDateTime   *string   `bun:"end_date_time" max:"50"`
	StartTime     *string   `bun:"start_time" max:"50"`
	EndTime       *string   `bun:"end_time" max:"50"`
	Weekdays      string    `bun:"weekdays"`
	DaysOfMonth   string    `bun:"days_of_month"`
	IntervalDay   *int      `bun:"interval_day"`
	Cron          *string   `bun:"cron" max:"255"`
	Timezone      *string   `bun:"timezone" max:"100"`
	Duration      *int      `bun:"duration"`
	CreatedAt     time.Time `bun:"created_at"`
	UpdatedAt     time.Time `bun:"updated_at"`
}

// intsJSON stores a list of numbers the way the SQL repository does, as JSON text.
func intsJSON(values []int) string {
	text, _ := json.Marshal(values)
	return string(text)
}

// copyMaintenance copies the maintenance windows. MongoDB keeps their weekdays
// and days of the month as arrays, and SQL as JSON text. A full update of a
// window leaves created_at zero, and a partial update or switching one on or
// off writes updated_at as an RFC 3339 string, which stamps reads.
func (c *copier) copyMaintenance(ctx context.Context) error {
	report := c.report.collection("maintenance", "maintenances")
	out := newBatcher[maintenanceRow](c.tx, "maintenances", report)
	err := c.src.each(ctx, "maintenance", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		created, updated := stamps(report, d, id, "created_at", "updated_at")
		c.maintenances.add(id)
		return out.add(ctx, maintenanceRow{
			ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Title: d.str("title"), Description: d.str("description"), Active: d.boolean("active"), Strategy: d.str("strategy"),
			StartDateTime: d.strPtr("start_date_time"), EndDateTime: d.strPtr("end_date_time"),
			StartTime: d.strPtr("start_time"), EndTime: d.strPtr("end_time"),
			Weekdays: intsJSON(d.ints("weekdays")), DaysOfMonth: intsJSON(d.ints("days_of_month")),
			IntervalDay: d.intPtr("interval_day"), Cron: d.strPtr("cron"), Timezone: d.strPtr("timezone"), Duration: d.intPtr("duration"),
			CreatedAt: created, UpdatedAt: updated,
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type statusPageRow struct {
	ID                  string    `bun:"id"`
	WorkspaceID         string    `bun:"workspace_id"`
	Slug                string    `bun:"slug" max:"255"`
	Title               string    `bun:"title" max:"255"`
	Description         string    `bun:"description"`
	Icon                string    `bun:"icon" max:"255"`
	Theme               string    `bun:"theme" max:"30"`
	Published           bool      `bun:"published"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
	FooterText          string    `bun:"footer_text"`
	AutoRefreshInterval int       `bun:"auto_refresh_interval"`
}

// statusPage is what the links to a status page need to report about it.
type statusPage struct {
	id   primitive.ObjectID
	slug string
	// unused names the fields of the MongoDB document that SQL has no column for and that hold something
	unused []string
}

// copyStatusPages copies the status pages. The MongoDB documents also have
// search_engine_index, password and google_analytics_tag_id, which the app
// never reads and SQL has no column for; the report names the pages that set them.
func (c *copier) copyStatusPages(ctx context.Context) error {
	report := c.report.collection("status_pages", "status_pages")
	out := newBatcher[statusPageRow](c.tx, "status_pages", report)
	slugs, stored := map[string]bool{}, map[string]bool{}
	err := c.src.each(ctx, "status_pages", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		slug := d.str("slug")
		if slugs[slug] {
			report.skip("slug already used by an earlier status page")
			return nil
		}
		slugs[slug] = true
		created, updated := stamps(report, d, id, "created_at", "updated_at")

		slug = c.uniqueText(report, "slug", stored, slug, 255)
		page := &statusPage{id: id, slug: slug}
		if d.boolean("search_engine_index") {
			page.unused = append(page.unused, "search_engine_index")
		}
		if d.str("password") != "" {
			page.unused = append(page.unused, "password")
		}
		if d.str("google_analytics_tag_id") != "" {
			page.unused = append(page.unused, "google_analytics_tag_id")
		}
		c.statusPages[id] = page
		c.statusPageOrder = append(c.statusPageOrder, page)
		return out.add(ctx, statusPageRow{
			ID: uuidOf(id), WorkspaceID: string(c.workspaceID), Slug: slug, Title: d.str("title"), Description: d.str("description"), Icon: d.str("icon"), Theme: d.str("theme"),
			Published: d.boolean("published"), CreatedAt: created, UpdatedAt: updated, FooterText: d.str("footer_text"),
			AutoRefreshInterval: int(d.integer("auto_refresh_interval")),
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}
