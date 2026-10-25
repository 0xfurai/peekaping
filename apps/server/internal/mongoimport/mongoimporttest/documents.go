package mongoimporttest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// The types in this file are the frozen MongoDB storage format that "bun db import-mongo" reads:
// one struct per collection, with the bson tags of the model that the module's MongoDB repository
// kept for it when the storage backend was frozen (ADR 0001), and the keys that the repository's
// updates and the services added to it. The repositories are being removed, but the installs they
// filled are not, so the seed writes these documents itself and the tests read them back with the
// same types. They are copies on purpose: a change to a module must not change the format of the
// installs the import has to read.
//
// A field with omitempty is left out of the document when empty, as the repository left out the
// key. A zero time without it is written as a date in the year 1, as the repository wrote it.

// UserDocument is a document of the "users" collection.
type UserDocument struct {
	ID             primitive.ObjectID `bson:"_id"`
	Email          string             `bson:"email"`
	Password       string             `bson:"password"`
	Active         bool               `bson:"active"`
	TwoFASecret    string             `bson:"twofa_secret"`
	TwoFAStatus    bool               `bson:"twofa_status"`
	TwoFALastToken string             `bson:"twofa_last_token"`
	CreatedAt      time.Time          `bson:"createdAt"`
	UpdatedAt      time.Time          `bson:"updatedAt"`
}

// SettingDocument is a document of the "setting" collection. A setting is upserted by its key,
// and the upsert writes key, value, type and updated_at only, so a setting nobody created
// with CreateIfAbsent has no created_at.
type SettingDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	Key       string             `bson:"key"`
	Value     string             `bson:"value"`
	Type      string             `bson:"type"`
	CreatedAt time.Time          `bson:"created_at,omitempty"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// APIKeyDocument is a document of the "api_keys" collection. The usage columns are null until the
// key has been used.
type APIKeyDocument struct {
	ID            primitive.ObjectID `bson:"_id"`
	Name          string             `bson:"name"`
	KeyHash       string             `bson:"key_hash"`
	DisplayKey    string             `bson:"display_key"`
	LastUsed      *time.Time         `bson:"last_used"`
	ExpiresAt     *time.Time         `bson:"expires_at"`
	UsageCount    int64              `bson:"usage_count"`
	MaxUsageCount *int64             `bson:"max_usage_count"`
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

// ProxyDocument is a document of the "proxies" collection.
type ProxyDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	Protocol  string             `bson:"protocol"`
	Host      string             `bson:"host"`
	Port      int                `bson:"port"`
	Auth      bool               `bson:"auth"`
	Username  string             `bson:"username,omitempty"`
	Password  string             `bson:"password,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// MonitorDocument is a document of the "monitor" collection. Status is a heartbeat status: 0
// down, 1 up, 2 pending, 3 maintenance.
type MonitorDocument struct {
	ID             primitive.ObjectID  `bson:"_id"`
	Type           string              `bson:"type"`
	Name           string              `bson:"name"`
	Interval       int                 `bson:"interval"`
	Timeout        int                 `bson:"timeout"`
	MaxRetries     int                 `bson:"max_retries"`
	RetryInterval  int                 `bson:"retry_interval"`
	ResendInterval int                 `bson:"resend_interval"`
	Active         bool                `bson:"active"`
	Status         int                 `bson:"status"`
	CreatedAt      time.Time           `bson:"created_at"`
	UpdatedAt      time.Time           `bson:"updated_at"`
	Config         string              `bson:"config"`
	ProxyID        *primitive.ObjectID `bson:"proxy_id,omitempty"`
	PushToken      string              `bson:"push_token"`
}

// NotificationChannelDocument is a document of the "notification_channel" collection.
type NotificationChannelDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	Name      string             `bson:"name"`
	Type      string             `bson:"type"`
	Active    bool               `bson:"active"`
	IsDefault bool               `bson:"is_default"`
	Config    *string            `bson:"config,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// TagDocument is a document of the "tags" collection.
type TagDocument struct {
	ID          primitive.ObjectID `bson:"_id"`
	Name        string             `bson:"name"`
	Color       string             `bson:"color"`
	Description *string            `bson:"description,omitempty"`
	CreatedAt   time.Time          `bson:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at"`
}

// MonitorNotificationDocument is a document of the "monitor_notification" collection, which links
// a monitor to a notification channel.
type MonitorNotificationDocument struct {
	ID             primitive.ObjectID `bson:"_id"`
	MonitorID      primitive.ObjectID `bson:"monitor_id"`
	NotificationID primitive.ObjectID `bson:"notification_id"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
}

// MonitorTagDocument is a document of the "monitor_tags" collection, which links a monitor to a tag.
type MonitorTagDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	MonitorID primitive.ObjectID `bson:"monitor_id"`
	TagID     primitive.ObjectID `bson:"tag_id"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// MaintenanceDocument is a document of the "maintenance" collection. A full update $sets the whole
// document, which leaves created_at zero, and a partial update or switching a window on or off
// writes updated_at as an RFC 3339 string, which the document's UpdatedAt can't hold: the
// seed writes that string itself.
type MaintenanceDocument struct {
	ID            primitive.ObjectID `bson:"_id"`
	Title         string             `bson:"title"`
	Description   string             `bson:"description"`
	Active        bool               `bson:"active"`
	Strategy      string             `bson:"strategy"`
	StartDateTime *string            `bson:"start_date_time,omitempty"`
	EndDateTime   *string            `bson:"end_date_time,omitempty"`
	StartTime     *string            `bson:"start_time,omitempty"`
	EndTime       *string            `bson:"end_time,omitempty"`
	Weekdays      []int              `bson:"weekdays,omitempty"`
	DaysOfMonth   []int              `bson:"days_of_month,omitempty"`
	IntervalDay   *int               `bson:"interval_day,omitempty"`
	Cron          *string            `bson:"cron,omitempty"`
	Timezone      *string            `bson:"timezone,omitempty"`
	Duration      *int               `bson:"duration,omitempty"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
}

// MonitorMaintenanceDocument is a document of the "monitor_maintenance" collection, which links a
// monitor to a maintenance window.
type MonitorMaintenanceDocument struct {
	ID            primitive.ObjectID `bson:"_id"`
	MonitorID     primitive.ObjectID `bson:"monitor_id"`
	MaintenanceID primitive.ObjectID `bson:"maintenance_id"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
}

// StatusPageDocument is a document of the "status_pages" collection. The repository creates a page
// with search_engine_index false and no password or Google Analytics tag, which only MongoDB keeps,
// and which a later update of the document can set.
type StatusPageDocument struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty"`
	Slug                 string             `bson:"slug"`
	Title                string             `bson:"title"`
	Description          string             `bson:"description"`
	Icon                 string             `bson:"icon"`
	Theme                string             `bson:"theme"`
	Published            bool               `bson:"published"`
	SearchEngineIndex    bool               `bson:"search_engine_index"`
	Password             string             `bson:"password,omitempty"`
	FooterText           string             `bson:"footer_text"`
	GoogleAnalyticsTagID string             `bson:"google_analytics_tag_id"`
	AutoRefreshInterval  int                `bson:"auto_refresh_interval"`
	CreatedAt            time.Time          `bson:"created_at"`
	UpdatedAt            time.Time          `bson:"updated_at"`
}

// MonitorStatusPageDocument is a document of the "monitor_status_page" collection, which places a
// monitor on a status page, at a position (order), shown or hidden (active). A full update leaves
// created_at zero.
type MonitorStatusPageDocument struct {
	ID           primitive.ObjectID `bson:"_id"`
	StatusPageID primitive.ObjectID `bson:"status_page_id"`
	MonitorID    primitive.ObjectID `bson:"monitor_id"`
	Order        int                `bson:"order"`
	Active       bool               `bson:"active"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

// DomainStatusPageDocument is a document of the "domain_status_page" collection, which gives a
// status page a domain.
type DomainStatusPageDocument struct {
	ID           primitive.ObjectID `bson:"_id"`
	StatusPageID primitive.ObjectID `bson:"status_page_id"`
	Domain       string             `bson:"domain"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

// HeartbeatDocument is a document of the "heartbeat" collection. Time and EndTime are written even
// when zero. Status is a heartbeat status, as in MonitorDocument.
type HeartbeatDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	MonitorID primitive.ObjectID `bson:"monitor_id"`
	Status    int                `bson:"status"`
	Msg       string             `bson:"msg"`
	Ping      int                `bson:"ping"`
	Duration  int                `bson:"duration"`
	DownCount int                `bson:"down_count"`
	Retries   int                `bson:"retries"`
	Important bool               `bson:"important"`
	Time      time.Time          `bson:"time"`
	EndTime   time.Time          `bson:"end_time"`
	Notified  bool               `bson:"notified"`
}

// StatDocument is a document of the "stat_minutely", "stat_hourly" and "stat_daily" collections:
// the checks of one monitor in one minute, hour or day, which the stats service adds up. Up also
// counts the Maintenance checks, which have no response time. Ping is the average response time
// of the other Up checks, and PingMin and PingMax their extremes; a bucket without such a check
// has no ping keys.
type StatDocument struct {
	ID          primitive.ObjectID `bson:"_id"`
	MonitorID   primitive.ObjectID `bson:"monitor_id"`
	Timestamp   time.Time          `bson:"timestamp"`
	Ping        float64            `bson:"ping,omitempty"`
	PingMin     float64            `bson:"ping_min,omitempty"`
	PingMax     float64            `bson:"ping_max,omitempty"`
	Up          int                `bson:"up"`
	Down        int                `bson:"down"`
	Maintenance int                `bson:"maintenance"`
}

// PingCount is how many checks the document's response time is the average of.
func (d StatDocument) PingCount() int { return d.Up - d.Maintenance }

// MonitorTLSInfoDocument is a document of the "monitor_tls_info" collection. It refers to its
// monitor by the hex form of the ObjectID, as a string.
type MonitorTLSInfoDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	MonitorID string             `bson:"monitor_id"`
	InfoJSON  string             `bson:"info_json"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// NotificationSentDocument is a document of the "notification_sent_history" collection, which
// records the certificate expiry notifications that were sent. It refers to its monitor by the
// hex form of the ObjectID, as a string.
type NotificationSentDocument struct {
	ID        primitive.ObjectID `bson:"_id"`
	Type      string             `bson:"type"`
	MonitorID string             `bson:"monitor_id"`
	Days      int                `bson:"days"`
	CreatedAt time.Time          `bson:"created_at"`
}

// Get decodes the first document of a collection that matches filter, and fails the test if there
// is none.
func Get[T any](t *testing.T, in *Install, collection string, filter bson.M) T {
	t.Helper()
	var document T
	require.NoError(t, in.Collection(collection).FindOne(t.Context(), filter).Decode(&document), "%s %v", collection, filter)
	return document
}

// GetByID decodes the document of a collection with the given ObjectID, in hex.
func GetByID[T any](t *testing.T, in *Install, collection, objectID string) T {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(objectID)
	require.NoError(t, err)
	return Get[T](t, in, collection, bson.M{"_id": id})
}

// List decodes the documents of a collection that match filter, in the order of sort (the order
// they were made, when sort is empty).
func List[T any](t *testing.T, in *Install, collection string, filter bson.M, sort bson.D) []T {
	t.Helper()
	if len(sort) == 0 {
		sort = bson.D{{Key: "_id", Value: 1}}
	}
	cursor, err := in.Collection(collection).Find(t.Context(), filter, options.Find().SetSort(sort))
	require.NoError(t, err)
	var documents []T
	require.NoError(t, cursor.All(t.Context(), &documents))
	return documents
}
