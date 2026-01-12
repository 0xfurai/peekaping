package status_page

import (
	"net/http"
	"time"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Controller struct {
	service          Service
	monitorService   monitor.Service
	heartbeatService heartbeat.Service
	logger           *zap.SugaredLogger
}

func NewController(service Service, monitorService monitor.Service, heartbeatService heartbeat.Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service:          service,
		monitorService:   monitorService,
		heartbeatService: heartbeatService,
		logger:           logger,
	}
}

// @Router    /status-pages [post]
// @Summary   Create a new status page
// @Tags      Status Pages
// @Accept    json
// @Produce   json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body CreateStatusPageDTO true "Status Page object"
// @Success   201  {object} utils.ApiResponse[Model]
// @Failure   400  {object} utils.APIError[any]
// @Failure   500  {object} utils.APIError[any]
func (c *Controller) Create(ctx *gin.Context) {
	var dto CreateStatusPageDTO
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	created, err := c.service.Create(ctx, &dto, orgID)
	if err != nil {
		// Surface domain uniqueness validation errors as 400
		if domainErr, ok := err.(*DomainAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":   domainErr.Code,
					"domain": domainErr.Domain,
				},
			})
			return
		}
		// Surface slug uniqueness validation errors as 400
		if slugErr, ok := err.(*SlugAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code": slugErr.Code,
					"slug": slugErr.Slug,
				},
			})
			return
		}
		c.logger.Errorw("Failed to create status page", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Status page created successfully", created))
}

// @Router    /status-pages/{id} [get]
// @Summary   Get a status page by ID
// @Tags      Status Pages
// @Produce   json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path      string  true  "Status Page ID"
// @Success   200  {object}  utils.ApiResponse[StatusPageWithMonitorsResponseDTO]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")
	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	page, err := c.service.FindByIDWithMonitors(ctx, id, orgID)
	if err != nil {
		c.logger.Errorw("Failed to get status page by id", "error", err, "id", id)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if page == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", page))
}

// @Router    /status-pages/slug/{slug} [get]
// @Summary   Get a status page by slug
// @Tags      Status Pages
// @Produce   json
// @Param     slug path      string  true  "Status Page Slug"
// @Success   200  {object}  utils.ApiResponse[Model]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindBySlug(ctx *gin.Context) {
	slug := ctx.Param("slug")
	page, err := c.service.FindBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if page == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", page))
}

// @Router    /status-pages/domain/{domain} [get]
// @Summary   Get a status page by domain name
// @Tags      Status Pages
// @Produce   json
// @Param     domain path      string  true  "Domain Name"
// @Success   200  {object}  utils.ApiResponse[Model]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindByDomain(ctx *gin.Context) {
	domain := ctx.Param("domain")
	page, err := c.service.FindByDomain(ctx, domain)
	if err != nil {
		c.logger.Errorw("Failed to get status page by domain", "error", err, "domain", domain)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if page == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", page))
}

// @Router    /status-pages [get]
// @Summary   Get all status pages
// @Tags      Status Pages
// @Produce   json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     q    query     string  false  "Search query"
// @Param     page query     int     false  "Page number" default(0)
// @Param     limit query    int     false  "Items per page" default(10)
// @Success   200  {object}  utils.ApiResponse[[]Model]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindAll(ctx *gin.Context) {
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

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	pages, err := c.service.FindAll(ctx, page, limit, q, orgID)
	if err != nil {
		c.logger.Errorw("Failed to get all status pages", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", pages))
}

// @Router    /status-pages/{id} [patch]
// @Summary   Update a status page
// @Tags      Status Pages
// @Accept    json
// @Produce   json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path      string  true  "Status Page ID"
// @Param     body body UpdateStatusPageDTO true "Status Page object"
// @Success   200  {object}  utils.ApiResponse[Model]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	var dto UpdateStatusPageDTO
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	updated, err := c.service.Update(ctx, id, &dto, orgID)
	if err != nil {
		// Surface domain uniqueness validation errors as 400
		if domainErr, ok := err.(*DomainAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":   domainErr.Code,
					"domain": domainErr.Domain,
				},
			})
			return
		}
		// Surface slug uniqueness validation errors as 400
		if slugErr, ok := err.(*SlugAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code": slugErr.Code,
					"slug": slugErr.Slug,
				},
			})
			return
		}
		c.logger.Errorw("Failed to update status page", "error", err, "id", id)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Status page updated successfully", updated))
}

// @Router    /status-pages/{id} [delete]
// @Summary   Delete a status page
// @Tags      Status Pages
// @Produce   json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path      string  true  "Status Page ID"
// @Success   200  {object}  utils.ApiResponse[any]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) Delete(ctx *gin.Context) {
	id := ctx.Param("id")
	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	err := c.service.Delete(ctx, id, orgID)
	if err != nil {
		c.logger.Errorw("Failed to delete status page", "error", err, "id", id)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Status page deleted successfully", nil))
}

// @Router    /status-pages/slug/{slug}/monitors [get]
// @Summary   Get monitors for a status page by slug with heartbeats and uptime
// @Tags      Status Pages
// @Produce   json
// @Param     slug path      string  true  "Status Page Slug"
// @Success   200  {object}  utils.ApiResponse[[]MonitorWithHeartbeatsAndUptimeDTO]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) GetMonitorsBySlug(ctx *gin.Context) {
	slug := ctx.Param("slug")

	// First get the status page
	page, err := c.service.FindBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if page == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
		return
	}

	// Get monitors for the status page
	monitors, err := c.service.GetMonitorsForStatusPage(ctx, page.ID)
	if err != nil {
		c.logger.Errorw("Failed to get monitors for status page", "error", err, "statusPageID", page.ID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Convert monitor_status_page models to monitor models with heartbeats and uptime
	monitorModels := make([]*MonitorWithHeartbeatsAndUptimeDTO, 0, len(monitors))
	for _, msp := range monitors {
		// Get the actual monitor data
		monitorModel, err := c.monitorService.FindByID(ctx, msp.MonitorID, "")
		if err != nil {
			c.logger.Errorw("Failed to get monitor by ID", "error", err, "monitorID", msp.MonitorID)
			continue
		}
		if monitorModel == nil {
			continue
		}

		// Get 100 heartbeats for this monitor
		heartbeats, err := c.heartbeatService.FindByMonitorIDPaginated(ctx, msp.MonitorID, 100, 0, nil, true)
		if err != nil {
			c.logger.Errorw("Failed to get heartbeats for monitor", "error", err, "monitorID", msp.MonitorID)
			heartbeats = []*heartbeat.Model{} // Empty slice if error
		}

		// Convert heartbeats to public DTOs
		publicHeartbeats := make([]*PublicHeartbeatDTO, 0, len(heartbeats))
		for _, hb := range heartbeats {
			publicHeartbeat := &PublicHeartbeatDTO{
				ID:      hb.ID,
				Status:  hb.Status,
				Time:    hb.Time,
				EndTime: hb.EndTime,
				Ping:    hb.Ping,
			}
			publicHeartbeats = append(publicHeartbeats, publicHeartbeat)
		}

		// Get 24h uptime for this monitor
		now := time.Now().UTC()
		periods := map[string]time.Duration{
			"24h": 24 * time.Hour,
		}
		uptimeStats, err := c.heartbeatService.FindUptimeStatsByMonitorID(ctx, msp.MonitorID, periods, now)
		if err != nil {
			c.logger.Errorw("Failed to get uptime stats for monitor", "error", err, "monitorID", msp.MonitorID)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("failed to get uptime stats for monitor"))
			return
		}

		uptime24h := 0.0
		if uptimeStats != nil {
			if uptime, exists := uptimeStats["24h"]; exists {
				uptime24h = uptime
			}
		}

		publicMonitor := &PublicMonitorDTO{
			ID:     monitorModel.ID,
			Type:   monitorModel.Type,
			Name:   monitorModel.Name,
			Active: monitorModel.Active,
		}

		monitorWithData := &MonitorWithHeartbeatsAndUptimeDTO{
			PublicMonitorDTO: publicMonitor,
			Heartbeats:       publicHeartbeats,
			Uptime24h:        uptime24h,
		}

		monitorModels = append(monitorModels, monitorWithData)
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", monitorModels))
}

// @Router    /status-pages/slug/{slug}/monitors/homepage [get]
// @Summary   Get monitors for a status page by slug for homepage
// @Tags      Status Pages
// @Produce   json
// @Param     slug path      string  true  "Status Page Slug"
// @Success   200  {object}  utils.ApiResponse[[]MonitorWithHeartbeatsAndUptimeDTO]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) GetMonitorsBySlugForHomepage(ctx *gin.Context) {
	slug := ctx.Param("slug")

	// First get the status page
	page, err := c.service.FindBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if page == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
		return
	}

	// Get monitors for the status page
	monitors, err := c.service.GetMonitorsForStatusPage(ctx, page.ID)
	if err != nil {
		c.logger.Errorw("Failed to get monitors for status page", "error", err, "statusPageID", page.ID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Convert monitor_status_page models to monitor models with heartbeats and uptime
	monitorModels := make([]*MonitorWithHeartbeatsAndUptimeDTO, 0, len(monitors))
	for _, msp := range monitors {
		// Get the actual monitor data
		monitorModel, err := c.monitorService.FindByID(ctx, msp.MonitorID, "")
		if err != nil {
			c.logger.Errorw("Failed to get monitor by ID", "error", err, "monitorID", msp.MonitorID)
			continue
		}
		if monitorModel == nil {
			continue
		}

		// Get 100 heartbeats for this monitor
		heartbeats, err := c.heartbeatService.FindByMonitorIDPaginated(ctx, msp.MonitorID, 1, 0, nil, true)
		if err != nil {
			c.logger.Errorw("Failed to get heartbeats for monitor", "error", err, "monitorID", msp.MonitorID)
			heartbeats = []*heartbeat.Model{} // Empty slice if error
		}

		// Convert heartbeats to public DTOs
		publicHeartbeats := make([]*PublicHeartbeatDTO, 0, len(heartbeats))
		for _, hb := range heartbeats {
			publicHeartbeat := &PublicHeartbeatDTO{
				ID:      hb.ID,
				Status:  hb.Status,
				Time:    hb.Time,
				EndTime: hb.EndTime,
				Ping:    hb.Ping,
			}
			publicHeartbeats = append(publicHeartbeats, publicHeartbeat)
		}

		// Get 24h uptime for this monitor
		now := time.Now().UTC()
		periods := map[string]time.Duration{
			"24h": 24 * time.Hour,
		}
		uptimeStats, err := c.heartbeatService.FindUptimeStatsByMonitorID(ctx, msp.MonitorID, periods, now)
		if err != nil {
			c.logger.Errorw("Failed to get uptime stats for monitor", "error", err, "monitorID", msp.MonitorID)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("failed to get uptime stats for monitor"))
			return
		}

		uptime24h := 0.0
		if uptimeStats != nil {
			if uptime, exists := uptimeStats["24h"]; exists {
				uptime24h = uptime
			}
		}

		publicMonitor := &PublicMonitorDTO{
			ID:     monitorModel.ID,
			Type:   monitorModel.Type,
			Name:   monitorModel.Name,
			Active: monitorModel.Active,
		}

		monitorWithData := &MonitorWithHeartbeatsAndUptimeDTO{
			PublicMonitorDTO: publicMonitor,
			Heartbeats:       publicHeartbeats,
			Uptime24h:        uptime24h,
		}

		monitorModels = append(monitorModels, monitorWithData)
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", monitorModels))
}
