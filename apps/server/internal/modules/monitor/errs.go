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
