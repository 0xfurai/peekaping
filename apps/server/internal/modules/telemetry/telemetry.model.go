// Package telemetry is the usage ping (ticket #232, ADR 0003): an anonymous summary of how an
// install is set up, sent at most once a day until an admin opts out. The payload is the
// contract the Worker in apps/telemetry enforces field by field (apps/telemetry/src/validate.ts).
package telemetry

// Settings keys. They are not user-editable, so the settings API can't reach them: the telemetry
// API is the only way to read or change them.
const (
	// EnabledKey holds the admin's choice: "true" after they opted in, "false" after they opted
	// out. Until they choose it is absent, which counts as on.
	EnabledKey = "TELEMETRY_ENABLED"
	// InstanceIDKey holds the instance ID. It exists while the ping is on: an install that never
	// chose makes it on first use, and an opt-out deletes it.
	InstanceIDKey = "TELEMETRY_INSTANCE_ID"
	// LastSentKey holds when the last usage ping was attempted, as an RFC 3339 time in UTC.
	LastSentKey = "TELEMETRY_LAST_SENT"
)

// Mode is how the install runs. It is passed in by the registration of each binary: the MODE
// setting is dev, prod or test and says nothing about it.
type Mode string

const (
	// ModeSingle is the one-process runtime (cmd/peekaping)
	ModeSingle Mode = "single"
	// ModeCluster is the api replicas and the probes, with Redis between the replicas
	ModeCluster Mode = "cluster"
)

// Payload is the usage ping, and the preview Settings shows. Every field is named in ADR 0003,
// and the service builds it field by field. Adding a field is a decision that changes the ADR and
// the Worker's validator too.
type Payload struct {
	InstanceID   string   `json:"instance_id" doc:"The random instance ID; empty after an admin opts out, and while TELEMETRY forces the ping off and no ID was made before" example:"6c1b4f0e-5d3a-4c8e-9f21-0a7b8c9d1e2f"`
	Version      string   `json:"version" doc:"The Peekaping version" example:"0.0.47"`
	Mode         string   `json:"mode" doc:"How the install runs" enum:"single,cluster" example:"single"`
	DB           string   `json:"db" doc:"The storage backend: sqlite, postgres or mysql. Empty for a backend the ping doesn't report." example:"sqlite"`
	OS           string   `json:"os" doc:"Operating system and architecture" example:"linux/arm64"`
	Monitors     string   `json:"monitors" doc:"How many monitors there are, paused ones included, as a bucket" enum:"0,1-10,11-50,51-200,201-1000,1000+" example:"1-10"`
	MonitorTypes []string `json:"monitor_types" doc:"The distinct monitor types in use, lower case and sorted" nullable:"false"`
	// NotificationChannelTypes keeps the JSON key notification_providers: it is the name the Worker accepts.
	NotificationChannelTypes []string `json:"notification_providers" doc:"The distinct notification channel types in use, lower case and sorted" nullable:"false"`
	Features                 Features `json:"features" doc:"Which features are on"`
}

// Features says whether the install uses three features, and nothing about how.
type Features struct {
	StatusPages bool `json:"status_pages" doc:"Whether there is any status page"`
	Maintenance bool `json:"maintenance" doc:"Whether there is any maintenance window"`
	Metrics     bool `json:"metrics" doc:"Whether the Prometheus metrics endpoint is on"`
}

// State is what the telemetry API reports: the admin's choice, whether the environment overrides
// it, and the payload the next ping would send.
type State struct {
	// Enabled is the admin's stored choice, or on when they haven't chosen. It stays what they
	// chose while ForcedOff is true.
	Enabled bool
	// ForcedOff is true when TELEMETRY switches the ping off whatever Enabled says
	ForcedOff bool
	// NoticePending is true while the admin has not yet been told that the usage ping and error
	// reports are on, and at least one of them is not forced off by the environment
	NoticePending bool
	// Payload is what the next ping would send
	Payload Payload
}

// monitorBucket is the bucket a number of monitors falls in
func monitorBucket(count int) string {
	switch {
	case count <= 0:
		return "0"
	case count <= 10:
		return "1-10"
	case count <= 50:
		return "11-50"
	case count <= 200:
		return "51-200"
	case count <= 1000:
		return "201-1000"
	default:
		return "1000+"
	}
}
