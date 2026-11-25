package monitor

import (
	"errors"
	"fmt"
	"math"

	"peekaping/internal/modules/shared"
)

// slowResponseConfig holds the resolved slow response alert settings of a monitor.
type slowResponseConfig struct {
	mode        shared.SlowResponseMode
	thresholdMs *int
	multiplier  *float64
	minMs       *int
	consecutive int
}

// slowResponseModeOrOff returns mode, or off when empty.
func slowResponseModeOrOff(mode shared.SlowResponseMode) shared.SlowResponseMode {
	if mode == "" {
		return shared.SlowResponseModeOff
	}
	return mode
}

// slowResponseConsecutiveOrDefault returns consecutive, or the default when <= 0.
func slowResponseConsecutiveOrDefault(consecutive int) int {
	if consecutive <= 0 {
		return shared.DefaultSlowResponseConsecutive
	}
	return consecutive
}

// validateSlowResponse checks a monitor's slow response settings and normalizes irrelevant fields
// for the chosen mode to nil.
func validateSlowResponse(cfg slowResponseConfig) (slowResponseConfig, error) {
	if cfg.consecutive < 1 {
		return cfg, &InvalidSlowResponseError{Err: errors.New("slow_response_consecutive must be at least 1")}
	}
	switch cfg.mode {
	case shared.SlowResponseModeOff:
		cfg.thresholdMs = nil
		cfg.multiplier = nil
		cfg.minMs = nil
		return cfg, nil
	case shared.SlowResponseModeFixed:
		if cfg.thresholdMs == nil || *cfg.thresholdMs < 1 {
			return cfg, &InvalidSlowResponseError{Err: errors.New("fixed mode requires slow_response_threshold_ms of at least 1 ms")}
		}
		cfg.multiplier = nil
		cfg.minMs = nil
		return cfg, nil
	case shared.SlowResponseModeBaseline:
		if cfg.multiplier == nil || math.IsNaN(*cfg.multiplier) || math.IsInf(*cfg.multiplier, 0) || *cfg.multiplier <= 1.0 {
			return cfg, &InvalidSlowResponseError{Err: errors.New("baseline mode requires slow_response_multiplier greater than 1")}
		}
		if cfg.minMs != nil && *cfg.minMs < 0 {
			return cfg, &InvalidSlowResponseError{Err: errors.New("slow_response_min_ms must be 0 or more")}
		}
		cfg.thresholdMs = nil
		return cfg, nil
	default:
		return cfg, &InvalidSlowResponseError{Err: fmt.Errorf("%q is not a slow_response_mode: use off, fixed or baseline", cfg.mode)}
	}
}

// resolveCreateSlowResponse validates and resolves the slow response settings for a create request.
func resolveCreateSlowResponse(dto *CreateUpdateDto) (slowResponseConfig, error) {
	consecutive := shared.DefaultSlowResponseConsecutive
	if dto.SlowResponseConsecutive != nil {
		consecutive = *dto.SlowResponseConsecutive
	}
	cfg := slowResponseConfig{
		mode:        slowResponseModeOrOff(dto.SlowResponseMode),
		thresholdMs: dto.SlowResponseThresholdMs,
		multiplier:  dto.SlowResponseMultiplier,
		minMs:       dto.SlowResponseMinMs,
		consecutive: consecutive,
	}
	return validateSlowResponse(cfg)
}

// CheckSlowResponseShape validates slow response settings from a CreateUpdateDto (e.g. on import).
func CheckSlowResponseShape(dto *CreateUpdateDto) error {
	_, err := resolveCreateSlowResponse(dto)
	return err
}

// resolveFullSlowResponse resolves and validates slow response settings for a full update.
// An older client that omits all slow response fields keeps the monitor's existing settings.
func resolveFullSlowResponse(current *Model, update *CreateUpdateDto) (slowResponseConfig, error) {
	hasAny := update.SlowResponseMode != "" ||
		update.SlowResponseThresholdMs != nil ||
		update.SlowResponseMultiplier != nil ||
		update.SlowResponseMinMs != nil ||
		update.SlowResponseConsecutive != nil
	if !hasAny {
		return slowResponseConfig{
			mode:        slowResponseModeOrOff(current.SlowResponseMode),
			thresholdMs: current.SlowResponseThresholdMs,
			multiplier:  current.SlowResponseMultiplier,
			minMs:       current.SlowResponseMinMs,
			consecutive: slowResponseConsecutiveOrDefault(current.SlowResponseConsecutive),
		}, nil
	}
	mode := update.SlowResponseMode
	if mode == "" {
		mode = slowResponseModeOrOff(current.SlowResponseMode)
	}
	consecutive := slowResponseConsecutiveOrDefault(current.SlowResponseConsecutive)
	if update.SlowResponseConsecutive != nil {
		consecutive = *update.SlowResponseConsecutive
	}
	return validateSlowResponse(slowResponseConfig{
		mode:        mode,
		thresholdMs: update.SlowResponseThresholdMs,
		multiplier:  update.SlowResponseMultiplier,
		minMs:       update.SlowResponseMinMs,
		consecutive: consecutive,
	})
}

// resolvePartialSlowResponse resolves and validates slow response settings for a partial update.
// Returns set=false when the update includes no slow response fields.
func resolvePartialSlowResponse(current *Model, update *PartialUpdateDto) (slowResponseConfig, bool, error) {
	hasAny := update.SlowResponseMode != nil ||
		update.SlowResponseThresholdMs != nil ||
		update.SlowResponseMultiplier != nil ||
		update.SlowResponseMinMs != nil ||
		update.SlowResponseConsecutive != nil
	if !hasAny {
		return slowResponseConfig{}, false, nil
	}
	cfg := slowResponseConfig{
		mode:        slowResponseModeOrOff(current.SlowResponseMode),
		thresholdMs: current.SlowResponseThresholdMs,
		multiplier:  current.SlowResponseMultiplier,
		minMs:       current.SlowResponseMinMs,
		consecutive: slowResponseConsecutiveOrDefault(current.SlowResponseConsecutive),
	}
	if update.SlowResponseMode != nil {
		cfg.mode = *update.SlowResponseMode
	}
	if update.SlowResponseThresholdMs != nil {
		cfg.thresholdMs = update.SlowResponseThresholdMs
	}
	if update.SlowResponseMultiplier != nil {
		cfg.multiplier = update.SlowResponseMultiplier
	}
	if update.SlowResponseMinMs != nil {
		cfg.minMs = update.SlowResponseMinMs
	}
	if update.SlowResponseConsecutive != nil {
		cfg.consecutive = *update.SlowResponseConsecutive
	}
	resolved, err := validateSlowResponse(cfg)
	return resolved, true, err
}
