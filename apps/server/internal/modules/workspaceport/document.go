// Package workspaceport exports a workspace's setup as a versioned JSON document, and its
// heartbeat history as a file of its own (PP-170). The import reads the same document.
//
// The records of a document point to each other by ref, not by database ID: a ref is the source
// record's ID as a string, and only means something inside its document. A document holds no ID of
// a database, no workspace ID, no timestamp of a record's own and no run state (a monitor's status
// or last check). It holds saved secrets in full.
package workspaceport

import (
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

const (
	// FormatName is the format of a workspace document
	FormatName = "peekaping-workspace"
	// Version is the version of the workspace document that this server writes and reads
	Version = 1
	// HeartbeatsFormatName is the format of the first line of a heartbeat file
	HeartbeatsFormatName = "peekaping-heartbeats"
	// HeartbeatsVersion is the version of the heartbeat file that this server writes
	HeartbeatsVersion = 1
)

// ConfigJSON is the config of a monitor or a notification channel: a JSON object, which the
// document holds as an object and the database holds as a string. An empty one is {}.
type ConfigJSON json.RawMessage

func (c ConfigJSON) MarshalJSON() ([]byte, error) {
	if len(c) == 0 {
		return []byte("{}"), nil
	}
	return c, nil
}

func (c *ConfigJSON) UnmarshalJSON(data []byte) error {
	*c = append((*c)[:0], data...)
	return nil
}

func (ConfigJSON) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "object", AdditionalProperties: true, Description: "The config of the record as a JSON object; its keys depend on the record's type"}
}

// String is the config as the database stores it: a JSON object
func (c ConfigJSON) String() string {
	if len(c) == 0 {
		return "{}"
	}
	return string(c)
}

// Document is a workspace's setup. Every array is ordered by name and then ref, so that two
// exports of the same data differ in ExportedAt alone. Where a record links to others, the links
// are refs of records in the same document, sorted; the only exception is a status page's monitors,
// which are in the order the page shows them.
type Document struct {
	Format          string    `json:"format" example:"peekaping-workspace"`
	Version         int       `json:"version" example:"1"`
	ExportedAt      time.Time `json:"exportedAt"`
	AppVersion      string    `json:"appVersion" doc:"The version of the server that wrote the file"`
	ContainsSecrets bool      `json:"containsSecrets" doc:"Always true: the file holds saved secrets in full"`

	Workspace            Workspace             `json:"workspace"`
	Settings             Settings              `json:"settings"`
	Tags                 []Tag                 `json:"tags"`
	Proxies              []Proxy               `json:"proxies"`
	NotificationChannels []NotificationChannel `json:"notificationChannels"`
	Monitors             []Monitor             `json:"monitors"`
	Maintenances         []Maintenance         `json:"maintenances"`
	StatusPages          []StatusPage          `json:"statusPages"`
}

// Workspace is what the document says of the workspace itself
type Workspace struct {
	Name string `json:"name"`
}

// Settings are the workspace's own settings, with the defaults of a workspace that stores none
type Settings struct {
	DataRetentionDays    int   `json:"dataRetentionDays"`
	CertExpiryNotifyDays []int `json:"certExpiryNotifyDays"`
}

type Tag struct {
	Ref         string  `json:"ref"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description *string `json:"description,omitempty"`
}

// Proxy holds its password in full. SensitiveFields is ["password"] when the proxy has one.
type Proxy struct {
	Ref             string   `json:"ref"`
	Protocol        string   `json:"protocol"`
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	Auth            bool     `json:"auth"`
	Username        string   `json:"username,omitempty"`
	Password        string   `json:"password,omitempty"`
	SensitiveFields []string `json:"sensitiveFields"`
}

// NotificationChannel holds its config in full. SensitiveFields lists the keys of Config that the
// API hides from responses: the saved secrets that are set, and the URLs whose user info or query
// values it masks.
type NotificationChannel struct {
	Ref             string     `json:"ref"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Active          bool       `json:"active"`
	IsDefault       bool       `json:"isDefault"`
	Config          ConfigJSON `json:"config"`
	SensitiveFields []string   `json:"sensitiveFields"`
}

type Monitor struct {
	Ref               string     `json:"ref"`
	Name              string     `json:"name"`
	Type              string     `json:"type"`
	Active            bool       `json:"active"`
	Interval          int        `json:"interval"`
	Timeout           int        `json:"timeout"`
	MaxRetries        int        `json:"maxRetries"`
	RetryInterval     int        `json:"retryInterval"`
	ResendInterval    int        `json:"resendInterval"`
	LocationRule      string     `json:"locationRule"`
	LocationThreshold *int       `json:"locationThreshold,omitempty"`
	Config            ConfigJSON `json:"config"`
	// PushToken is the monitor's push token, which a push monitor has
	PushToken               string   `json:"pushToken,omitempty"`
	ProxyRef                string   `json:"proxyRef,omitempty"`
	NotificationChannelRefs []string `json:"notificationChannelRefs"`
	TagRefs                 []string `json:"tagRefs"`
}

type Maintenance struct {
	Ref           string   `json:"ref"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Active        bool     `json:"active"`
	Strategy      string   `json:"strategy"`
	StartDateTime *string  `json:"startDateTime,omitempty"`
	EndDateTime   *string  `json:"endDateTime,omitempty"`
	StartTime     *string  `json:"startTime,omitempty"`
	EndTime       *string  `json:"endTime,omitempty"`
	Weekdays      []int    `json:"weekdays"`
	DaysOfMonth   []int    `json:"daysOfMonth"`
	IntervalDay   *int     `json:"intervalDay,omitempty"`
	Cron          *string  `json:"cron,omitempty"`
	Timezone      *string  `json:"timezone,omitempty"`
	Duration      *int     `json:"duration,omitempty"`
	MonitorRefs   []string `json:"monitorRefs"`
}

// StatusPage lists its custom domains and its monitors. MonitorRefs are in the order the page
// shows its monitors.
type StatusPage struct {
	Ref                 string   `json:"ref"`
	Slug                string   `json:"slug"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Icon                string   `json:"icon"`
	Theme               string   `json:"theme"`
	Published           bool     `json:"published"`
	ShowLocations       bool     `json:"showLocations"`
	FooterText          string   `json:"footerText"`
	AutoRefreshInterval int      `json:"autoRefreshInterval"`
	Domains             []string `json:"domains"`
	MonitorRefs         []string `json:"monitorRefs"`
}

// HeartbeatHeader is the first line of a heartbeat file
type HeartbeatHeader struct {
	Format     string    `json:"format" example:"peekaping-heartbeats"`
	Version    int       `json:"version" example:"1"`
	ExportedAt time.Time `json:"exportedAt"`
}

// HeartbeatLine is one heartbeat, a line of a heartbeat file after the header. MonitorRef is the
// ref the same monitor has in the workspace document of the same workspace.
type HeartbeatLine struct {
	MonitorRef string `json:"monitorRef"`
	// Probe is the name of the probe that ran the check, which is informational: probes belong to the install
	Probe     string    `json:"probe,omitempty"`
	Status    int       `json:"status"`
	Msg       string    `json:"msg"`
	Ping      int       `json:"ping"`
	Duration  int       `json:"duration"`
	DownCount int       `json:"downCount"`
	Retries   int       `json:"retries"`
	Important bool      `json:"important"`
	Time      time.Time `json:"time"`
	// EndTime is left out for a heartbeat that has none
	EndTime *time.Time `json:"endTime,omitempty"`
}
