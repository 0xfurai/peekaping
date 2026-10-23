package error_reporting

import (
	"context"
	"fmt"
	"sync"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

// Service stores the admin's choice and tells the processes that report. The stored setting is
// the source of truth: the Switch only carries it to the processes, so a Switch that fails never
// fails a change. It is told the stored choice again by Run.
type Service interface {
	// State reports the admin's choice (on, until they choose) and whether the environment forces
	// reporting off
	State(ctx context.Context) (State, error)
	// SetEnabled stores the admin's choice, turns reporting on or off in this process, tries to
	// tell the others, and returns the new state. The choice is stored while the environment
	// forces reporting off: the environment still wins, so the processes are told it is off. If
	// the others can't be told (Redis is down), that is logged and not returned: the choice is
	// stored and this process follows it, and Run tells the others when it can.
	SetEnabled(ctx context.Context, enabled bool) (State, error)
	// Apply tells the processes the stored choice. The api and the single-mode process call it
	// at start, since a choice made before a restart is still the admin's choice.
	Apply(ctx context.Context) error
	// Run applies the stored choice every interval until ctx is done. The cluster api runs it so
	// that a choice that missed Redis, or a key that Redis lost, is back within one interval, and
	// so that a change made on another api replica reaches this one.
	Run(ctx context.Context, every time.Duration)
}

type ServiceImpl struct {
	settings shared.SettingService
	cfg      *config.Config
	sw       Switch
	logger   *zap.SugaredLogger

	// mu keeps a change and a run of Run from telling the processes in the wrong order
	mu sync.Mutex
}

func NewService(settings shared.SettingService, cfg *config.Config, sw Switch, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{settings: settings, cfg: cfg, sw: sw, logger: logger.Named("[error-reporting-service]")}
}

func (s *ServiceImpl) State(ctx context.Context) (State, error) {
	enabled, err := s.chosenEnabled(ctx)
	if err != nil {
		return State{}, err
	}
	return State{Enabled: enabled, ForcedOff: s.cfg.ErrorReportingForcedOff()}, nil
}

func (s *ServiceImpl) SetEnabled(ctx context.Context, enabled bool) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value := "false"
	if enabled {
		value = "true"
	}
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: value, Type: "bool"}); err != nil {
		return State{}, fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	state := State{Enabled: enabled, ForcedOff: s.cfg.ErrorReportingForcedOff()}
	if err := s.tell(ctx, state); err != nil {
		// The choice is stored and this process follows it. Run tells the others.
		s.logger.Warnw("Failed to tell the other processes the error reporting choice, Run will try again", "error", err)
	}
	// A choice answers the one-time notice that tells an upgraded install reports are on. The choice
	// is stored and followed by now, so a failure here fails the request and asking again is harmless.
	// The usage ping's choice marks it too, at the same time when the setup form sends both, so the
	// write leaves a stored flag alone instead of racing to insert it.
	if _, err := s.settings.CreateIfAbsent(ctx, shared.TelemetryNoticeSeenKey, &shared.SettingModel{Value: "true", Type: "bool"}); err != nil {
		return State{}, fmt.Errorf("store %s: %w", shared.TelemetryNoticeSeenKey, err)
	}
	return state, nil
}

func (s *ServiceImpl) Apply(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.State(ctx)
	if err != nil {
		return err
	}
	return s.tell(ctx, state)
}

func (s *ServiceImpl) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Apply(ctx); err != nil {
				s.logger.Warnw("Failed to apply the error reporting choice, will try again", "error", err)
			}
		}
	}
}

// tell turns reporting on unless the admin opted out or the environment forces it off
func (s *ServiceImpl) tell(ctx context.Context, state State) error {
	if err := s.sw.Set(ctx, state.Enabled && !state.ForcedOff); err != nil {
		return fmt.Errorf("tell the processes that report: %w", err)
	}
	return nil
}

// chosenEnabled reports the admin's choice: on until they choose. A value that is not "true" counts
// as off, since nothing but this service writes the key.
func (s *ServiceImpl) chosenEnabled(ctx context.Context) (bool, error) {
	stored, err := s.settings.GetByKey(ctx, EnabledKey)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", EnabledKey, err)
	}
	return stored == nil || stored.Value == "true", nil
}
