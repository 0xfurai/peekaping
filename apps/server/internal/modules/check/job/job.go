// Package job is what a check of a monitor takes to run: the Job a check is loaded as, the errors
// that say there is none to run, and the monitor config's certificate flag. It has no services
// behind it, so the remote probe, which gets its jobs from the server's assignment, uses it too.
// Package check has the Loader that reads a Job from the server's storage.
package job

import (
	"encoding/json"
	"errors"

	"peekaping/internal/modules/shared"
)

var (
	// ErrMonitorNotFound means the monitor no longer exists, for example because it was
	// deleted after its check was scheduled
	ErrMonitorNotFound = errors.New("monitor not found")
	// ErrMonitorPaused means the monitor is paused, so it gets no checks until it's resumed
	ErrMonitorPaused = errors.New("monitor is paused")
)

// Job is everything one check of a monitor needs, as the monitor stands when it is loaded
type Job struct {
	Monitor *shared.Monitor
	// Proxy is the monitor's proxy; nil when it has none or the proxy couldn't be loaded
	Proxy *shared.Proxy
	// IsUnderMaintenance is set while any of the monitor's maintenance windows is active
	IsUnderMaintenance bool
	// CheckCertExpiry is set when the monitor's config asks for its TLS certificate's expiry to be checked
	CheckCertExpiry bool
	// LastHeartbeat is the latest heartbeat of a push monitor; nil for other monitor types and
	// for a push monitor that has none yet
	LastHeartbeat *shared.HeartBeatModel
}

// CertExpiryRequested reports whether a monitor's config asks for the expiry of its TLS
// certificate to be checked: its check_cert_expiry flag, which every monitor type may carry. An
// empty config asks for nothing. A config that isn't JSON asks for nothing too, and the error
// says why.
func CertExpiryRequested(config string) (bool, error) {
	if config == "" {
		return false, nil
	}
	var flags struct {
		CheckCertExpiry bool `json:"check_cert_expiry"`
	}
	if err := json.Unmarshal([]byte(config), &flags); err != nil {
		return false, err
	}
	return flags.CheckCertExpiry, nil
}
