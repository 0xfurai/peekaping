package monitor

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"peekaping/internal/modules/shared"
)

// pushType is the monitor type that runs no checks of its own: it waits for pushes, which only
// reach the process that serves the push URL, so it runs on the local probe
const pushType = "push"

// ErrNoProbes is what a ProbeLookup returns on an install without probes, which is a MongoDB
// install (ADR 0001): its monitors all run on the local probe
var ErrNoProbes = errors.New("probes need SQLite or PostgreSQL storage")

// ProbeInfo is what the monitor service needs to know of a probe to assign a monitor to it
type ProbeInfo struct {
	Name string
	// SupportedTypes are the monitor types the probe runs. Empty means every type.
	SupportedTypes []string
}

// ProbeLookup tells the monitor service which probes exist. The probe module's service stands
// behind it, wired in internal/app, so that the monitor module doesn't import the probe module.
type ProbeLookup interface {
	// FindProbe returns nil when there is no such probe, and ErrNoProbes on an install without
	// probes
	FindProbe(ctx context.Context, id string) (*ProbeInfo, error)
}

// currentProbeIDs are the probes a monitor runs on. One read without its probes runs on the local
// one.
func currentProbeIDs(m *Model) []string {
	if len(m.ProbeIDs) == 0 {
		return []string{shared.LocalProbeID}
	}
	return m.ProbeIDs
}

// validateProbes checks that a monitor of the type can run on the probes: it has exactly one, it
// exists, it runs the type, and a push monitor is on the local probe. The caller passes the ctx of
// its transaction, if it has one, since a SQLite database has a single connection.
func (mr *MonitorServiceImpl) validateProbes(ctx context.Context, monitorType string, probeIDs []string) error {
	if len(probeIDs) != 1 {
		return &InvalidProbesError{Err: errors.New("a monitor runs on exactly one probe")}
	}
	probeID := probeIDs[0]
	if monitorType == pushType && probeID != shared.LocalProbeID {
		return &InvalidProbesError{Err: errors.New("a push monitor runs on the local probe only")}
	}

	info, err := mr.findProbe(ctx, probeID)
	switch {
	case errors.Is(err, ErrNoProbes):
		// Without probes the local one is all there is, and it runs everything
		if probeID == shared.LocalProbeID {
			return nil
		}
		return &InvalidProbesError{Err: fmt.Errorf("probe %q can't be used: %w", probeID, ErrNoProbes)}
	case err != nil:
		return err
	case info == nil:
		return &InvalidProbesError{Err: fmt.Errorf("probe %q doesn't exist", probeID)}
	case len(info.SupportedTypes) > 0 && !slices.Contains(info.SupportedTypes, monitorType):
		return &InvalidProbesError{Err: fmt.Errorf("probe %q doesn't run %s monitors", info.Name, monitorType)}
	}
	return nil
}

func (mr *MonitorServiceImpl) findProbe(ctx context.Context, id string) (*ProbeInfo, error) {
	if mr.probes == nil {
		return nil, ErrNoProbes
	}
	return mr.probes.FindProbe(ctx, id)
}

// validateChangedProbes checks the type and probes a monitor has after an update, unless they are
// the ones it has: an update that changes neither is not checked, so that a monitor whose probe
// stopped running its type can still be renamed or paused.
func (mr *MonitorServiceImpl) validateChangedProbes(ctx context.Context, current *Model, monitorType string, probeIDs []string) error {
	if monitorType == current.Type && slices.Equal(probeIDs, currentProbeIDs(current)) {
		return nil
	}
	return mr.validateProbes(ctx, monitorType, probeIDs)
}

// validateFullProbes checks the probes a full update leaves the monitor on. Probes it doesn't send
// are the ones the monitor has.
func (mr *MonitorServiceImpl) validateFullProbes(ctx context.Context, current *Model, update *CreateUpdateDto) error {
	probeIDs := update.ProbeIDs
	if probeIDs == nil {
		probeIDs = currentProbeIDs(current)
	}
	return mr.validateChangedProbes(ctx, current, update.Type, probeIDs)
}

// validatePartialProbes is validateFullProbes for a partial update, which sends only what changes
func (mr *MonitorServiceImpl) validatePartialProbes(ctx context.Context, current *Model, update *PartialUpdateDto) error {
	monitorType, probeIDs := current.Type, currentProbeIDs(current)
	if update.Type != nil {
		monitorType = *update.Type
	}
	if update.ProbeIDs != nil {
		probeIDs = update.ProbeIDs
	}
	return mr.validateChangedProbes(ctx, current, monitorType, probeIDs)
}
