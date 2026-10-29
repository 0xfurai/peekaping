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
	// EnforcesPrivateTargetBlock is true for a probe that blocks private and internal addresses
	// when the hello answer's BlockPrivateTargets says so. A probe from before that field ignores
	// it, and leaves this out: a server that blocks them deals no monitors to such an instance. A
	// server from before ignores this, so the protocol version stays the same.
	EnforcesPrivateTargetBlock bool `json:"enforces_private_target_block,omitempty"`
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
	// BlockPrivateTargets is true when the server blocks private and internal addresses
	// (BLOCK_PRIVATE_TARGETS), which the probe then blocks too. A server that doesn't leaves it out,
	// and so did every server before it existed: an absent field is off, and a probe that doesn't
	// know it ignores it, so the protocol version stays the same.
	BlockPrivateTargets bool `json:"block_private_targets,omitempty"`
	// ChecksPerHostPerMinute is the server's CHECKS_PER_HOST_PER_MINUTE: the most checks to one target
	// host a checking process runs per minute. A probe spaces its own checks to the stricter of this
	// and its own setting. A server that has none set leaves it out, as every server before it did:
	// an absent field is 0, off, and a probe that doesn't know it ignores it, so the protocol version
	// stays the same.
	ChecksPerHostPerMinute int `json:"checks_per_host_per_minute,omitempty"`
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
