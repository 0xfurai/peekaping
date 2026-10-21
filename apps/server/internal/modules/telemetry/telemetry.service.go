package telemetry

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"peekaping/internal/config"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/version"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// The modules the payload is read from, through their existing FindAll methods. Each is the
// part of the module's service the telemetry needs, so no repository interface changes and the
// MongoDB storage stays untouched (ADR 0001).
type (
	MonitorLister interface {
		FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*monitor.Model, error)
	}
	ChannelLister interface {
		FindAll(ctx context.Context, page int, limit int, q string) ([]*notification_channel.Model, error)
	}
	StatusPageLister interface {
		FindAll(ctx context.Context, page int, limit int, q string) ([]*status_page.Model, error)
	}
	MaintenanceLister interface {
		FindAll(ctx context.Context, page int, limit int, q string, strategy string) ([]*maintenance.Model, error)
	}
)

// Service builds the usage ping.
type Service interface {
	// BuildPayload builds the ping the install would send now, from the settings and the
	// install's monitors, notification channels, status pages and maintenance windows. It is
	// the one function behind both the ping and the preview in Settings. The instance ID is empty
	// unless the admin has opted in.
	BuildPayload(ctx context.Context) (Payload, error)
	// State reports the admin's choice, whether the environment forces the ping off, and the
	// payload the next ping would send.
	State(ctx context.Context) (State, error)
	// SetEnabled stores the admin's choice and returns the new state. Opting in creates the
	// instance ID unless there is one; opting out deletes it and the time of the last ping. The
	// choice is stored while the environment forces the ping off: the environment still wins.
	SetEnabled(ctx context.Context, enabled bool) (State, error)
}

// listPageSize is how many records the payload reads from a module at a time
const listPageSize = 200

// maxTypes is how many type names the payload holds in each list. The Worker rejects more.
const maxTypes = 50

// typeName is what the Worker accepts as a type name (apps/telemetry/src/validate.ts). A name that
// doesn't match is left out of the payload, not sent.
var typeName = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type ServiceImpl struct {
	settings     shared.SettingService
	monitors     MonitorLister
	channels     ChannelLister
	statusPages  StatusPageLister
	maintenances MaintenanceLister
	cfg          *config.Config
	mode         Mode
	logger       *zap.SugaredLogger
}

func NewService(
	settings shared.SettingService,
	monitors MonitorLister,
	channels ChannelLister,
	statusPages StatusPageLister,
	maintenances MaintenanceLister,
	cfg *config.Config,
	mode Mode,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		settings:     settings,
		monitors:     monitors,
		channels:     channels,
		statusPages:  statusPages,
		maintenances: maintenances,
		cfg:          cfg,
		mode:         mode,
		logger:       logger.Named("[telemetry-service]"),
	}
}

func (s *ServiceImpl) BuildPayload(ctx context.Context) (Payload, error) {
	enabled, err := s.storedEnabled(ctx)
	if err != nil {
		return Payload{}, err
	}
	return s.build(ctx, enabled)
}

func (s *ServiceImpl) State(ctx context.Context) (State, error) {
	enabled, err := s.storedEnabled(ctx)
	if err != nil {
		return State{}, err
	}
	payload, err := s.build(ctx, enabled)
	if err != nil {
		return State{}, err
	}
	return State{Enabled: enabled, ForcedOff: s.cfg.TelemetryForcedOff(), Payload: payload}, nil
}

func (s *ServiceImpl) SetEnabled(ctx context.Context, enabled bool) (State, error) {
	if enabled {
		if err := s.optIn(ctx); err != nil {
			return State{}, err
		}
	} else if err := s.optOut(ctx); err != nil {
		return State{}, err
	}
	return s.State(ctx)
}

// optIn creates the instance ID, unless there is one, and then records the opt-in. The ID comes
// first: the ping needs both, so a failure between the two sends nothing.
func (s *ServiceImpl) optIn(ctx context.Context) error {
	id, err := s.storedValue(ctx, InstanceIDKey)
	if err != nil {
		return err
	}
	if id == "" {
		// A random UUID v4 in lower case, from nothing about the install
		if _, err := s.settings.SetByKey(ctx, InstanceIDKey, &shared.SettingModel{Value: uuid.NewString(), Type: "string"}); err != nil {
			return fmt.Errorf("store %s: %w", InstanceIDKey, err)
		}
	}
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: "true", Type: "bool"}); err != nil {
		return fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	return nil
}

// optOut deletes the instance ID and the time of the last ping, and then records the opt-out.
// The ID goes first: the ping needs it, so a failure between the two sends nothing.
func (s *ServiceImpl) optOut(ctx context.Context) error {
	for _, key := range []string{InstanceIDKey, LastSentKey} {
		if err := s.settings.DeleteByKey(ctx, key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: "false", Type: "bool"}); err != nil {
		return fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	return nil
}

// build builds the payload. It carries the instance ID only while the admin is opted in.
func (s *ServiceImpl) build(ctx context.Context, optedIn bool) (Payload, error) {
	instanceID := ""
	if optedIn {
		var err error
		if instanceID, err = s.storedValue(ctx, InstanceIDKey); err != nil {
			return Payload{}, err
		}
	}

	monitorCount := 0
	monitorTypes := map[string]struct{}{}
	err := walkPages(ctx, func(ctx context.Context, page, limit int) ([]*monitor.Model, error) {
		return s.monitors.FindAll(ctx, page, limit, "", nil, nil, nil)
	}, func(m *monitor.Model) {
		monitorCount++
		addType(monitorTypes, m.Type)
	})
	if err != nil {
		return Payload{}, fmt.Errorf("list monitors: %w", err)
	}

	channelTypes := map[string]struct{}{}
	err = walkPages(ctx, func(ctx context.Context, page, limit int) ([]*notification_channel.Model, error) {
		return s.channels.FindAll(ctx, page, limit, "")
	}, func(c *notification_channel.Model) {
		addType(channelTypes, c.Type)
	})
	if err != nil {
		return Payload{}, fmt.Errorf("list notification channels: %w", err)
	}

	statusPages, err := s.statusPages.FindAll(ctx, 0, 1, "")
	if err != nil {
		return Payload{}, fmt.Errorf("list status pages: %w", err)
	}
	maintenances, err := s.maintenances.FindAll(ctx, 0, 1, "", "")
	if err != nil {
		return Payload{}, fmt.Errorf("list maintenance windows: %w", err)
	}

	return Payload{
		InstanceID:            instanceID,
		Version:               version.Version,
		Mode:                  string(s.mode),
		DB:                    storageBackend(s.cfg.DBType),
		OS:                    runtime.GOOS + "/" + runtime.GOARCH,
		Monitors:              monitorBucket(monitorCount),
		MonitorTypes:          sortedNames(monitorTypes),
		NotificationProviders: sortedNames(channelTypes),
		Features: Features{
			StatusPages: len(statusPages) > 0,
			Maintenance: len(maintenances) > 0,
			Metrics:     s.cfg.MetricsEnabled,
		},
	}, nil
}

// storedEnabled reports whether the admin has opted in
func (s *ServiceImpl) storedEnabled(ctx context.Context) (bool, error) {
	value, err := s.storedValue(ctx, EnabledKey)
	return value == "true", err
}

// storedValue returns the value of a setting, or "" when it isn't stored
func (s *ServiceImpl) storedValue(ctx context.Context, key string) (string, error) {
	stored, err := s.settings.GetByKey(ctx, key)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	if stored == nil {
		return "", nil
	}
	return stored.Value, nil
}

// walkPages calls visit on every record fetch returns, page by page
func walkPages[T any](
	ctx context.Context,
	fetch func(ctx context.Context, page, limit int) ([]T, error),
	visit func(T),
) error {
	for page := 0; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		records, err := fetch(ctx, page, listPageSize)
		if err != nil {
			return err
		}
		for _, record := range records {
			visit(record)
		}
		if len(records) < listPageSize {
			return nil
		}
	}
}

// addType adds a type name to the set when the Worker would accept it, lower-cased
func addType(set map[string]struct{}, name string) {
	name = strings.ToLower(name)
	if typeName.MatchString(name) {
		set[name] = struct{}{}
	}
}

// sortedNames returns the names in order, at most maxTypes of them, and never nil
func sortedNames(set map[string]struct{}) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxTypes {
		names = names[:maxTypes]
	}
	return names
}

// storageBackend is the storage backend as the Worker names it. A backend it doesn't know, such
// as MySQL, is "", which the Worker rejects: nothing is reported for it.
func storageBackend(dbType string) string {
	switch strings.ToLower(dbType) {
	case "sqlite":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mongo", "mongodb":
		return "mongo"
	}
	return ""
}
