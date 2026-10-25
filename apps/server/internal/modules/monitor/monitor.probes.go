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

// ErrNoProbes is what a graph without a ProbeLookup answers for every probe: its monitors all run
// on the local probe
var ErrNoProbes = errors.New("probes aren't available here")

// ProbeInfo is what the monitor service needs to know of a probe to assign a monitor to it
type ProbeInfo struct {
	Name string
	// SupportedTypes are the monitor types the probe runs. Empty means every type.
	SupportedTypes []string
}

// ProbeLookup tells the monitor service which probes exist. The probe module's service stands
// behind it, wired in internal/app, so that the monitor module doesn't import the probe module.
type ProbeLookup interface {
	// FindProbe returns nil when there is no such probe
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

// validateProbes checks that a monitor of the type can run on the probes: it has one or more,
// none twice, each exists and runs the type, and a push monitor is on the local probe alone. The
// caller passes the ctx of its transaction, if it has one, since a SQLite database has a single
// connection.
func (mr *MonitorServiceImpl) validateProbes(ctx context.Context, monitorType string, probeIDs []string) error {
	if len(probeIDs) == 0 {
		return &InvalidProbesError{Err: errors.New("a monitor runs on at least one probe")}
	}
	seen := make(map[string]bool, len(probeIDs))
	for _, probeID := range probeIDs {
		if seen[probeID] {
			return &InvalidProbesError{Err: fmt.Errorf("probe %q is listed twice", probeID)}
		}
		seen[probeID] = true
	}
	if monitorType == pushType && !slices.Equal(probeIDs, []string{shared.LocalProbeID}) {
		return &InvalidProbesError{Err: errors.New("a push monitor runs on the local probe only")}
	}
	for _, probeID := range probeIDs {
		if err := mr.validateProbe(ctx, monitorType, probeID); err != nil {
			return err
		}
	}
	return nil
}

// validateProbe checks that the probe exists and runs the monitor type. A push monitor runs on the
// local probe whatever types it lists.
func (mr *MonitorServiceImpl) validateProbe(ctx context.Context, monitorType, probeID string) error {
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
	case monitorType == pushType:
		// A push monitor needs no executor on a probe: its pushes arrive at the api. The types a
		// probe lists are the ones it can check, and a probe's instances leave push out of them.
		return nil
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
	if monitorType == current.Type && sameProbes(probeIDs, currentProbeIDs(current)) {
		return nil
	}
	return mr.validateProbes(ctx, monitorType, probeIDs)
}

// sameProbes reports whether the lists hold the same probes, whatever their order
func sameProbes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
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

// validateLocationRule checks a monitor's location rule against the probes it runs on: the rule
// exists, at_least has a threshold and no other rule does, and the threshold is from 1 to the
// number of probes, since a monitor can't have more Down locations than locations.
func validateLocationRule(rule shared.LocationRule, threshold *int, probes int) error {
	switch rule {
	case shared.LocationRuleAny, shared.LocationRuleAll:
		if threshold != nil {
			return &InvalidLocationRuleError{Err: errors.New("location_threshold is only for the at_least rule")}
		}
	case shared.LocationRuleAtLeast:
		if threshold == nil {
			return &InvalidLocationRuleError{Err: errors.New("at_least needs a location_threshold")}
		}
		if *threshold < 1 || *threshold > probes {
			noun, takes := "probes", fmt.Sprintf("1 to %d", probes)
			if probes == 1 {
				noun, takes = "probe", "only 1"
			}
			return &InvalidLocationRuleError{Err: fmt.Errorf("at_least %d is out of range: the monitor runs on %d %s, so it takes %s", *threshold, probes, noun, takes)}
		}
	default:
		return &InvalidLocationRuleError{Err: fmt.Errorf("%q is not a rule: use any, all or at_least", rule)}
	}
	return nil
}

// fullLocationRule is the rule a full update leaves the monitor with. One that sends neither a
// rule nor a threshold keeps the monitor's, as older clients send the whole monitor without them;
// a threshold sent alone goes with the rule the monitor has.
func fullLocationRule(current *Model, update *CreateUpdateDto) (shared.LocationRule, *int) {
	switch {
	case update.LocationRule != "":
		return update.LocationRule, update.LocationThreshold
	case update.LocationThreshold != nil:
		return locationRuleOrAny(current.LocationRule), update.LocationThreshold
	default:
		return locationRuleOrAny(current.LocationRule), current.LocationThreshold
	}
}

// validateFullLocationRule checks the location rule a full update leaves the monitor with against
// the probes it then runs on, and returns the rule
func validateFullLocationRule(current *Model, update *CreateUpdateDto) (shared.LocationRule, *int, error) {
	rule, threshold := fullLocationRule(current, update)
	probeIDs := update.ProbeIDs
	if probeIDs == nil {
		probeIDs = currentProbeIDs(current)
	}
	return rule, threshold, validateLocationRule(rule, threshold, len(probeIDs))
}

// partialLocationRule is the rule a partial update leaves the monitor with, and whether the update
// sets it. A rule sent replaces the monitor's, threshold included; a threshold sent alone goes with
// the rule the monitor has.
func partialLocationRule(current *Model, update *PartialUpdateDto) (rule shared.LocationRule, threshold *int, set bool) {
	switch {
	case update.LocationRule != nil:
		return *update.LocationRule, update.LocationThreshold, true
	case update.LocationThreshold != nil:
		return locationRuleOrAny(current.LocationRule), update.LocationThreshold, true
	default:
		return locationRuleOrAny(current.LocationRule), current.LocationThreshold, false
	}
}

// validatePartialLocationRule checks the location rule a partial update leaves the monitor with
// against the probes it then runs on, so that an update that only removes probes is checked too.
// An update that changes neither the rule nor the probes is not checked: a monitor can still be
// paused or renamed.
func validatePartialLocationRule(current *Model, update *PartialUpdateDto) error {
	rule, threshold, set := partialLocationRule(current, update)
	if !set && update.ProbeIDs == nil {
		return nil
	}
	probeIDs := update.ProbeIDs
	if probeIDs == nil {
		probeIDs = currentProbeIDs(current)
	}
	return validateLocationRule(rule, threshold, len(probeIDs))
}
