package monitor

import "errors"

var (
	ErrNotFound = errors.New("not found")
)

var (
	ErrMonitorNotFound = errors.New("monitor not found")
)

// InvalidConfigError is a monitor type and config that don't fit together. Err says why.
type InvalidConfigError struct {
	Err error
}

func (e *InvalidConfigError) Error() string { return "invalid monitor configuration: " + e.Err.Error() }

func (e *InvalidConfigError) Unwrap() error { return e.Err }

// InvalidProbesError is a probe_ids that a monitor can't have. Err says why.
type InvalidProbesError struct {
	Err error
}

func (e *InvalidProbesError) Error() string { return "invalid probe_ids: " + e.Err.Error() }

func (e *InvalidProbesError) Unwrap() error { return e.Err }
