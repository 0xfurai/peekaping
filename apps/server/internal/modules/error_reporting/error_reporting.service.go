package error_reporting

import (
	"context"
	"fmt"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
)

// Service stores the admin's choice and tells the processes that report.
type Service interface {
	// State reports the admin's choice and whether the environment forces reporting off
	State(ctx context.Context) (State, error)
	// SetEnabled stores the admin's choice, tells the processes, and returns the new state. The
	// choice is stored while the environment forces reporting off: the environment still wins,
	// so the processes are told it is off. If they can't be told (Redis is down), the choice is
	// stored all the same and the error is returned: they are told again at the api's next
	// start or the next change.
	SetEnabled(ctx context.Context, enabled bool) (State, error)
	// Apply tells the processes the stored choice. The api and the single-mode process call it
	// at start, since a choice made before a restart is still the admin's choice.
	Apply(ctx context.Context) error
}

type ServiceImpl struct {
	settings shared.SettingService
	cfg      *config.Config
	sw       Switch
}

func NewService(settings shared.SettingService, cfg *config.Config, sw Switch) Service {
	return &ServiceImpl{settings: settings, cfg: cfg, sw: sw}
}

func (s *ServiceImpl) State(ctx context.Context) (State, error) {
	enabled, err := s.storedEnabled(ctx)
	if err != nil {
		return State{}, err
	}
	return State{Enabled: enabled, ForcedOff: s.cfg.ErrorReportingForcedOff()}, nil
}

func (s *ServiceImpl) SetEnabled(ctx context.Context, enabled bool) (State, error) {
	value := "false"
	if enabled {
		value = "true"
	}
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: value, Type: "bool"}); err != nil {
		return State{}, fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	state := State{Enabled: enabled, ForcedOff: s.cfg.ErrorReportingForcedOff()}
	if err := s.tell(ctx, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *ServiceImpl) Apply(ctx context.Context) error {
	state, err := s.State(ctx)
	if err != nil {
		return err
	}
	return s.tell(ctx, state)
}

// tell turns reporting on only when the admin chose it and the environment allows it
func (s *ServiceImpl) tell(ctx context.Context, state State) error {
	if err := s.sw.Set(ctx, state.Enabled && !state.ForcedOff); err != nil {
		return fmt.Errorf("tell the processes that report: %w", err)
	}
	return nil
}

// storedEnabled reports whether the admin has opted in
func (s *ServiceImpl) storedEnabled(ctx context.Context) (bool, error) {
	stored, err := s.settings.GetByKey(ctx, EnabledKey)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", EnabledKey, err)
	}
	return stored != nil && stored.Value == "true", nil
}
