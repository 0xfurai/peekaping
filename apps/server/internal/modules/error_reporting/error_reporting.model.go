// Package error_reporting is the admin's switch for error reports (ticket #233, ADR 0003): the
// API that reads and changes the choice, the setting that stores it, and how the choice reaches
// the process that reports. The reports themselves are made by package reporting.
package error_reporting

import (
	"context"

	"peekaping/internal/reporting"
)

// EnabledKey is the setting that holds the admin's choice: "true" after they opted in to error
// reports, "false" after they opted out. Until they choose it is absent, which counts as on. It is
// not user-editable, so the settings API can't reach it: the error reporting API is the only way
// to read or change it.
const EnabledKey = "ERROR_REPORTING_ENABLED"

// State is what the error reporting API reports: the admin's choice, and whether the
// environment overrides it.
type State struct {
	// Enabled is the admin's stored choice, or on when they haven't chosen. It stays what they
	// chose while ForcedOff is true.
	Enabled bool
	// ForcedOff is true when ERROR_REPORTING switches error reports off whatever Enabled
	// says
	ForcedOff bool
}

// Switch tells this process whether to report. Single mode and each api replica have one. The
// replicas don't tell one another: each follows the stored choice (Service.Run).
type Switch interface {
	// Set turns reporting on or off in this process. on is the effective choice: false while the
	// environment forces reporting off.
	Set(ctx context.Context, on bool) error
}

// LocalSwitch is the switch of the server binaries: it turns reporting on or off in its own process
type LocalSwitch struct{}

func (LocalSwitch) Set(_ context.Context, on bool) error {
	reporting.SetEnabled(on)
	return nil
}
