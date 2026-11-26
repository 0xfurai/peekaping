package monitor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/netguard"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
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
	registerDtoRules()

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
		responses[i] = &MonitorResponseDto{Monitor: *m, TypeDisabled: m.TypeDisabled, OverLimit: m.OverLimit, NotificationIds: notificationIds, TagIds: tagIds}
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
	query, err := parseFindAllQuery(input.Request)
	if err != nil {
		return nil, err
	}

	response, err := ic.monitorService.FindAll(ctx, workspaceID, query.page, query.limit, query.q, query.active, query.status, query.tagIds)
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

// findAllQuery is the query of a monitor listing, as the service takes it.
type findAllQuery struct {
	page, limit int
	q           string
	active      *bool
	status      *int
	tagIds      []string
}

// parseFindAllQuery reads the query of a listing, and answers 400 for the first parameter it can't
// read: page, limit, active, then status.
func parseFindAllQuery(r *http.Request) (findAllQuery, error) {
	var query findAllQuery
	var err error
	if query.page, err = queryIntIn(r, "page", 0, 0, math.MaxInt, "Invalid page parameter"); err != nil {
		return query, err
	}
	if query.limit, err = queryIntIn(r, "limit", 10, 1, utils.MaxPageSize, "Invalid limit parameter"); err != nil {
		return query, err
	}
	query.q = r.URL.Query().Get("q")
	if query.active, err = queryBool(r, "active", "Invalid active parameter (must be true or false)"); err != nil {
		return query, err
	}
	if query.status, err = queryOptionalInt(r, "status", "Invalid status parameter (must be int)"); err != nil {
		return query, err
	}
	query.tagIds = splitTagIDs(r.URL.Query().Get("tag_ids"))
	return query, nil
}

// badRequest is the 400 with the usual message
func badRequest(message string) error {
	return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(message))
}

// queryIntIn reads an integer query parameter that must be between min and max, and answers 400
// with the message when it isn't a number or is out of range
func queryIntIn(r *http.Request, key string, fallback, min, max int, message string) (int, error) {
	value, err := utils.GetQueryInt(r, key, fallback)
	if err != nil || value < min || value > max {
		return 0, badRequest(message)
	}
	return value, nil
}

// queryBool reads an optional boolean query parameter, nil when it is absent, and answers 400 with
// the message when it isn't true or false
func queryBool(r *http.Request, key, message string) (*bool, error) {
	value, err := utils.GetQueryBool(r, key)
	if err != nil {
		return nil, badRequest(message)
	}
	return value, nil
}

// queryOptionalInt reads an optional integer query parameter, nil when it is absent, and answers
// 400 with the message when it isn't a number
func queryOptionalInt(r *http.Request, key, message string) (*int, error) {
	if r.URL.Query().Get(key) == "" {
		return nil, nil
	}
	value, err := utils.GetQueryInt(r, key, 0)
	if err != nil {
		return nil, badRequest(message)
	}
	return &value, nil
}

// splitTagIDs splits a comma-separated list of tag IDs, trims each and drops the empty ones. It is
// nil when none is left.
func splitTagIDs(list string) []string {
	if list == "" {
		return nil
	}
	var tagIds []string
	for _, tagId := range strings.Split(list, ",") {
		if tagId = strings.TrimSpace(tagId); tagId != "" {
			tagIds = append(tagIds, tagId)
		}
	}
	return tagIds
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
		return nil, ic.failValidation(ctx, workspaceID, err, &monitor.Interval, &monitor.RetryInterval)
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		return nil, configRefusalResponse(err)
	}

	createdMonitor, err := ic.monitorService.Create(ctx, workspaceID, monitor)
	if err != nil {
		return nil, ic.failCreate(err)
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
		OverLimit:       createdMonitor.OverLimit,
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
		return nil, ic.failValidation(ctx, workspaceID, err, &monitor.Interval, &monitor.RetryInterval)
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		return nil, configRefusalResponse(err)
	}

	updatedMonitor, err := ic.monitorService.UpdateFull(ctx, workspaceID, id, &monitor)
	if err != nil {
		return nil, ic.failUpdate(err)
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
		return nil, ic.failValidation(ctx, workspaceID, err, monitor.Interval, monitor.RetryInterval)
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

// failCreate maps the error of a create to its response.
func (ic *MonitorController) failCreate(err error) error {
	if failure := writeRefusalResponse(err); failure != nil {
		return failure
	}
	ic.logger.Errorw("Failed to create monitor", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// failUpdate maps the error of a full update to its response. The monitor isn't found as 404 with
// the error's own message.
func (ic *MonitorController) failUpdate(err error) error {
	if failure := writeRefusalResponse(err); failure != nil {
		return failure
	}
	ic.logger.Errorw("Failed to update monitor", "error", err)
	if errors.Is(err, ErrMonitorNotFound) {
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse(err.Error()))
	}
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// configRefusalResponse is the 400 for a config the service refuses: the address the install's
// guard blocks, or any other invalid configuration.
func configRefusalResponse(err error) error {
	var blocked *netguard.TargetError
	if errors.As(err, &blocked) {
		return utils.TargetNotAllowed(blocked)
	}
	return badRequest(fmt.Sprintf("Invalid monitor configuration: %v", err))
}

// writeRefusalResponse is the response for the error of a create or full update that the client can
// act on: a refusal of the install's limits, of the probes, location rule or push token, or a
// link to a record that isn't in the workspace. It is nil for any other error.
func writeRefusalResponse(err error) error {
	if failure := limitRefusalResponse(err); failure != nil {
		return failure
	}
	if failure := probeRefusalResponse(err); failure != nil {
		return failure
	}
	return linkNotFoundResponse(err)
}

// limitRefusalResponse is the response for a monitor type that is turned off, a limit of the
// workspace that is reached, and an interval below the minimum, and nil for any other error
func limitRefusalResponse(err error) error {
	var typeDisabled *MonitorTypeDisabledError
	var reached *limits.ReachedError
	var tooShort *IntervalTooShortError
	switch {
	case errors.As(err, &typeDisabled):
		return monitorTypeDisabledResponse(typeDisabled)
	case errors.As(err, &reached):
		return limits.Fail(reached)
	case errors.As(err, &tooShort):
		return intervalTooShortResponse(tooShort)
	}
	return nil
}

// probeRefusalResponse is the response for probes that don't fit the monitor, a location rule that
// is invalid and a push token that another monitor holds, and nil for any other error
func probeRefusalResponse(err error) error {
	var invalidProbes *InvalidProbesError
	var invalidRule *InvalidLocationRuleError
	var tokenTaken *PushTokenAlreadyUsedError
	switch {
	case errors.As(err, &invalidProbes):
		return invalidProbesResponse(invalidProbes)
	case errors.As(err, &invalidRule):
		return invalidLocationRuleResponse(invalidRule)
	case errors.As(err, &tokenTaken):
		return pushTokenTakenResponse(tokenTaken)
	}
	return nil
}

// failWrite maps the error of a write that looks up the monitor itself to its response.
func (ic *MonitorController) failWrite(action string, err error) error {
	var blocked *netguard.TargetError
	var invalidConfig *InvalidConfigError
	var invalidTimeout *InvalidTimeoutError
	switch {
	case errors.Is(err, ErrMonitorNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
	case errors.As(err, &blocked):
		return utils.TargetNotAllowed(blocked)
	case errors.As(err, &invalidConfig):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", invalidConfig.Err)))
	case errors.As(err, &invalidTimeout):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid timeout: %v", invalidTimeout)))
	}
	if failure := probeRefusalResponse(err); failure != nil {
		return failure
	}
	if failure := limitRefusalResponse(err); failure != nil {
		return failure
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

// failValidation answers a write whose DTO fails validation. An interval or retry interval below the
// DTOs' min=20 is below the install's minimum too, so it gets the same INTERVAL_TOO_SHORT answer as
// the service's own check, with the minimum the service enforces for the workspace (the install's,
// or the workspace's own limit when that is stricter). It wins over the other failures of the
// request, as the client can't tell the refusal by its code otherwise. Anything else is the usual
// 400 with the validator's message.
func (ic *MonitorController) failValidation(ctx context.Context, workspaceID workspace.ID, err error, interval, retryInterval *int) error {
	var invalid validator.ValidationErrors
	if errors.As(err, &invalid) {
		for _, f := range []struct {
			name, structField string
			value             *int
		}{
			{"interval", "Interval", interval},
			{"retry_interval", "RetryInterval", retryInterval},
		} {
			tooShort := slices.ContainsFunc(invalid, func(fe validator.FieldError) bool {
				return fe.StructField() == f.structField && fe.Tag() == "min"
			})
			if tooShort && f.value != nil {
				minimum, minErr := ic.monitorService.MinCheckInterval(ctx, workspaceID)
				if minErr != nil {
					ic.logger.Errorw("Failed to read the minimum check interval", "error", minErr)
					return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
				}
				return intervalTooShortResponse(intervalTooShortError(f.name, *f.value, minimum))
			}
		}
	}
	return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
}

// intervalTooShortResponse tells the client that the interval or retry interval it sent is below the
// install's minimum, and what the minimum is
func intervalTooShortResponse(err *IntervalTooShortError) error {
	return httpx.Fail(http.StatusBadRequest, IntervalTooShortResponse{Message: err.Error(), Error: err})
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

	limit, err := queryIntIn(r, "limit", 50, 1, utils.MaxPageSize, "Invalid limit parameter (1-1000)")
	if err != nil {
		return nil, err
	}
	page, err := queryIntIn(r, "page", 0, 0, math.MaxInt, "Invalid page parameter (>=0)")
	if err != nil {
		return nil, err
	}
	importantPtr, err := queryBool(r, "important", "Invalid important parameter (must be true or false)")
	if err != nil {
		return nil, err
	}
	reversePtr, err := queryBool(r, "reverse", "Invalid reverse parameter (must be true or false)")
	if err != nil {
		return nil, err
	}
	reverse := reversePtr != nil && *reversePtr

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

	since, until, granularity, err := parseStatRange(r)
	if err != nil {
		return nil, err
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

// statGranularities is the length of a point of each granularity the stat points are served in
var statGranularities = map[string]time.Duration{
	"minute": time.Minute,
	"hour":   time.Hour,
	"day":    24 * time.Hour,
}

// maxStatPoints is the most points one request for stat points may ask for
const maxStatPoints = 1441

// parseStatRange reads the range and granularity of a request for stat points, and answers 400 for
// the first thing that is wrong with it: since, until, granularity, the order of the two times and
// the number of points.
func parseStatRange(r *http.Request) (since, until time.Time, granularity string, err error) {
	if since, err = statSince(r); err != nil {
		return
	}
	if until, err = statUntil(r); err != nil {
		return
	}
	granularity = httpx.DefaultQuery(r, "granularity", "minute")
	interval, ok := statGranularities[granularity]
	if !ok {
		err = badRequest("Invalid 'granularity' parameter (must be minute, hour, or day)")
		return
	}
	if until.Before(since) {
		err = badRequest("'until' must be after 'since'")
		return
	}
	if estPoints := int(until.Sub(since)/interval) + 1; estPoints > maxStatPoints {
		err = badRequest(fmt.Sprintf("Too many points requested: %d (max 1441)", estPoints))
	}
	return
}

// statSince reads the required 'since' parameter
func statSince(r *http.Request) (time.Time, error) {
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		return time.Time{}, badRequest("Missing required 'since' parameter")
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		return time.Time{}, badRequest("Invalid 'since' parameter (must be RFC3339)")
	}
	return since, nil
}

// statUntil reads the 'until' parameter, which is now when it is omitted
func statUntil(r *http.Request) (time.Time, error) {
	untilStr := r.URL.Query().Get("until")
	if untilStr == "" {
		return time.Now().UTC(), nil
	}
	until, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		return time.Time{}, badRequest("Invalid 'until' parameter (must be RFC3339)")
	}
	return until, nil
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
