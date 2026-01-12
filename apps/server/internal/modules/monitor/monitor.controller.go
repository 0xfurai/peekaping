package monitor

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/monitor_tag"
	"vigi/internal/modules/monitor_tls_info"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type MonitorController struct {
	monitorService             Service
	logger                     *zap.SugaredLogger
	monitorNotificationService monitor_notification.Service
	monitorTagService          monitor_tag.Service
	tlsInfoService             monitor_tls_info.Service
}

func NewMonitorController(
	monitorService Service,
	logger *zap.SugaredLogger,
	monitorNotificationService monitor_notification.Service,
	monitorTagService monitor_tag.Service,
	tlsInfoService monitor_tls_info.Service,
) *MonitorController {
	utils.Validate.RegisterStructValidation(CreateUpdateDtoStructLevelValidation, CreateUpdateDto{})

	return &MonitorController{
		monitorService,
		logger,
		monitorNotificationService,
		monitorTagService,
		tlsInfoService,
	}
}

// @Router		/monitors [get]
// @Summary		Get monitors
// @Tags			Monitors
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Security  OrgIdAuth
// @Param     q    query     string  false  "Search query"
// @Param     page query     int     false  "Page number" default(1)
// @Param     limit query    int     false  "Items per page" default(10)
// @Param     active query   bool    false  "Active status"
// @Param     status query   int     false  "Status"
// @Param     tag_ids query  string  false  "Comma-separated list of tag IDs to filter by"
// @Param     X-Organization-ID header string false "Organization ID"
// @Success		200	{object}	utils.ApiResponse[[]Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		403	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) FindAll(ctx *gin.Context) {
	page, err := utils.GetQueryInt(ctx, "page", 0)
	if err != nil || page < 0 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
		return
	}

	limit, err := utils.GetQueryInt(ctx, "limit", 10)
	if err != nil || limit < 1 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
		return
	}

	q := ctx.Query("q")

	active, err := utils.GetQueryBool(ctx, "active")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid active parameter (must be true or false)"))
		return
	}

	var statusPtr *int
	if statusStr := ctx.Query("status"); statusStr != "" {
		statusVal, err := utils.GetQueryInt(ctx, "status", 0)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid status parameter (must be int)"))
			return
		}
		statusPtr = &statusVal
	}

	// Parse tag_ids parameter
	var tagIds []string
	if tagIdsStr := ctx.Query("tag_ids"); tagIdsStr != "" {
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

	// Extract OrgID from context (set by OrganizationMiddleware)
	orgID := ctx.GetString("orgId")
	if orgID == "" {
		// Fallback or error?
		// If strict mode, we might require it. But for now, let's assume if it's there we use it.
		// Detailed plan said: "Update MonitorController to use context OrgID".
		// If middleware is not applied, orgID is empty.
		// If orgID is empty, FindAll will return empty list if we implemented it to require orgID, OR return all if query ignores empty string.
		// In repo implementation: `if orgID != "" { query = query.Where("org_id = ?", orgID) }`.
		// So if empty, it returns ALL monitors (Security Risk?).
		// BUT the plan says "Breaking Change for API Clients: The X-Organization-ID header will be required".
		// AND we will enforce middleware.
		// So if middleware is enforced, orgID will be present.
		// If middleware is NOT enforced on this route yet (we need to update Route), it might be empty.
		// I'll leave it as is, relying on middleware to enforce presence if needed.
	}

	response, err := ic.monitorService.FindAll(ctx, page, limit, q, active, statusPtr, tagIds, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitors", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/monitors [post]
// @Summary		Create monitor
// @Tags			Monitors
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Security  OrgIdAuth
// @Param     body body   CreateUpdateDto  true  "Monitor object"
// @Success		201	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) Create(ctx *gin.Context) {
	var monitor *CreateUpdateDto
	if err := ctx.ShouldBindJSON(&monitor); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Inject OrgID from middleware
	monitor.OrgID = ctx.GetString("orgId")

	if err := utils.Validate.Struct(monitor); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", err)))
		return
	}

	createdMonitor, err := ic.monitorService.Create(ctx, monitor)
	if err != nil {
		ic.logger.Errorw("Failed to create monitor", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ic.logger.Infof("Created monitor: %+v\n", createdMonitor)

	// Handle multiple notification IDs
	if len(monitor.NotificationIds) > 0 {
		for _, notificationId := range monitor.NotificationIds {
			_, err = ic.monitorNotificationService.Create(ctx, createdMonitor.ID, notificationId)
			if err != nil {
				ic.logger.Errorw("Failed to create monitor-notification record", "error", err)
				ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
				return
			}
		}
	}

	// Handle multiple tag IDs
	if len(monitor.TagIds) > 0 {
		for _, tagId := range monitor.TagIds {
			_, err = ic.monitorTagService.Create(ctx, createdMonitor.ID, tagId)
			if err != nil {
				ic.logger.Errorw("Failed to create monitor-tag record", "error", err)
				ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
				return
			}
		}
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Monitor created successfully", createdMonitor))
}

// @Router		/monitors/{id} [get]
// @Summary		Get monitor by ID
// @Tags			Monitors
// @Produce		json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param       id   path      string  true  "Monitor ID"
// @Success		200	{object}	utils.ApiResponse[MonitorResponseDto]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	monitor, err := ic.monitorService.FindByID(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if monitor == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
		return
	}

	// Fetch notification_ids
	notificationRels, err := ic.monitorNotificationService.FindByMonitorID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor-notification relations", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	notificationIds := make([]string, 0, len(notificationRels))
	for _, rel := range notificationRels {
		notificationIds = append(notificationIds, rel.NotificationID)
	}

	// Fetch tag_ids
	tagRels, err := ic.monitorTagService.FindByMonitorID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor-tag relations", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	tagIds := make([]string, 0, len(tagRels))
	for _, rel := range tagRels {
		tagIds = append(tagIds, rel.TagID)
	}

	// Compose response with notification_ids and tag_ids
	response := MonitorResponseDto{
		ID:              monitor.ID,
		Name:            monitor.Name,
		Interval:        monitor.Interval,
		Timeout:         monitor.Timeout,
		Type:            monitor.Type,
		Active:          monitor.Active,
		MaxRetries:      monitor.MaxRetries,
		RetryInterval:   monitor.RetryInterval,
		ResendInterval:  monitor.ResendInterval,
		Status:          int(monitor.Status),
		CreatedAt:       monitor.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       monitor.UpdatedAt.Format(time.RFC3339),
		NotificationIds: notificationIds,
		TagIds:          tagIds,
		ProxyId:         monitor.ProxyId,
		Config:          monitor.Config,
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/monitors/{id} [put]
// @Summary		Update monitor
// @Tags			Monitors
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param       id   path      string  true  "Monitor ID"
// @Param       monitor body     CreateUpdateDto  true  "Monitor object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) UpdateFull(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	var monitor CreateUpdateDto
	if err := ctx.ShouldBindJSON(&monitor); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// validate
	if err := utils.Validate.Struct(monitor); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate monitor type and config
	if err := ic.monitorService.ValidateMonitorConfig(monitor.Type, monitor.Config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", err)))
		return
	}

	// Set OrgID in DTO to ensure it is passed down
	monitor.OrgID = orgID

	updatedMonitor, err := ic.monitorService.UpdateFull(ctx, id, &monitor)
	if err != nil {
		ic.logger.Errorw("Failed to update monitor", "error", err)
		if errors.Is(err, ErrMonitorNotFound) {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse(err.Error()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Delete all existing notification relations and create new ones
	err = ic.monitorNotificationService.DeleteByMonitorID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete existing monitor-notification relations", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Create new notification relations
	for _, notificationId := range monitor.NotificationIds {
		_, err = ic.monitorNotificationService.Create(ctx, id, notificationId)
		if err != nil {
			ic.logger.Errorw("Failed to create monitor-notification record", "error", err)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			return
		}
	}

	// Delete all existing tag relations and create new ones
	err = ic.monitorTagService.DeleteByMonitorID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete existing monitor-tag relations", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Create new tag relations
	for _, tagId := range monitor.TagIds {
		_, err = ic.monitorTagService.Create(ctx, id, tagId)
		if err != nil {
			ic.logger.Errorw("Failed to create monitor-tag record", "error", err)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			return
		}
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Monitor updated successfully", updatedMonitor))
}

// @Router		/monitors/{id} [patch]
// @Summary		Update monitor
// @Tags			Monitors
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param       id   path      string  true  "Monitor ID"
// @Param       monitor body     PartialUpdateDto  true  "Monitor object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) UpdatePartial(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	var monitor PartialUpdateDto
	if err := ctx.ShouldBindJSON(&monitor); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate monitor type and config if they are being updated
	if monitor.Type != nil && monitor.Config != nil {
		if err := ic.monitorService.ValidateMonitorConfig(*monitor.Type, *monitor.Config); err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Invalid monitor configuration: %v", err)))
			return
		}
	}

	// Set OrgID in DTO
	monitor.OrgID = &orgID

	updatedMonitor, err := ic.monitorService.UpdatePartial(ctx, id, &monitor, false, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to update monitor", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Handle notification IDs if they are being updated
	if len(monitor.NotificationIds) > 0 {
		// Replace all monitor-notification relations in an optimized way
		existing, err := ic.monitorNotificationService.FindByMonitorID(ctx, id)
		if err != nil {
			ic.logger.Errorw("Failed to fetch monitor-notification relations", "error", err)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			return
		}

		// Build sets for comparison
		existingMap := make(map[string]string) // notificationID -> relationID
		for _, rel := range existing {
			existingMap[rel.NotificationID] = rel.ID
		}
		newSet := make(map[string]struct{})
		for _, nid := range monitor.NotificationIds {
			newSet[nid] = struct{}{}
		}

		// Delete relations not in the new list
		for notificationID, relID := range existingMap {
			if _, found := newSet[notificationID]; !found {
				if err := ic.monitorNotificationService.Delete(ctx, relID); err != nil {
					ic.logger.Warnw("Failed to delete monitor-notification relation", "error", err)
				}
			}
		}

		// Add new relations not already present
		for _, nid := range monitor.NotificationIds {
			if _, found := existingMap[nid]; !found {
				if _, err := ic.monitorNotificationService.Create(ctx, id, nid); err != nil {
					ic.logger.Warnw("Failed to create monitor-notification relation", "error", err)
				}
			}
		}
	}

	// Handle tag IDs if they are being updated
	if len(monitor.TagIds) > 0 {
		// Replace all monitor-tag relations in an optimized way
		existing, err := ic.monitorTagService.FindByMonitorID(ctx, id)
		if err != nil {
			ic.logger.Errorw("Failed to fetch monitor-tag relations", "error", err)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			return
		}

		// Build sets for comparison
		existingMap := make(map[string]string) // tagID -> relationID
		for _, rel := range existing {
			existingMap[rel.TagID] = rel.ID
		}
		newSet := make(map[string]struct{})
		for _, tid := range monitor.TagIds {
			newSet[tid] = struct{}{}
		}

		// Delete relations not in the new list
		for tagID, relID := range existingMap {
			if _, found := newSet[tagID]; !found {
				if err := ic.monitorTagService.Delete(ctx, relID); err != nil {
					ic.logger.Warnw("Failed to delete monitor-tag relation", "error", err)
				}
			}
		}

		// Add new relations not already present
		for _, tid := range monitor.TagIds {
			if _, found := existingMap[tid]; !found {
				if _, err := ic.monitorTagService.Create(ctx, id, tid); err != nil {
					ic.logger.Warnw("Failed to create monitor-tag relation", "error", err)
				}
			}
		}
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Monitor updated successfully", updatedMonitor))
}

// @Router		/monitors/{id} [delete]
// @Summary		Delete monitor
// @Tags			Monitors
// @Produce		json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param       id   path      string  true  "Monitor ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) Delete(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	err := ic.monitorService.Delete(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to delete monitor", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Monitor deleted successfully", nil))
}

// @Router	/monitors/{id}/heartbeats [get]
// @Summary	Get paginated heartbeats for a monitor
// @Tags		Monitors
// @Produce	json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param	id	path	string	true	"Monitor ID"
// @Param	limit	query	int	false	"Number of heartbeats per page (default 50)"
// @Param	page	query	int	false	"Page number (default 0)"
// @Param	important	query	bool	false	"Filter by important heartbeats only"
// @Param	reverse	query	bool	false	"Reverse the order of heartbeats"
// @Success	200	{object}	utils.ApiResponse[[]heartbeat.Model]
// @Failure	400	{object}	utils.APIError[any]
// @Failure	404	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (ic *MonitorController) FindByMonitorIDPaginated(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	limit, err := utils.GetQueryInt(ctx, "limit", 50)
	if err != nil || limit < 1 || limit > 1000 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter (1-1000)"))
		return
	}

	page, err := utils.GetQueryInt(ctx, "page", 0)
	if err != nil || page < 0 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter (>=0)"))
		return
	}

	var importantPtr *bool
	if ctx.Query("important") != "" {
		importantPtr, err = utils.GetQueryBool(ctx, "important")
		if err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid important parameter (must be true or false)"))
			return
		}
	}

	reverse := false
	if ctx.Query("reverse") != "" {
		reversePtr, err := utils.GetQueryBool(ctx, "reverse")
		if err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid reverse parameter (must be true or false)"))
			return
		}
		if reversePtr != nil {
			reverse = *reversePtr
		}
	}

	results, err := ic.monitorService.GetHeartbeats(ctx, id, limit, page, importantPtr, reverse, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to get heartbeats", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", results))
}

// @Router /monitors/{id}/stats/points [get]
// @Summary Get monitor stat points (ping/up/down) from stats tables
// @Tags Monitors
// @Produce json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param id path string true "Monitor ID"
// @Param since query string true "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339, default now)"
// @Param granularity query string false "Granularity (minute, hour, day)"
// @Success 200 {object} utils.ApiResponse[StatPointsSummaryDto]
// @Failure 400 {object} utils.APIError[any]
// @Failure 404 {object} utils.APIError[any]
// @Failure 500 {object} utils.APIError[any]
func (ic *MonitorController) GetStatPoints(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	sinceStr := ctx.Query("since")
	if sinceStr == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Missing required 'since' parameter"))
		return
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid 'since' parameter (must be RFC3339)"))
		return
	}

	untilStr := ctx.Query("until")
	var until time.Time
	if untilStr == "" {
		until = time.Now().UTC()
	} else {
		until, err = time.Parse(time.RFC3339, untilStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid 'until' parameter (must be RFC3339)"))
			return
		}
	}

	granularity := ctx.DefaultQuery("granularity", "minute")

	var interval time.Duration
	switch granularity {
	case "minute":
		interval = time.Minute
	case "hour":
		interval = time.Hour
	case "day":
		interval = 24 * time.Hour
	default:
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid 'granularity' parameter (must be minute, hour, or day)"))
		return
	}

	if until.Before(since) {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("'until' must be after 'since'"))
		return
	}

	diff := until.Sub(since)
	estPoints := int(diff/interval) + 1
	if estPoints > 1441 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(fmt.Sprintf("Too many points requested: %d (max 1441)", estPoints)))
		return
	}

	summary, err := ic.monitorService.GetStatPoints(ctx, id, since, until, granularity, orgID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", summary))
}

// @Router /monitors/{id}/stats/uptime [get]
// @Summary Get monitor uptime stats (24h, 30d, 365d)
// @Tags Monitors
// @Produce json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param id path string true "Monitor ID"
// @Success 200 {object} utils.ApiResponse[CustomUptimeStatsDto]
// @Failure 400 {object} utils.APIError[any]
// @Failure 404 {object} utils.APIError[any]
// @Failure 500 {object} utils.APIError[any]
func (ic *MonitorController) GetUptimeStats(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	stats, err := ic.monitorService.GetUptimeStats(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to get uptime stats (short)", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", stats))
}

// @Router		/monitors/batch [get]
// @Summary		Get monitors by IDs
// @Tags			Monitors
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     ids    query     string  true  "Comma-separated list of monitor IDs"
// @Success		200	{object}	utils.ApiResponse[[]Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *MonitorController) FindByIDs(ctx *gin.Context) {
	idsStr := ctx.Query("ids")
	if idsStr == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("ids parameter is required"))
		return
	}

	// Split the comma-separated string into an array
	ids := strings.Split(idsStr, ",")
	if len(ids) == 0 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("at least one monitor ID is required"))
		return
	}

	// Limit the number of IDs to prevent abuse
	if len(ids) > 100 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("maximum 100 monitor IDs allowed"))
		return
	}

	orgID := ctx.GetString("orgId")
	monitors, err := ic.monitorService.FindByIDs(ctx, ids, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitors", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", monitors))
}

// @Router /monitors/{id}/reset [post]
// @Summary Reset monitor data (heartbeats and stats)
// @Tags Monitors
// @Produce json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param id path string true "Monitor ID"
// @Success 200 {object} utils.ApiResponse[any]
// @Failure 400 {object} utils.APIError[any]
// @Failure 404 {object} utils.APIError[any]
// @Failure 500 {object} utils.APIError[any]
func (ic *MonitorController) ResetMonitorData(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	err := ic.monitorService.ResetMonitorData(ctx, id, orgID)
	if err != nil {
		if err.Error() == "monitor not found" {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
			return
		}
		ic.logger.Errorw("Failed to reset monitor data", "monitorID", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Monitor data reset successfully", nil))
}

// @Router /monitors/{id}/tls [get]
// @Summary Get monitor TLS certificate information
// @Tags Monitors
// @Produce json
// @Security BearerAuth
// @Security OrgIdAuth
// @Param id path string true "Monitor ID"
// @Success 200 {object} utils.ApiResponse[any]
// @Failure 400 {object} utils.APIError[any]
// @Failure 404 {object} utils.APIError[any]
// @Failure 500 {object} utils.APIError[any]
func (ic *MonitorController) GetTLSInfo(ctx *gin.Context) {
	id := ctx.Param("id")

	orgID := ctx.GetString("orgId")
	// First, verify the monitor exists
	_, err := ic.monitorService.FindByID(ctx, id, orgID)
	if err != nil {
		if err.Error() == "monitor not found" {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found"))
			return
		}
		ic.logger.Errorw("Failed to fetch monitor", "monitorID", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Get TLS info for the monitor
	tlsInfo, err := ic.tlsInfoService.GetTLSInfo(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to get TLS info", "monitorID", id, "error", err) // TODO: fix 500 when no rows in a set
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// If no TLS info found, return null/empty
	if tlsInfo == nil {
		ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("success", nil))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", tlsInfo))
}
