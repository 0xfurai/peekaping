package monitor

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound = errors.New("not found")
)

var (
	ErrMonitorNotFound = errors.New("monitor not found")
)

// ErrProxyNotFound is returned, wrapped with the id, when a monitor is given a proxy that isn't in
// its workspace: it doesn't exist, or it belongs to another workspace, and the caller can't tell
// which (docs/adr/0005 section 8). A notification channel or tag that isn't there is
// monitor_notification.ErrUnknownChannel or monitor_tag.ErrUnknownTag.
//
// The controller answers 400 with "Proxy not found", the same for both.
var ErrProxyNotFound = errors.New("proxy not found")

// LookupError is a read of the monitor that failed, as opposed to one that found nothing. A route
// that loads the monitor before it reads its heartbeats or stats answers it with a 500.
type LookupError struct {
	Err error
}

func (e *LookupError) Error() string { return "find monitor: " + e.Err.Error() }

func (e *LookupError) Unwrap() error { return e.Err }

// InvalidConfigError is a monitor type and config that don't fit together. Err says why.
type InvalidConfigError struct {
	Err error
}

func (e *InvalidConfigError) Error() string { return "invalid monitor configuration: " + e.Err.Error() }

func (e *InvalidConfigError) Unwrap() error { return e.Err }

// InvalidTimeoutError is a timeout above 0.8 x the check interval, both in seconds.
type InvalidTimeoutError struct {
	Timeout, Interval int
}

func (e *InvalidTimeoutError) Error() string {
	return fmt.Sprintf("timeout %d s is above 0.8 x the %d s check interval", e.Timeout, e.Interval)
}

// IntervalTooShortError is a check interval or retry interval below MIN_CHECK_INTERVAL (PP-159),
// on a write that sets it: a create, an update, or a partial update that sends the field. The
// controller answers 400 with the code, so that a client can tell this refusal from an invalid
// configuration.
type IntervalTooShortError struct {
	Code    string `json:"code" example:"INTERVAL_TOO_SHORT"`
	Field   string `json:"field" doc:"The field that is below the minimum: interval or retry_interval." example:"interval"`
	Minimum int    `json:"minimum" doc:"The smallest interval and retry interval this install allows, in seconds." example:"60"`
	// Value is what the request sent, in seconds
	Value int `json:"-"`
}

func (e *IntervalTooShortError) Error() string {
	return fmt.Sprintf("%s of %d s is below the minimum of %d seconds", e.Field, e.Value, e.Minimum)
}

func intervalTooShortError(field string, value, minimum int) *IntervalTooShortError {
	return &IntervalTooShortError{Code: "INTERVAL_TOO_SHORT", Field: field, Minimum: minimum, Value: value}
}

// InvalidProbesError is a probe_ids that a monitor can't have. Err says why.
type InvalidProbesError struct {
	Err error
}

func (e *InvalidProbesError) Error() string { return "invalid probe_ids: " + e.Err.Error() }

func (e *InvalidProbesError) Unwrap() error { return e.Err }

// InvalidLocationRuleError is a location rule that a monitor can't have: one that doesn't exist, a
// threshold that goes with the wrong rule or that the monitor's probes can't reach. Err says why.
type InvalidLocationRuleError struct {
	Err error
}

func (e *InvalidLocationRuleError) Error() string { return "invalid location_rule: " + e.Err.Error() }

func (e *InvalidLocationRuleError) Unwrap() error { return e.Err }

// MonitorTypeDisabledError is a monitor type that this install turns off (DISABLED_MONITOR_TYPES,
// PP-157), on a write that would give a monitor that type: a create, or an update that changes the
// type. A monitor that already has the type can still be edited. The controller answers 400 with
// the code, so that a client can tell this refusal from an invalid configuration.
type MonitorTypeDisabledError struct {
	Code string `json:"code" example:"MONITOR_TYPE_DISABLED"`
	Type string `json:"type" doc:"The monitor type that is turned off." example:"docker"`
}

func (e *MonitorTypeDisabledError) Error() string {
	return fmt.Sprintf("%s monitors are turned off on this install", e.Type)
}

func monitorTypeDisabledError(monitorType string) *MonitorTypeDisabledError {
	return &MonitorTypeDisabledError{Code: "MONITOR_TYPE_DISABLED", Type: monitorType}
}

// PushTokenAlreadyUsedError is a push token that another monitor holds. Push tokens are unique
// across the install, and the database enforces it. The error leaves the token out: it is a secret
// the caller already has, and a log or a response has no need to carry it again.
type PushTokenAlreadyUsedError struct {
	Code string `json:"code" example:"PUSH_TOKEN_EXISTS"`
}

func (e *PushTokenAlreadyUsedError) Error() string {
	return "push token is already used by another monitor"
}

func pushTokenAlreadyUsedError() *PushTokenAlreadyUsedError {
	return &PushTokenAlreadyUsedError{Code: "PUSH_TOKEN_EXISTS"}
}
