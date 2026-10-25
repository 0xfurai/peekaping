package telemetry

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/version"

	"github.com/google/uuid"
)

// The modules the payload is read from, through their existing FindAll methods. Each is the
// part of the module's service the telemetry needs, so no repository interface changes.
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
	// after an opt-out, and while the environment forces the ping off and none was made before.
	BuildPayload(ctx context.Context) (Payload, error)
	// State reports the admin's choice (on, until they choose), whether the environment forces
	// the ping off, and the payload the next ping would send.
	State(ctx context.Context) (State, error)
	// SetEnabled stores the admin's choice and returns the new state. Opting in creates the
	// instance ID unless there is one; opting out deletes it and the time of the last ping. The
	// choice is stored while the environment forces the ping off: the environment still wins. Any
	// choice also marks the upgrade notice seen.
	SetEnabled(ctx context.Context, enabled bool) (State, error)
	// MarkNoticeSeen records that the admin dismissed the upgrade notice, which stops it showing.
	// It is no choice: the usage ping and error reports stay as they were.
	MarkNoticeSeen(ctx context.Context) (State, error)

	// The sender's Source
	Source
}

// pingInterval is how long after a ping the next one is due
const pingInterval = 24 * time.Hour

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
}

func NewService(
	settings shared.SettingService,
	monitors MonitorLister,
	channels ChannelLister,
	statusPages StatusPageLister,
	maintenances MaintenanceLister,
	cfg *config.Config,
	mode Mode,
) Service {
	return &ServiceImpl{
		settings:     settings,
		monitors:     monitors,
		channels:     channels,
		statusPages:  statusPages,
		maintenances: maintenances,
		cfg:          cfg,
		mode:         mode,
	}
}

func (s *ServiceImpl) BuildPayload(ctx context.Context) (Payload, error) {
	enabled, err := s.chosenEnabled(ctx)
	if err != nil {
		return Payload{}, err
	}
	return s.build(ctx, enabled)
}

func (s *ServiceImpl) State(ctx context.Context) (State, error) {
	enabled, err := s.chosenEnabled(ctx)
	if err != nil {
		return State{}, err
	}
	payload, err := s.build(ctx, enabled)
	if err != nil {
		return State{}, err
	}
	pending, err := s.noticePending(ctx)
	if err != nil {
		return State{}, err
	}
	return State{Enabled: enabled, ForcedOff: s.cfg.TelemetryForcedOff(), NoticePending: pending, Payload: payload}, nil
}

func (s *ServiceImpl) SetEnabled(ctx context.Context, enabled bool) (State, error) {
	if enabled {
		if err := s.optIn(ctx); err != nil {
			return State{}, err
		}
	} else if err := s.optOut(ctx); err != nil {
		return State{}, err
	}
	// The choice is stored. A failure here fails the request, and asking again is harmless.
	if err := s.markNoticeSeen(ctx); err != nil {
		return State{}, err
	}
	return s.State(ctx)
}

func (s *ServiceImpl) MarkNoticeSeen(ctx context.Context) (State, error) {
	if err := s.markNoticeSeen(ctx); err != nil {
		return State{}, err
	}
	return s.State(ctx)
}

// markNoticeSeen stores the flag in one statement that leaves a stored flag alone: the setup form
// sends both choices at once, each marks the notice seen with the same "true", and a plain set
// (an update, then an insert) lets the second insert hit the key and fail a choice that is stored
func (s *ServiceImpl) markNoticeSeen(ctx context.Context) error {
	if _, err := s.settings.CreateIfAbsent(ctx, shared.TelemetryNoticeSeenKey, &shared.SettingModel{Value: "true", Type: "bool"}); err != nil {
		return fmt.Errorf("store %s: %w", shared.TelemetryNoticeSeenKey, err)
	}
	return nil
}

// noticePending reports whether the admin still has to be told that the usage ping and error
// reports are on: the notice is not marked seen, and the environment does not force both off
func (s *ServiceImpl) noticePending(ctx context.Context) (bool, error) {
	if s.cfg.TelemetryForcedOff() && s.cfg.ErrorReportingForcedOff() {
		return false, nil
	}
	seen, err := s.storedValue(ctx, shared.TelemetryNoticeSeenKey)
	return seen != "true", err
}

func (s *ServiceImpl) NextPing(ctx context.Context, now time.Time) (*Payload, error) {
	if s.cfg.TelemetryForcedOff() {
		return nil, nil
	}
	enabled, err := s.chosenEnabled(ctx)
	if err != nil || !enabled {
		return nil, err
	}
	id, err := s.ensureInstanceID(ctx)
	if err != nil || id == "" {
		return nil, err
	}
	lastSent, err := s.storedValue(ctx, LastSentKey)
	if err != nil {
		return nil, err
	}
	// A time that is missing, unreadable or still ahead of the clock counts as no ping yet: a
	// clock that was wrong once must not silence the ping until it is right again.
	if last, err := time.Parse(time.RFC3339, lastSent); err == nil && !last.After(now) && now.Sub(last) <= pingInterval {
		return nil, nil
	}
	payload, err := s.build(ctx, true)
	if err != nil || payload.InstanceID == "" {
		// An admin who opted out while the payload was built has no ID left to send
		return nil, err
	}
	return &payload, nil
}

func (s *ServiceImpl) RecordSent(ctx context.Context, instanceID string, at time.Time) error {
	// Opting out deletes the last-sent time with the instance ID. A ping that was already on its
	// way must not bring it back: the next opt-in would wait up to a day for its first ping.
	stored, err := s.storedValue(ctx, InstanceIDKey)
	if err != nil {
		return err
	}
	if instanceID == "" || stored != instanceID {
		return nil
	}
	if _, err := s.settings.SetByKey(ctx, LastSentKey, &shared.SettingModel{Value: at.UTC().Format(time.RFC3339), Type: "string"}); err != nil {
		return fmt.Errorf("store %s: %w", LastSentKey, err)
	}
	return nil
}

// optIn creates the instance ID, unless there is one, and then records the opt-in. The ID comes
// first: the ping needs both, so a failure between the two sends nothing.
func (s *ServiceImpl) optIn(ctx context.Context) error {
	if _, err := s.createInstanceID(ctx); err != nil {
		return err
	}
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: "true", Type: "bool"}); err != nil {
		return fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	return nil
}

// optOut records the opt-out, and then deletes the instance ID and the time of the last ping. The
// choice goes first: with no stored choice the ping is on and makes an ID of its own, so an ID that
// is deleted while the choice still says "on" would be made again, and a failure between the two
// steps would leave the ping the admin refused running.
func (s *ServiceImpl) optOut(ctx context.Context) error {
	if _, err := s.settings.SetByKey(ctx, EnabledKey, &shared.SettingModel{Value: "false", Type: "bool"}); err != nil {
		return fmt.Errorf("store %s: %w", EnabledKey, err)
	}
	for _, key := range []string{InstanceIDKey, LastSentKey} {
		if err := s.settings.DeleteByKey(ctx, key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return nil
}

// ensureInstanceID returns the instance ID, making one when there is none: an install that never
// chose has no ID until the first state read or ping. Nothing is made while the environment forces
// the ping off, so a TELEMETRY=off install is never given an identifier.
func (s *ServiceImpl) ensureInstanceID(ctx context.Context) (string, error) {
	id, err := s.storedValue(ctx, InstanceIDKey)
	if err != nil || id != "" || s.cfg.TelemetryForcedOff() {
		return id, err
	}
	id, err = s.createInstanceID(ctx)
	if err != nil {
		return "", err
	}
	// An admin who opted out while the ID was made deleted the ID they knew of, not this one
	enabled, err := s.chosenEnabled(ctx)
	if err != nil {
		return "", err
	}
	if !enabled {
		if err := s.settings.DeleteByKey(ctx, InstanceIDKey); err != nil {
			return "", fmt.Errorf("delete %s: %w", InstanceIDKey, err)
		}
		return "", nil
	}
	return id, nil
}

// createInstanceID stores a new instance ID unless there is one, and returns the stored one. Of
// several api replicas that make an ID at the same moment, all end up with the one that was
// stored first.
func (s *ServiceImpl) createInstanceID(ctx context.Context) (string, error) {
	// A random UUID v4 in lower case, from nothing about the install
	created, err := s.settings.CreateIfAbsent(ctx, InstanceIDKey, &shared.SettingModel{Value: uuid.NewString(), Type: "string"})
	if err != nil {
		return "", fmt.Errorf("store %s: %w", InstanceIDKey, err)
	}
	return created.Value, nil
}

// build builds the payload. It carries the instance ID only while the ping is on.
func (s *ServiceImpl) build(ctx context.Context, on bool) (Payload, error) {
	instanceID := ""
	if on {
		var err error
		if instanceID, err = s.ensureInstanceID(ctx); err != nil {
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
		InstanceID:               instanceID,
		Version:                  version.Version,
		Mode:                     string(s.mode),
		DB:                       storageBackend(s.cfg.DBType),
		OS:                       runtime.GOOS + "/" + runtime.GOARCH,
		Monitors:                 monitorBucket(monitorCount),
		MonitorTypes:             sortedNames(monitorTypes),
		NotificationChannelTypes: sortedNames(channelTypes),
		Features: Features{
			StatusPages: len(statusPages) > 0,
			Maintenance: len(maintenances) > 0,
			Metrics:     s.cfg.MetricsEnabled,
		},
	}, nil
}

// chosenEnabled reports the admin's choice: on until they choose. A value that is neither "true"
// nor "false" counts as off, since nothing but this service writes the key.
func (s *ServiceImpl) chosenEnabled(ctx context.Context) (bool, error) {
	value, err := s.storedValue(ctx, EnabledKey)
	return value == "" || value == "true", err
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

// storageBackend is the storage backend as the Worker names it. A backend it doesn't know is "",
// which the Worker rejects: nothing is reported for it.
func storageBackend(dbType string) string {
	switch strings.ToLower(dbType) {
	case "sqlite":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	}
	return ""
}
