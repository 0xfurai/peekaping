package monitor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"strings"
	"time"

	"go.uber.org/zap"
)

type MonitorController struct {
	monitorService             Service
	logger                     *zap.SugaredLogger
	monitorNotificationService monitor_notification.Service
	monitorTagService          monitor_tag.Service
	tlsInfoService             monitor_tls_info.Service
	staleStore                 stale.Store
}

func NewMonitorController(
	monitorService Service,
	logger *zap.SugaredLogger,
	monitorNotificationService monitor_notification.Service,
	monitorTagService monitor_tag.Service,
	tlsInfoService monitor_tls_info.Service,
	staleStore stale.Store,
) *MonitorController {
	validation.Validate.RegisterStructValidation(CreateUpdateDtoStructLevelValidation, CreateUpdateDto{})

	return &MonitorController{
		monitorService,
		logger,
		monitorNotificationService,
		monitorTagService,
		tlsInfoService,
		staleStore,
	}
}

// staleMonitors returns when each stale monitor was last checked. If Redis can't be read
// it logs and returns none: the monitors are still worth showing.
func (ic *MonitorController) staleMonitors(ctx context.Context) map[string]stale.Entry {
	if ic.staleStore == nil {
		return nil
	}
	entries, err := ic.staleStore.All(ctx)
	if err != nil {
		ic.logger.Warnw("Failed to read stale monitors", "error", err)
		return nil
	}
	return entries
}

// withLinks turns monitors into API responses, each with the IDs of the notification channels and
// tags linked to it. Every endpoint that returns a monitor answers with these.
func (ic *MonitorController) withLinks(ctx context.Context, monitors []*Model) ([]*MonitorResponseDto, error) {
	if monitors == nil {
		return nil, nil
	}
	responses := make([]*MonitorResponseDto, len(monitors))
	if len(monitors) == 0 {
		return responses, nil
	}

	// One query for each kind of link, whatever the number of monitors
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	notificationRels, err := ic.monitorNotificationService.FindByMonitorIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("fetch monitor-notification relations: %w", err)
	}
	tagRels, err := ic.monitorTagService.FindByMonitorIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("fetch monitor-tag relations: %w", err)
	}

	for i, m := range monitors {
		notificationIds := make([]string, 0, len(notificationRels[m.ID]))
		for _, rel := range notificationRels[m.ID] {
			notificationIds = append(notificationIds, rel.NotificationID)
		}
		tagIds := make([]string, 0, len(tagRels[m.ID]))
		for _, rel := range tagRels[m.ID] {
			tagIds = append(tagIds, rel.TagID)
		}
		responses[i] = &MonitorResponseDto{Monitor: *m, TypeDisabled: m.TypeDisabled, NotificationIds: notificationIds, TagIds: tagIds}
	}
	return responses, nil
}

// withLinksOf is withLinks for one monitor.
func (ic *MonitorController) withLinksOf(ctx context.Context, monitor *Model) (*MonitorResponseDto, error) {
	responses, err := ic.withLinks(ctx, []*Model{monitor})
	if err != nil {
		return nil, err
	}
	return responses[0], nil
}

func (ic *MonitorController) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
	}

	limit, err := utils.GetQueryInt(r, "limit", 10)
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
	}

	q := r.URL.Query().Get("q")

	active, err := utils.GetQueryBool(r, "active")
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid active parameter (must be true or false)"))
	}

	var statusPtr *int
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		statusVal, err := utils.GetQueryInt(r, "status", 0)
		if err != nil {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid status parameter (must be int)"))
		}
		statusPtr = &statusVal
	}

	// Parse tag_ids parameter
	var tagIds []string
	if tagIdsStr := r.URL.Query().Get("tag_ids"); tagIdsStr != "" {
		tagIds = strings.Split(tagIdsStr, ",")
		// Trim whitespace from each tag ID
		for i, tagId := range tagIds {
			tagIds[i] = strings.TrimSpace(tagId)
		}
		// Remove empty strings
		var validTagIds []string
		for _, tagId := range tagIds {
			if tagId != "" {
				validTagIds = append(validTagIds, tagId)
			}
		}
		tagIds = validTagIds
	}

	response, err := ic.monitorService.FindAll(ctx, workspaceID, page, limit, q, active, statusPtr, tagIds)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitors", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	staleMonitors := ic.staleMonitors(ctx)
	for _, m := range response {
		if entry, ok := staleMonitors[m.ID]; ok {
			m.StaleSince = &entry.Since
		}
	}

	monitors, err := ic.withLinks(ctx, response)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor links", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", monitors))
}
func (ic *MonitorController) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	monitor, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	monitor.ApplyCreateDefaults()

	if err := validation.Validate.Struct(monitor); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", err)))
	}

	createdMonitor, err := ic.monitorService.Create(ctx, workspaceID, monitor)
	if err != nil {
		var invalidProbes *InvalidProbesError
		var invalidRule *InvalidLocationRuleError
		var tokenTaken *PushTokenAlreadyUsedError
		var typeDisabled *MonitorTypeDisabledError
		switch {
		case errors.As(err, &typeDisabled):
			return nil, monitorTypeDisabledResponse(typeDisabled)
		case errors.As(err, &invalidProbes):
			return nil, invalidProbesResponse(invalidProbes)
		case errors.As(err, &invalidRule):
			return nil, invalidLocationRuleResponse(invalidRule)
		case errors.As(err, &tokenTaken):
			return nil, pushTokenTakenResponse(tokenTaken)
		}
		if linkErr := linkNotFoundResponse(err); linkErr != nil {
			return nil, linkErr
		}
		ic.logger.Errorw("Failed to create monitor", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	// Only the ID and type: the monitor's config holds credentials, and so does its push token
	ic.logger.Infof("Created monitor: %s (type: %s)", createdMonitor.ID, createdMonitor.Type)

	// The monitor and its links are committed, and a transaction that failed on a link would have
	// returned an error above, so the links are exactly the IDs of the request. The response is
	// built from them without a read: one that failed would answer 500 for a monitor that exists,
	// and a client sending the POST again would create a second one.
	response := &MonitorResponseDto{
		Monitor:         *createdMonitor,
		TypeDisabled:    createdMonitor.TypeDisabled,
		NotificationIds: distinct(monitor.NotificationIds),
		TagIds:          distinct(monitor.TagIds),
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Monitor created successfully", response))
}
func (ic *MonitorController) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	monitor, err := ic.monitorService.FindByID(ctx, workspaceID, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	if monitor == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
	}

	if entry, ok := ic.staleMonitors(ctx)[monitor.ID]; ok {
		monitor.StaleSince = &entry.Since
	}

	response, err := ic.withLinksOf(ctx, monitor)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor links", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", response))
}
func (ic *MonitorController) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	monitor, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// validate
	if err := validation.Validate.Struct(monitor); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", err)))
	}

	updatedMonitor, err := ic.monitorService.UpdateFull(ctx, workspaceID, id, &monitor)
	if err != nil {
		var invalidProbes *InvalidProbesError
		var invalidRule *InvalidLocationRuleError
		var tokenTaken *PushTokenAlreadyUsedError
		var typeDisabled *MonitorTypeDisabledError
		switch {
		case errors.As(err, &typeDisabled):
			return nil, monitorTypeDisabledResponse(typeDisabled)
		case errors.As(err, &invalidProbes):
			return nil, invalidProbesResponse(invalidProbes)
		case errors.As(err, &invalidRule):
			return nil, invalidLocationRuleResponse(invalidRule)
		case errors.As(err, &tokenTaken):
			return nil, pushTokenTakenResponse(tokenTaken)
		}
		if linkErr := linkNotFoundResponse(err); linkErr != nil {
			return nil, linkErr
		}
		ic.logger.Errorw("Failed to update monitor", "error", err)
		if errors.Is(err, ErrMonitorNotFound) {
			return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse(err.Error()))
		}
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	response, err := ic.withLinksOf(ctx, updatedMonitor)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor links", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Monitor updated successfully", response))
}
func (ic *MonitorController) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	monitor, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(monitor); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	updatedMonitor, err := ic.monitorService.UpdatePartial(ctx, workspaceID, id, &monitor, false)
	if err != nil {
		return nil, ic.failWrite("update", err)
	}

	response, err := ic.withLinksOf(ctx, updatedMonitor)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor links", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Monitor updated successfully", response))
}

// failWrite maps the error of a write that looks up the monitor itself to its response.
func (ic *MonitorController) failWrite(action string, err error) error {
	var invalidConfig *InvalidConfigError
	var invalidTimeout *InvalidTimeoutError
	var invalidProbes *InvalidProbesError
	var invalidRule *InvalidLocationRuleError
	var tokenTaken *PushTokenAlreadyUsedError
	var typeDisabled *MonitorTypeDisabledError
	switch {
	case errors.Is(err, ErrMonitorNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
	case errors.As(err, &invalidConfig):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", invalidConfig.Err)))
	case errors.As(err, &invalidTimeout):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid timeout: %v", invalidTimeout)))
	case errors.As(err, &invalidProbes):
		return invalidProbesResponse(invalidProbes)
	case errors.As(err, &invalidRule):
		return invalidLocationRuleResponse(invalidRule)
	case errors.As(err, &tokenTaken):
		return pushTokenTakenResponse(tokenTaken)
	case errors.As(err, &typeDisabled):
		return monitorTypeDisabledResponse(typeDisabled)
	}
	if linkErr := linkNotFoundResponse(err); linkErr != nil {
		return linkErr
	}
	ic.logger.Errorw("Failed to "+action+" monitor", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// failLookup is the answer to the error of a read that loads the monitor first: 404 when the
// monitor isn't in the workspace and 500 when the read failed. It returns nil for any other error.
func (ic *MonitorController) failLookup(err error) error {
	var lookup *LookupError
	switch {
	case errors.Is(err, ErrMonitorNotFound):
		return monitorNotFoundResponse()
	case errors.As(err, &lookup):
		ic.logger.Errorw("Failed to find monitor", "error", err)
		return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return nil
}

// monitorNotFoundResponse is the answer for a monitor that doesn't exist and for one that belongs
// to another workspace, which a caller can't tell apart
func monitorNotFoundResponse() error {
	return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
}

// linkNotFoundResponse is the 400 for a request that links the monitor to a notification channel,
// tag or proxy that isn't in its workspace, and nil for any other error. The message names the kind
// and not the ID, so a record of another workspace and one that doesn't exist get the same answer
// (docs/adr/0005 section 8).
func linkNotFoundResponse(err error) error {
	switch {
	case errors.Is(err, monitor_notification.ErrUnknownChannel):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Notification channel not found"))
	case errors.Is(err, monitor_tag.ErrUnknownTag):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Tag not found"))
	case errors.Is(err, ErrProxyNotFound):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Proxy not found"))
	}
	return nil
}

// pushTokenTakenResponse tells the client that the push token it sent belongs to another monitor
func pushTokenTakenResponse(err *PushTokenAlreadyUsedError) error {
	return httpx.Fail(http.StatusBadRequest, PushTokenErrorResponse{Error: err})
}

// monitorTypeDisabledResponse tells the client that this install turns the monitor type off, so a
// monitor can't be given it
func monitorTypeDisabledResponse(err *MonitorTypeDisabledError) error {
	return httpx.Fail(http.StatusBadRequest, MonitorTypeDisabledResponse{Message: err.Error(), Error: err})
}

// invalidProbesResponse tells the client why the probes it sent don't fit the monitor
func invalidProbesResponse(err *InvalidProbesError) error {
	return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid probe_ids: %v", err.Err)))
}

func (ic *MonitorController) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	if err := ic.monitorService.Delete(ctx, workspaceID, id); err != nil {
		return nil, ic.failWrite("delete", err)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Monitor deleted successfully", nil))
}
func (ic *MonitorController) FindByMonitorIDPaginated(ctx context.Context, input *FindByMonitorIDPaginatedInput) (*httpx.Output[utils.ApiResponse[[]*heartbeat.Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	limit, err := utils.GetQueryInt(r, "limit", 50)
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter (1-1000)"))
	}

	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter (>=0)"))
	}

	var importantPtr *bool
	if r.URL.Query().Get("important") != "" {
		importantPtr, err = utils.GetQueryBool(r, "important")
		if err != nil {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid important parameter (must be true or false)"))
		}
	}

	reverse := false
	if r.URL.Query().Get("reverse") != "" {
		reversePtr, err := utils.GetQueryBool(r, "reverse")
		if err != nil {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid reverse parameter (must be true or false)"))
		}
		if reversePtr != nil {
			reverse = *reversePtr
		}
	}

	results, err := ic.monitorService.GetHeartbeats(ctx, workspaceID, id, r.URL.Query().Get("probe_id"), limit, page, importantPtr, reverse)
	if err != nil {
		if failure := ic.failLookup(err); failure != nil {
			return nil, failure
		}
		ic.logger.Errorw("Failed to get heartbeats", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", results))
}
func (ic *MonitorController) GetStatPoints(ctx context.Context, input *GetStatPointsInput) (*httpx.Output[utils.ApiResponse[*StatPointsSummaryDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Missing required 'since' parameter"))
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid 'since' parameter (must be RFC3339)"))
	}

	untilStr := r.URL.Query().Get("until")
	var until time.Time
	if untilStr == "" {
		until = time.Now().UTC()
	} else {
		until, err = time.Parse(time.RFC3339, untilStr)
		if err != nil {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid 'until' parameter (must be RFC3339)"))
		}
	}

	granularity := httpx.DefaultQuery(r, "granularity", "minute")

	var interval time.Duration
	switch granularity {
	case "minute":
		interval = time.Minute
	case "hour":
		interval = time.Hour
	case "day":
		interval = 24 * time.Hour
	default:
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid 'granularity' parameter (must be minute, hour, or day)"))
	}

	if until.Before(since) {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("'until' must be after 'since'"))
	}

	diff := until.Sub(since)
	estPoints := int(diff/interval) + 1
	if estPoints > 1441 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Too many points requested: %d (max 1441)", estPoints)))
	}

	summary, err := ic.monitorService.GetStatPoints(ctx, workspaceID, id, r.URL.Query().Get("probe_id"), since, until, granularity)
	if err != nil {
		if failure := ic.failLookup(err); failure != nil {
			return nil, failure
		}
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", summary))
}
func (ic *MonitorController) GetUptimeStats(ctx context.Context, input *GetUptimeStatsInput) (*httpx.Output[utils.ApiResponse[*CustomUptimeStatsDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	stats, err := ic.monitorService.GetUptimeStats(ctx, workspaceID, id, r.URL.Query().Get("probe_id"))
	if err != nil {
		if failure := ic.failLookup(err); failure != nil {
			return nil, failure
		}
		ic.logger.Errorw("Failed to get uptime stats (short)", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", stats))
}
func (ic *MonitorController) FindByIDs(ctx context.Context, input *FindByIDsInput) (*httpx.Output[utils.ApiResponse[[]*MonitorResponseDto]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	idsStr := r.URL.Query().Get("ids")
	if idsStr == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("ids parameter is required"))
	}

	// Split the comma-separated string into an array
	ids := strings.Split(idsStr, ",")
	if len(ids) == 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("at least one monitor ID is required"))
	}

	// Limit the number of IDs to prevent abuse
	if len(ids) > 100 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("maximum 100 monitor IDs allowed"))
	}

	monitors, err := ic.monitorService.FindByIDs(ctx, workspaceID, ids)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitors", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	response, err := ic.withLinks(ctx, monitors)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor links", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", response))
}
func (ic *MonitorController) ResetMonitorData(ctx context.Context, input *ResetMonitorDataInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	err = ic.monitorService.ResetMonitorData(ctx, workspaceID, id)
	if err != nil {
		if failure := ic.failLookup(err); failure != nil {
			return nil, failure
		}
		ic.logger.Errorw("Failed to reset monitor data", "monitorID", id, "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Monitor data reset successfully", nil))
}
func (ic *MonitorController) GetTLSInfo(ctx context.Context, input *GetTLSInfoInput) (*httpx.Output[utils.NullableApiResponse[*monitor_tls_info.TLSInfo]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	// The TLS info is read by the monitor's id, so the monitor is loaded in the workspace first
	findInWorkspace := func(ctx context.Context, id string) (*Model, error) {
		return ic.monitorService.FindByID(ctx, workspaceID, id)
	}
	if _, err := utils.RequireRecord(ctx, ic.logger, "Monitor", findInWorkspace, id); err != nil {
		return nil, err
	}

	// Get TLS info for the monitor
	tlsInfo, err := ic.tlsInfoService.GetTLSInfo(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to get TLS info", "monitorID", id, "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	// If no TLS info found, return null/empty
	if tlsInfo == nil {
		return httpx.Reply(http.StatusOK, utils.NewNullableSuccessResponse[*monitor_tls_info.TLSInfo]("success", nil))
	}

	return httpx.Reply(http.StatusOK, utils.NewNullableSuccessResponse("success", tlsInfo))
}

// invalidLocationRuleResponse tells the client why the location rule it sent doesn't fit the monitor
func invalidLocationRuleResponse(err *InvalidLocationRuleError) error {
	return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid location_rule: %v", err.Err)))
}
