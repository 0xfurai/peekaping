// Package probeproto is the wire contract between Peekaping and a probe: the messages of the four
// calls under /probe/v1 (hello, assignments, results and bye) and a client that makes them over
// net/http.
//
// The probe binary is built from this package, so it imports the standard library and nothing
// from the rest of Peekaping. The server's handlers answer with these types, and its contract
// tests drive them through Client.
package probeproto

import (
	"encoding/json"
	"time"
)

// ProtocolVersion is the version of this package's messages. It moves only when a message changes shape.
const ProtocolVersion = 1

const (
	// PathPrefix is where the API serves the probe routes
	PathPrefix = "/probe/v1"
	// HeaderInstanceID carries the probe instance's id on every request
	HeaderInstanceID = "X-Probe-Instance"
)

// Rejection reasons of a result
const (
	ReasonUnknownMonitor = "unknown_monitor"
	ReasonNotAssigned    = "not_assigned"
	ReasonTooOld         = "too_old"
	ReasonDuplicate      = "duplicate"
	ReasonInvalid        = "invalid"
)

// Heartbeat status words on the wire
const (
	StatusUp          = "up"
	StatusDown        = "down"
	StatusPending     = "pending"
	StatusMaintenance = "maintenance"
)

type HelloRequest struct {
	InstanceID      string   `json:"instance_id"`
	ProtocolVersion int      `json:"protocol_version"`
	ProbeVersion    string   `json:"probe_version"`
	SupportedTypes  []string `json:"supported_types"`
	Hostname        string   `json:"hostname"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
}

type ProbeInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

type HelloResponse struct {
	ProtocolVersion int       `json:"protocol_version"`
	ServerVersion   string    `json:"server_version"`
	ServerTime      time.Time `json:"server_time"`
	Probe           ProbeInfo `json:"probe"`
	PollTimeoutMs   int       `json:"poll_timeout_ms"`
	FlushIntervalMs int       `json:"flush_interval_ms"`
	BatchMax        int       `json:"batch_max"`
	BufferMax       int       `json:"buffer_max"`
}

type Proxy struct {
	ID        string    `json:"id"`
	Protocol  string    `json:"protocol"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Auth      bool      `json:"auth"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Monitor struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Interval       int       `json:"interval"`
	Timeout        int       `json:"timeout"`
	MaxRetries     int       `json:"max_retries"`
	RetryInterval  int       `json:"retry_interval"`
	ResendInterval int       `json:"resend_interval"`
	Config         string    `json:"config"`
	UpdatedAt      time.Time `json:"updated_at"`
	Proxy          *Proxy    `json:"proxy,omitempty"`
}

type AssignmentsResponse struct {
	Version  string    `json:"version"`
	Monitors []Monitor `json:"monitors"`
}

type Result struct {
	ID        string          `json:"id"`
	MonitorID string          `json:"monitor_id"`
	Status    string          `json:"status"`
	Message   string          `json:"message"`
	PingMs    int             `json:"ping_ms"`
	StartTime time.Time       `json:"start_time"`
	EndTime   time.Time       `json:"end_time"`
	TLSInfo   json.RawMessage `json:"tls_info,omitempty"`
}

type ResultsRequest struct {
	Results []Result `json:"results"`
}

type ResultOutcome struct {
	ID       string `json:"id"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type ResultsResponse struct {
	Results []ResultOutcome `json:"results"`
}

// ErrorResponse is the body of every error reply: the API's usual {"message": ..., "data": null}
type ErrorResponse struct {
	Message string `json:"message"`
}
