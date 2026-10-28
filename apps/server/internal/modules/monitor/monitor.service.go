package monitor

import (
	"context"
	"errors"
	"fmt"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service works on monitors, which belong to a workspace (docs/adr/0005 section 3).
//
// A method that serves a workspace's request takes the workspace right after the context, and
// answers ErrMonitorNotFound for a monitor of another workspace. A method whose name ends in
// AnyWorkspace works across workspaces, for the paths that run for the whole install.
type Service interface {
	// Create makes the monitor in the workspace, whatever workspace the monitor says
	Create(ctx context.Context, workspaceID workspace.ID, monitor *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	// FindByIDAnyWorkspace is for a path that loads the monitor it was told about, whatever its
	// workspace: a notification, a public status page, a badge, the probe protocol
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	FindByIDs(ctx context.Context, workspaceID workspace.ID, ids []string) ([]*Model, error)
	FindByIDsAnyWorkspace(ctx context.Context, ids []string) ([]*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every monitor of the install, paused ones included
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	FindActiveAnyWorkspace(ctx context.Context) ([]*Model, error)
	FindActivePaginatedAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	// FindActiveByProbeAnyWorkspace returns a page of the active monitors the probe runs. A probe
	// belongs to the install, so it runs the monitors of every workspace.
	FindActiveByProbeAnyWorkspace(ctx context.Context, probeID string, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, monitor *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, monitor *PartialUpdateDto, noPublish bool) (*Model, error)
	// UpdatePartialAnyWorkspace is UpdatePartial for a background job that knows the monitor by its
	// id alone, such as the one that stores a status change
	UpdatePartialAnyWorkspace(ctx context.Context, id string, monitor *PartialUpdateDto, noPublish bool) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
	ValidateMonitorConfig(monitorType string, configJSON string) error

	// GetHeartbeats, GetStatPoints and GetUptimeStats load the monitor in the workspace first, and
	// answer ErrMonitorNotFound when it isn't there. The heartbeats and stats are then read by the
	// monitor's id.
	GetHeartbeats(ctx context.Context, workspaceID workspace.ID, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error)
	// GetHeartbeatsAnyWorkspace is for a public status page, which shows the monitors published on
	// it whatever their workspace
	GetHeartbeatsAnyWorkspace(ctx context.Context, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error)

	// RemoveProxyReference clears the proxy from the workspace's monitors that use it, and returns
	// the monitors it changed as they are stored now. It runs in the transaction ctx carries, if
	// any (dbtx), and publishes nothing: the caller calls PublishUpdated once its transaction has
	// committed, since an event for a change that is rolled back would be a lie.
	RemoveProxyReference(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error)
	// PublishUpdated publishes a monitor-updated event for each monitor, so that the scheduler and
	// the probes load their config again
	PublishUpdated(ctx context.Context, monitors []*Model)
	FindByProxyId(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error)

	GetStatPoints(ctx context.Context, workspaceID workspace.ID, id, probeID string, since, until time.Time, granularity string) (*StatPointsSummaryDto, error)
	GetUptimeStats(ctx context.Context, workspaceID workspace.ID, id, probeID string) (*CustomUptimeStatsDto, error)
	// GetUptime24hAnyWorkspace returns the monitor's uptime over the last 24 hours as a percentage,
	// from the checks one probe ran (all of them when probeID is empty), with one stats read. It
	// is nil when the period has no Up or Down heartbeat, which is not the same as 0%. It is for
	// public status pages and badges.
	GetUptime24hAnyWorkspace(ctx context.Context, id, probeID string) (*float64, error)

	// FindOneByPushTokenAnyWorkspace finds the monitor a push token belongs to: the token alone
	// names the monitor, and so its workspace
	FindOneByPushTokenAnyWorkspace(ctx context.Context, pushToken string) (*Model, error)
	ResetMonitorData(ctx context.Context, workspaceID workspace.ID, id string) error
	// RepairStoredStatusesAnyWorkspace is the job at startup that repairs the monitors of every
	// workspace
	RepairStoredStatusesAnyWorkspace(ctx context.Context) (int, error)
	// AssignProbeToAllAnyWorkspace adds the probe to every monitor of the install that isn't a
	// push monitor, paused ones included, of one of the types (every type when there are none)
	// and doesn't run on it yet, and returns how many it added. A probe belongs to the install, so
	// it is assigned the monitors of every workspace. The monitors keep their other probes and
	// their location rule. claim, when it isn't nil, runs first in the same transaction, and
	// nothing is assigned when it says false: a caller that must assign once passes the step that
	// clears its flag, so that the flag is cleared only if the assignment is stored. It returns
	// the error of an unknown probe, and of a claim or write that fails, with nothing assigned.
	AssignProbeToAllAnyWorkspace(ctx context.Context, probeID string, supportedTypes []string, claim func(ctx context.Context) (bool, error)) (int, error)
}

// StatusRepairPageSize is how many monitors RepairStoredStatuses reads at a time.
const StatusRepairPageSize = 100

type StatPoint struct {
	Up          int     `json:"up"`
	Down        int     `json:"down"`
	Maintenance int     `json:"maintenance"`
	Ping        float64 `json:"ping"`
	PingMin     float64 `json:"ping_min"`
	PingMax     float64 `json:"ping_max"`
	Timestamp   int64   `json:"timestamp"`
}

type MonitorServiceImpl struct {
	monitorRepository          MonitorRepository
	transactor                 dbtx.Transactor
	heartbeatService           heartbeat.Service
	eventBus                   events.EventBus
	monitorNotificationService monitor_notification.Service
	monitorTagService          monitor_tag.Service
	executorRegistry           *executor.ExecutorRegistry
	statPointsService          stats.Service
	probes                     ProbeLookup
	logger                     *zap.SugaredLogger
}

func NewMonitorService(
	monitorRepository MonitorRepository,
	transactor dbtx.Transactor,
	heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	monitorNotificationService monitor_notification.Service,
	monitorTagService monitor_tag.Service,
	executorRegistry *executor.ExecutorRegistry,
	statPointsService stats.Service,
	probes ProbeLookup,
	logger *zap.SugaredLogger,
) Service {
	return &MonitorServiceImpl{
		monitorRepository,
		transactor,
		heartbeatService,
		eventBus,
		monitorNotificationService,
		monitorTagService,
		executorRegistry,
		statPointsService,
		probes,
		logger.Named("[monitor-service]"),
	}
}

func (mr *MonitorServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, monitorCreateDto *CreateUpdateDto) (*Model, error) {
	lowerLinkIDs(monitorCreateDto)
	createModel := &Model{
		WorkspaceID:    workspaceID,
		Type:           monitorCreateDto.Type,
		Name:           monitorCreateDto.Name,
		Interval:       monitorCreateDto.Interval,
		Timeout:        monitorCreateDto.Timeout,
		MaxRetries:     monitorCreateDto.MaxRetries,
		RetryInterval:  monitorCreateDto.RetryInterval,
		ResendInterval: monitorCreateDto.ResendInterval,
		Active:         monitorCreateDto.Active,
		Status:         shared.MonitorStatusPending,
		CreatedAt:      time.Now().UTC(),
		Config:         monitorCreateDto.Config,
		ProxyId:        monitorCreateDto.ProxyId,
		PushToken:      monitorCreateDto.PushToken,
		ProbeIDs:       monitorCreateDto.ProbeIDs,

		LocationRule:      locationRuleOrAny(monitorCreateDto.LocationRule),
		LocationThreshold: monitorCreateDto.LocationThreshold,
	}

	// A monitor created without probes runs on the local one
	probes := 1
	if monitorCreateDto.ProbeIDs != nil {
		if err := mr.validateProbes(ctx, monitorCreateDto.Type, monitorCreateDto.ProbeIDs); err != nil {
			return nil, err
		}
		probes = len(monitorCreateDto.ProbeIDs)
	}
	if err := validateLocationRule(createModel.LocationRule, createModel.LocationThreshold, probes); err != nil {
		return nil, err
	}

	// The monitor and its links are stored together or not at all, and the event comes after
	// they are committed, so a listener that reads the monitor's links finds them.
	var createdModel *Model
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		var err error
		// The channels, tags and proxy the monitor names must be its workspace's, and all of them
		// are checked before the monitor or a link is written
		if err = mr.requireLinks(ctx, workspaceID, monitorCreateDto.NotificationIds, monitorCreateDto.TagIds, monitorCreateDto.ProxyId); err != nil {
			return err
		}
		createdModel, err = mr.monitorRepository.Create(ctx, workspaceID, createModel)
		if err != nil {
			return err
		}
		return mr.createLinks(ctx, createdModel.ID, monitorCreateDto.NotificationIds, monitorCreateDto.TagIds)
	})
	if err != nil {
		return nil, err
	}

	// Emit monitor created event
	mr.publishMonitor(ctx, events.MonitorCreated, createdModel)

	return createdModel, nil
}

func (mr *MonitorServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return mr.monitorRepository.FindByID(ctx, workspaceID, id)
}

func (mr *MonitorServiceImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return mr.monitorRepository.FindByIDAnyWorkspace(ctx, id)
}

func (mr *MonitorServiceImpl) FindByIDs(ctx context.Context, workspaceID workspace.ID, ids []string) ([]*Model, error) {
	return mr.monitorRepository.FindByIDs(ctx, workspaceID, ids)
}

func (mr *MonitorServiceImpl) FindByIDsAnyWorkspace(ctx context.Context, ids []string) ([]*Model, error) {
	return mr.monitorRepository.FindByIDsAnyWorkspace(ctx, ids)
}

func (mr *MonitorServiceImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*Model, error) {
	monitors, err := mr.monitorRepository.FindAll(ctx, workspaceID, page, limit, q, active, status, tagIds)
	if err != nil {
		return nil, err
	}

	return monitors, nil
}

func (mr *MonitorServiceImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return mr.monitorRepository.FindAllAnyWorkspace(ctx, page, limit)
}

func (mr *MonitorServiceImpl) FindActiveAnyWorkspace(ctx context.Context) ([]*Model, error) {
	return mr.monitorRepository.FindActiveAnyWorkspace(ctx)
}

func (mr *MonitorServiceImpl) FindActivePaginatedAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return mr.monitorRepository.FindActivePaginatedAnyWorkspace(ctx, page, limit)
}

func (mr *MonitorServiceImpl) FindActiveByProbeAnyWorkspace(ctx context.Context, probeID string, page int, limit int) ([]*Model, error) {
	return mr.monitorRepository.FindActiveByProbeAnyWorkspace(ctx, probeID, page, limit)
}

func (mr *MonitorServiceImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, monitor *CreateUpdateDto) (*Model, error) {
	lowerLinkIDs(monitor)
	model := &Model{
		ID:             id,
		Name:           monitor.Name,
		Type:           monitor.Type,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		UpdatedAt:      time.Now().UTC(),
		Config:         monitor.Config,
		ProxyId:        monitor.ProxyId,
		PushToken:      monitor.PushToken,
		ProbeIDs:       monitor.ProbeIDs,
	}

	// The monitor and its links change together or not at all. The monitor is read before the
	// update, to check its probes against the type it will have, and back after it: the response
	// and the event carry what is stored, including its status and created_at.
	var updatedMonitor *Model
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		current, err := mr.FindByID(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		if err := mr.validateFullProbes(ctx, current, monitor); err != nil {
			return err
		}
		// The rule is checked against the probes the monitor will have, and a request without one
		// keeps the monitor's
		model.LocationRule, model.LocationThreshold, err = validateFullLocationRule(current, monitor)
		if err != nil {
			return err
		}
		if err := mr.requireLinks(ctx, workspaceID, monitor.NotificationIds, monitor.TagIds, monitor.ProxyId); err != nil {
			return err
		}
		if err := mr.monitorRepository.UpdateFull(ctx, workspaceID, id, model); err != nil {
			return err
		}
		if err := mr.replaceLinks(ctx, id, monitor.NotificationIds, monitor.TagIds); err != nil {
			return err
		}
		updatedMonitor, err = mr.FindByID(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if updatedMonitor == nil {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Emit monitor updated event, once the update is committed
	mr.publishMonitor(ctx, events.MonitorUpdated, updatedMonitor)

	return updatedMonitor, nil
}

func (mr *MonitorServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, monitor *PartialUpdateDto, noPublish bool) (*Model, error) {
	return mr.updatePartial(ctx, &workspaceID, id, monitor, noPublish)
}

func (mr *MonitorServiceImpl) UpdatePartialAnyWorkspace(ctx context.Context, id string, monitor *PartialUpdateDto, noPublish bool) (*Model, error) {
	return mr.updatePartial(ctx, nil, id, monitor, noPublish)
}

// find reads the monitor in the workspace, or across workspaces when there is none. A monitor of
// another workspace is not found.
func (mr *MonitorServiceImpl) find(ctx context.Context, workspaceID *workspace.ID, id string) (*Model, error) {
	if workspaceID == nil {
		return mr.monitorRepository.FindByIDAnyWorkspace(ctx, id)
	}
	return mr.monitorRepository.FindByID(ctx, *workspaceID, id)
}

// updatePartial is UpdatePartial in the workspace, or across workspaces when there is none
func (mr *MonitorServiceImpl) updatePartial(ctx context.Context, workspaceID *workspace.ID, id string, monitor *PartialUpdateDto, noPublish bool) (*Model, error) {
	monitor.NotificationIds = utils.LowerUUIDs(monitor.NotificationIds)
	monitor.TagIds = utils.LowerUUIDs(monitor.TagIds)
	if monitor.ProxyId != nil {
		lowered := utils.LowerUUID(*monitor.ProxyId)
		monitor.ProxyId = &lowered
	}
	model := &UpdateModel{
		ID:             &id,
		Type:           monitor.Type,
		Name:           monitor.Name,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		Status:         monitor.Status,
		Config:         monitor.Config,
		ProxyId:        monitor.ProxyId,
		PushToken:      monitor.PushToken,
		ProbeIDs:       monitor.ProbeIDs,
	}

	// The monitor and its links change together or not at all. The monitor is read inside the
	// transaction, so that what the update is checked against is what it then changes. Links
	// change only when IDs are sent: a request without them leaves the links as they are.
	var updatedMonitor *Model
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		current, err := mr.find(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		if err := mr.validatePartialConfig(current, monitor); err != nil {
			return err
		}
		if err := validatePartialTimeout(current, monitor); err != nil {
			return err
		}
		if err := mr.validatePartialProbes(ctx, current, monitor); err != nil {
			return err
		}
		// The rule is checked against the probes the monitor will have, so an update that only
		// removes probes is checked too
		if err := validatePartialLocationRule(current, monitor); err != nil {
			return err
		}
		if rule, threshold, set := partialLocationRule(current, monitor); set {
			model.LocationRule, model.LocationThreshold = &rule, threshold
		}
		// A background update changes a status and never links, so only a request's can name
		// records of another workspace
		if workspaceID != nil {
			proxyID := ""
			if monitor.ProxyId != nil {
				proxyID = *monitor.ProxyId
			}
			if err := mr.requireLinks(ctx, *workspaceID, monitor.NotificationIds, monitor.TagIds, proxyID); err != nil {
				return err
			}
		}
		if workspaceID == nil {
			err = mr.monitorRepository.UpdatePartialAnyWorkspace(ctx, id, model)
		} else {
			err = mr.monitorRepository.UpdatePartial(ctx, *workspaceID, id, model)
		}
		if err != nil {
			return err
		}
		if len(monitor.NotificationIds) > 0 {
			if err := mr.syncNotificationLinks(ctx, id, monitor.NotificationIds); err != nil {
				return err
			}
		}
		if len(monitor.TagIds) > 0 {
			if err := mr.syncTagLinks(ctx, id, monitor.TagIds); err != nil {
				return err
			}
		}
		updatedMonitor, err = mr.find(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if updatedMonitor == nil {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Emit monitor updated event, once the update is committed
	if !noPublish {
		mr.publishMonitor(ctx, events.MonitorUpdated, updatedMonitor)
	}

	return updatedMonitor, nil
}

// validatePartialConfig checks that the type and config the monitor has after the update fit
// together. An update that changes neither leaves them unchecked: a status change, or a rename,
// doesn't depend on them.
func (mr *MonitorServiceImpl) validatePartialConfig(current *Model, update *PartialUpdateDto) error {
	if update.Type == nil && update.Config == nil {
		return nil
	}
	monitorType, config := current.Type, current.Config
	if update.Type != nil {
		monitorType = *update.Type
	}
	if update.Config != nil {
		config = *update.Config
	}
	if err := mr.ValidateMonitorConfig(monitorType, config); err != nil {
		return &InvalidConfigError{Err: err}
	}
	return nil
}

// validatePartialTimeout checks that the timeout is at most 0.8 x the check interval the monitor
// has after the update. An update that sends neither leaves them unchecked, so a monitor stored
// with a longer timeout can still be paused or renamed.
func validatePartialTimeout(current *Model, update *PartialUpdateDto) error {
	if update.Interval == nil && update.Timeout == nil {
		return nil
	}
	interval, timeout := current.Interval, current.Timeout
	if update.Interval != nil {
		interval = *update.Interval
	}
	if update.Timeout != nil {
		timeout = *update.Timeout
	}
	if timeoutTooLong(timeout, interval) {
		return &InvalidTimeoutError{Timeout: timeout, Interval: interval}
	}
	return nil
}

func (mr *MonitorServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	// The monitor goes with its links, heartbeats and stats, or stays with all of them
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		current, err := mr.FindByID(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		if err := mr.heartbeatService.DeleteByMonitorID(ctx, id); err != nil {
			return fmt.Errorf("delete heartbeats: %w", err)
		}
		if err := mr.statPointsService.DeleteByMonitorID(ctx, id); err != nil {
			return fmt.Errorf("delete stats: %w", err)
		}
		if err := mr.monitorRepository.Delete(ctx, workspaceID, id); err != nil {
			return err
		}
		if err := mr.monitorNotificationService.DeleteByMonitorID(ctx, id); err != nil {
			return fmt.Errorf("delete notification channel links: %w", err)
		}
		if err := mr.monitorTagService.DeleteByMonitorID(ctx, id); err != nil {
			return fmt.Errorf("delete tag links: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Emit monitor deleted event, once the delete is committed
	if err := events.Publish(ctx, mr.eventBus, events.MonitorDeleted, id); err != nil {
		mr.logger.Errorw("Failed to publish monitor deleted event", "monitor_id", id, "error", err)
	}

	return nil
}

// requireLinks checks that the notification channels, tags and proxy a request names are in the
// workspace, and fails on the first that isn't. A monitor links only to records of its own
// workspace (docs/adr/0005 section 2), and a record of another workspace is refused as one that
// doesn't exist (section 8): the request fails with a 400 that names the kind.
// Callers check before they write anything, so a refusal changes nothing. An empty proxy ID is no
// proxy.
func (mr *MonitorServiceImpl) requireLinks(ctx context.Context, workspaceID workspace.ID, notificationIDs, tagIDs []string, proxyID string) error {
	if err := mr.monitorNotificationService.RequireChannels(ctx, workspaceID, distinct(notificationIDs)); err != nil {
		return fmt.Errorf("link notification channel: %w", err)
	}
	if err := mr.monitorTagService.RequireTags(ctx, workspaceID, distinct(tagIDs)); err != nil {
		return fmt.Errorf("link tag: %w", err)
	}
	if proxyID == "" {
		return nil
	}
	// An ID that isn't a UUID is no proxy, and PostgreSQL fails the query on it instead of finding
	// nothing. The query takes the lower case form, as the channels' and tags' do.
	parsed, err := uuid.Parse(proxyID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrProxyNotFound, proxyID)
	}
	found, err := mr.monitorRepository.ProxyInWorkspace(ctx, workspaceID, parsed.String())
	if err != nil {
		return fmt.Errorf("find proxy: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrProxyNotFound, proxyID)
	}
	return nil
}

// createLinks links the monitor to each notification channel and tag, once for each ID however
// often the request repeats it. The first link that can't be saved fails the call; the caller's
// transaction decides what that undoes.
func (mr *MonitorServiceImpl) createLinks(ctx context.Context, monitorID string, notificationIDs, tagIDs []string) error {
	for _, notificationID := range distinct(notificationIDs) {
		if _, err := mr.monitorNotificationService.Create(ctx, monitorID, notificationID); err != nil {
			return fmt.Errorf("link notification channel %s: %w", notificationID, err)
		}
	}
	for _, tagID := range distinct(tagIDs) {
		if _, err := mr.monitorTagService.Create(ctx, monitorID, tagID); err != nil {
			return fmt.Errorf("link tag %s: %w", tagID, err)
		}
	}
	return nil
}

// lowerLinkIDs writes the IDs of the channels, tags and proxy a request names in lower case, which
// is how they are stored whatever case the request used.
func lowerLinkIDs(dto *CreateUpdateDto) {
	dto.NotificationIds = utils.LowerUUIDs(dto.NotificationIds)
	dto.TagIds = utils.LowerUUIDs(dto.TagIds)
	dto.ProxyId = utils.LowerUUID(dto.ProxyId)
}

// distinct returns the IDs without repeats, in the order they first appear.
func distinct(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

// replaceLinks unlinks the monitor from all its notification channels and tags and links it
// to the given ones.
func (mr *MonitorServiceImpl) replaceLinks(ctx context.Context, monitorID string, notificationIDs, tagIDs []string) error {
	if err := mr.monitorNotificationService.DeleteByMonitorID(ctx, monitorID); err != nil {
		return fmt.Errorf("unlink notification channels: %w", err)
	}
	if err := mr.monitorTagService.DeleteByMonitorID(ctx, monitorID); err != nil {
		return fmt.Errorf("unlink tags: %w", err)
	}
	return mr.createLinks(ctx, monitorID, notificationIDs, tagIDs)
}

// syncNotificationLinks makes the given notification channels the monitor's only ones: it
// unlinks the others and links the new ones, and leaves the links that stay as they are.
func (mr *MonitorServiceImpl) syncNotificationLinks(ctx context.Context, monitorID string, notificationIDs []string) error {
	existing, err := mr.monitorNotificationService.FindByMonitorID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("find notification channel links: %w", err)
	}
	wanted := make(map[string]bool, len(notificationIDs))
	for _, notificationID := range notificationIDs {
		wanted[notificationID] = true
	}

	linked := make(map[string]bool, len(notificationIDs))
	for _, link := range existing {
		if wanted[link.NotificationID] {
			linked[link.NotificationID] = true
			continue
		}
		if err := mr.monitorNotificationService.Delete(ctx, link.ID); err != nil {
			return fmt.Errorf("unlink notification channel %s: %w", link.NotificationID, err)
		}
	}
	for _, notificationID := range notificationIDs {
		if linked[notificationID] {
			continue
		}
		if _, err := mr.monitorNotificationService.Create(ctx, monitorID, notificationID); err != nil {
			return fmt.Errorf("link notification channel %s: %w", notificationID, err)
		}
		linked[notificationID] = true
	}
	return nil
}

// syncTagLinks is syncNotificationLinks for tags.
func (mr *MonitorServiceImpl) syncTagLinks(ctx context.Context, monitorID string, tagIDs []string) error {
	existing, err := mr.monitorTagService.FindByMonitorID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("find tag links: %w", err)
	}
	wanted := make(map[string]bool, len(tagIDs))
	for _, tagID := range tagIDs {
		wanted[tagID] = true
	}

	linked := make(map[string]bool, len(tagIDs))
	for _, link := range existing {
		if wanted[link.TagID] {
			linked[link.TagID] = true
			continue
		}
		if err := mr.monitorTagService.Delete(ctx, link.ID); err != nil {
			return fmt.Errorf("unlink tag %s: %w", link.TagID, err)
		}
	}
	for _, tagID := range tagIDs {
		if linked[tagID] {
			continue
		}
		if _, err := mr.monitorTagService.Create(ctx, monitorID, tagID); err != nil {
			return fmt.Errorf("link tag %s: %w", tagID, err)
		}
		linked[tagID] = true
	}
	return nil
}

// publishMonitor tells the other parts of the program, in this process and the others,
// that a monitor was created or changed. A failure is logged: the change is already stored.
func (mr *MonitorServiceImpl) publishMonitor(ctx context.Context, topic events.Topic[shared.Monitor], mon *Model) {
	if mon == nil {
		return
	}
	if err := events.Publish(ctx, mr.eventBus, topic, *mon); err != nil {
		mr.logger.Errorw("Failed to publish monitor event", "topic", topic.Name(), "monitor_id", mon.ID, "error", err)
	}
}

func (mr *MonitorServiceImpl) ValidateMonitorConfig(
	monitorType string,
	configJSON string,
) error {
	if mr.executorRegistry == nil {
		return fmt.Errorf("executor registry not available")
	}
	return mr.executorRegistry.ValidateConfig(monitorType, configJSON)
}

// requireMonitor loads the monitor in the workspace, and is ErrMonitorNotFound when it isn't
// there. Every read of a monitor's heartbeats or stats goes through it first.
func (mr *MonitorServiceImpl) requireMonitor(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	monitor, err := mr.monitorRepository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, &LookupError{Err: err}
	}
	if monitor == nil {
		return nil, fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
	}
	return monitor, nil
}

func (mr *MonitorServiceImpl) GetHeartbeats(ctx context.Context, workspaceID workspace.ID, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	if _, err := mr.requireMonitor(ctx, workspaceID, id); err != nil {
		return nil, err
	}
	return mr.heartbeatService.FindByMonitorIDAndProbeIDPaginated(ctx, id, probeID, limit, page, important, reverse)
}

func (mr *MonitorServiceImpl) GetHeartbeatsAnyWorkspace(ctx context.Context, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	return mr.heartbeatService.FindByMonitorIDAndProbeIDPaginated(ctx, id, probeID, limit, page, important, reverse)
}

func (mr *MonitorServiceImpl) RemoveProxyReference(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error) {
	var changed []*Model
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		using, err := mr.monitorRepository.FindByProxyId(ctx, workspaceID, proxyId)
		if err != nil || len(using) == 0 {
			return err
		}
		if err := mr.monitorRepository.RemoveProxyReference(ctx, workspaceID, proxyId); err != nil {
			return err
		}
		for _, m := range using {
			updated, err := mr.monitorRepository.FindByID(ctx, workspaceID, m.ID)
			if err != nil {
				return err
			}
			if updated != nil {
				changed = append(changed, updated)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func (mr *MonitorServiceImpl) PublishUpdated(ctx context.Context, monitors []*Model) {
	for _, m := range monitors {
		mr.publishMonitor(ctx, events.MonitorUpdated, m)
	}
}

func (mr *MonitorServiceImpl) FindByProxyId(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error) {
	return mr.monitorRepository.FindByProxyId(ctx, workspaceID, proxyId)
}

func (mr *MonitorServiceImpl) GetStatPoints(ctx context.Context, workspaceID workspace.ID, id, probeID string, since, until time.Time, granularity string) (*StatPointsSummaryDto, error) {
	var period stats.StatPeriod
	switch granularity {
	case "minute":
		period = stats.StatMinutely
	case "hour":
		period = stats.StatHourly
	case "day":
		period = stats.StatDaily
	default:
		return nil, fmt.Errorf("invalid granularity: %s", granularity)
	}

	// Get monitor information to pass interval for minute-level grouping
	monitor, err := mr.requireMonitor(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}

	// Use the new method that accepts monitor interval
	statsList, err := mr.statPointsService.FindStatsByMonitorIDAndTimeRangeWithInterval(ctx, id, probeID, since, until, period, monitor.Interval)
	if err != nil {
		return nil, err
	}

	points := make([]*StatPoint, 0, len(statsList))
	for _, s := range statsList {
		points = append(points, &StatPoint{
			Up:          s.Up,
			Down:        s.Down,
			Maintenance: s.Maintenance,
			Ping:        s.Ping,
			PingMin:     s.PingMin,
			PingMax:     s.PingMax,
			Timestamp:   s.Timestamp.Unix() * 1000,
		})
	}

	stats := mr.statPointsService.StatPointsSummary(statsList)

	return &StatPointsSummaryDto{
		ProbeID: probeID,
		Points:  points,
		MaxPing: stats.MaxPing,
		MinPing: stats.MinPing,
		AvgPing: stats.AvgPing,
		Uptime:  stats.Uptime,
	}, nil
}

// GetUptimeStats returns uptime percentages for 24h, 7d, 30d and 365d. A period with no Up or
// Down heartbeat is nil, not 0.
func (mr *MonitorServiceImpl) GetUptimeStats(ctx context.Context, workspaceID workspace.ID, id, probeID string) (*CustomUptimeStatsDto, error) {
	if _, err := mr.requireMonitor(ctx, workspaceID, id); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	periods := map[string]time.Duration{
		"24h":  24 * time.Hour,
		"7d":   7 * 24 * time.Hour,
		"30d":  30 * 24 * time.Hour,
		"365d": 365 * 24 * time.Hour,
	}

	uptimes := make(map[string]*float64)
	for key, duration := range periods {
		since := now.Add(-duration)
		summary, err := mr.statPointsService.Summarize(ctx, id, probeID, since, now)
		if err != nil {
			return nil, err
		}
		uptimes[key] = summary.Uptime
	}

	stats := &CustomUptimeStatsDto{
		ProbeID:    probeID,
		Uptime24h:  uptimes["24h"],
		Uptime7d:   uptimes["7d"],
		Uptime30d:  uptimes["30d"],
		Uptime365d: uptimes["365d"],
	}

	return stats, nil
}

func (mr *MonitorServiceImpl) GetUptime24hAnyWorkspace(ctx context.Context, id, probeID string) (*float64, error) {
	now := time.Now().UTC()
	summary, err := mr.statPointsService.Summarize(ctx, id, probeID, now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	return summary.Uptime, nil
}

func (mr *MonitorServiceImpl) FindOneByPushTokenAnyWorkspace(ctx context.Context, pushToken string) (*Model, error) {
	return mr.monitorRepository.FindOneByPushTokenAnyWorkspace(ctx, pushToken)
}

// assignPublishChunk is how many monitors AssignProbeToAll reads back at a time to announce
const assignPublishChunk = 500

// MARK: AssignProbeToAllAnyWorkspace
func (mr *MonitorServiceImpl) AssignProbeToAllAnyWorkspace(ctx context.Context, probeID string, supportedTypes []string, claim func(ctx context.Context) (bool, error)) (int, error) {
	var changed []string
	err := mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		if claim != nil {
			claimed, err := claim(ctx)
			if err != nil || !claimed {
				return err
			}
		}
		// One look at the probe stands in for the check of each monitor that an update makes: the
		// repository only picks monitors of the types the probe runs
		info, err := mr.findProbe(ctx, probeID)
		if err != nil {
			return err
		}
		if info == nil {
			return &InvalidProbesError{Err: fmt.Errorf("probe %q doesn't exist", probeID)}
		}
		changed, err = mr.monitorRepository.AddProbeToAllAnyWorkspace(ctx, probeID, supportedTypes)
		return err
	})
	if err != nil {
		return 0, err
	}

	// Emit monitor updated events, once the assignment is committed. A monitor that can't be read
	// back is logged: the assignment is stored, and the claim that made it is spent.
	for _, ids := range chunked(changed, assignPublishChunk) {
		monitors, err := mr.FindByIDsAnyWorkspace(ctx, ids)
		if err != nil {
			mr.logger.Errorw("Failed to read the monitors a probe was assigned", "probe_id", probeID, "error", err)
			continue
		}
		for _, m := range monitors {
			mr.publishMonitor(ctx, events.MonitorUpdated, m)
		}
	}
	return len(changed), nil
}

func (mr *MonitorServiceImpl) ResetMonitorData(ctx context.Context, workspaceID workspace.ID, id string) error {
	// First check that the monitor exists in the workspace
	if _, err := mr.requireMonitor(ctx, workspaceID, id); err != nil {
		return err
	}

	// Delete all heartbeats for this monitor
	err := mr.heartbeatService.DeleteByMonitorID(ctx, id)
	if err != nil {
		mr.logger.Errorw("Failed to delete heartbeats for monitor", "monitorID", id, "error", err)
		return fmt.Errorf("failed to delete heartbeats: %w", err)
	}

	// Delete all stats for this monitor
	err = mr.statPointsService.DeleteByMonitorID(ctx, id)
	if err != nil {
		mr.logger.Errorw("Failed to delete stats for monitor", "monitorID", id, "error", err)
		return fmt.Errorf("failed to delete stats: %w", err)
	}

	// Forget where the monitor stood on each probe, so that its next result is its first
	if err := mr.monitorRepository.ResetLocations(ctx, workspaceID, id); err != nil {
		mr.logger.Errorw("Failed to reset the locations of monitor", "monitorID", id, "error", err)
		return fmt.Errorf("failed to reset locations: %w", err)
	}

	// Reset monitor status to pending (like a fresh monitor)
	pendingStatus := shared.MonitorStatusPending
	err = mr.monitorRepository.UpdatePartial(ctx, workspaceID, id, &UpdateModel{
		ID:     &id,
		Status: &pendingStatus,
	})
	if err != nil {
		mr.logger.Errorw("Failed to reset monitor status", "monitorID", id, "error", err)
		return fmt.Errorf("failed to reset monitor status: %w", err)
	}

	mr.logger.Infow("Successfully reset monitor data", "monitorID", id)

	// Emit monitor updated event
	updatedMonitor, _ := mr.FindByID(ctx, workspaceID, id)
	if updatedMonitor != nil {
		mr.publishMonitor(ctx, events.MonitorUpdated, updatedMonitor)
	} else {
		mr.logger.Errorw("Failed to find updated monitor", "monitorID", id)
	}

	return nil
}

// RepairStoredStatusesAnyWorkspace gives every monitor, paused ones too, the status of its latest heartbeat,
// and returns how many it changed. A monitor without heartbeats is left alone. A monitor that
// fails is logged and skipped, and the errors come back joined. A cancelled ctx ends the sweep
// at once with ctx.Err() among them.
func (mr *MonitorServiceImpl) RepairStoredStatusesAnyWorkspace(ctx context.Context) (int, error) {
	repaired := 0
	var failures []error

	for page := 0; ; page++ {
		if err := ctx.Err(); err != nil {
			return repaired, errors.Join(append(failures, err)...)
		}
		monitors, err := mr.monitorRepository.FindAllAnyWorkspace(ctx, page, StatusRepairPageSize)
		if err != nil {
			mr.logger.Errorw("Failed to list monitors to repair their status", "page", page, "error", err)
			return repaired, errors.Join(append(failures, fmt.Errorf("list monitors, page %d: %w", page, err))...)
		}

		for _, m := range monitors {
			if err := ctx.Err(); err != nil {
				return repaired, errors.Join(append(failures, err)...)
			}
			changed, err := mr.repairStoredStatus(ctx, m)
			if err != nil {
				mr.logger.Errorw("Failed to repair monitor status", "monitorID", m.ID, "error", err)
				failures = append(failures, fmt.Errorf("monitor %s: %w", m.ID, err))
				continue
			}
			if changed {
				repaired++
			}
		}

		if len(monitors) < StatusRepairPageSize {
			return repaired, errors.Join(failures...)
		}
	}
}

// repairStoredStatus stores the status of the monitor's latest heartbeat when it differs. It writes
// through the repository so no event is published: the monitor did not just change status.
func (mr *MonitorServiceImpl) repairStoredStatus(ctx context.Context, m *Model) (bool, error) {
	latest, err := mr.heartbeatService.FindByMonitorIDPaginated(ctx, m.ID, 1, 0, nil, false)
	if err != nil {
		return false, err
	}
	if len(latest) == 0 || latest[0].Status == m.Status {
		return false, nil
	}

	status := latest[0].Status
	if err := mr.monitorRepository.UpdatePartialAnyWorkspace(ctx, m.ID, &UpdateModel{ID: &m.ID, Status: &status}); err != nil {
		return false, err
	}
	mr.logger.Infow("Repaired monitor status", "monitorID", m.ID, "from", m.Status, "to", status)
	return true, nil
}
