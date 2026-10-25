package status_page

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"slices"
	"strings"
	"time"

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
func (c *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(&dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	created, err := c.service.Create(ctx, &dto)
	if err != nil {
		// Surface domain uniqueness validation errors as 400
		if domainErr, ok := err.(*DomainAlreadyUsedError); ok {
			return nil, httpx.Fail(http.StatusBadRequest, DomainErrorResponse{Error: domainErr})
		}
		c.logger.Errorw("Failed to create status page", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Status page created successfully", created))
}
func (c *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*StatusPageWithMonitorsResponseDTO]], error) {
	r := input.Request
	id := r.PathValue("id")
	page, err := c.service.FindByIDWithMonitors(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to get status page by id", "error", err, "id", id)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", page))
}
func (c *Controller) FindBySlug(ctx context.Context, input *FindBySlugInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	slug := r.PathValue("slug")
	page, err := c.service.FindPublishedBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", page))
}
func (c *Controller) FindByDomain(ctx context.Context, input *FindByDomainInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	domain := r.PathValue("domain")
	page, err := c.service.FindPublishedByDomain(ctx, domain)
	if err != nil {
		c.logger.Errorw("Failed to get status page by domain", "error", err, "domain", domain)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", page))
}
func (c *Controller) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*Model]], error) {
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

	pages, err := c.service.FindAll(ctx, page, limit, q)
	if err != nil {
		c.logger.Errorw("Failed to get all status pages", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", pages))
}
func (c *Controller) Update(ctx context.Context, input *UpdateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, c.logger, "Status page", c.service.FindByID, id); err != nil {
		return nil, err
	}

	updated, err := c.service.Update(ctx, id, &dto)
	if err != nil {
		// Surface domain uniqueness validation errors as 400
		if domainErr, ok := err.(*DomainAlreadyUsedError); ok {
			return nil, httpx.Fail(http.StatusBadRequest, DomainErrorResponse{Error: domainErr})
		}
		c.logger.Errorw("Failed to update status page", "error", err, "id", id)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Status page updated successfully", updated))
}
func (c *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	r := input.Request
	id := r.PathValue("id")
	if _, err := utils.RequireRecord(ctx, c.logger, "Status page", c.service.FindByID, id); err != nil {
		return nil, err
	}

	err := c.service.Delete(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to delete status page", "error", err, "id", id)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Status page deleted successfully", nil))
}
func (c *Controller) GetMonitorsBySlug(ctx context.Context, input *GetMonitorsBySlugInput) (*httpx.Output[utils.ApiResponse[[]*MonitorWithHeartbeatsAndUptimeDTO]], error) {
	return c.publicMonitors(ctx, input.Request.PathValue("slug"), 100)
}
func (c *Controller) GetMonitorsBySlugForHomepage(ctx context.Context, input *GetMonitorsBySlugForHomepageInput) (*httpx.Output[utils.ApiResponse[[]*MonitorWithHeartbeatsAndUptimeDTO]], error) {
	return c.publicMonitors(ctx, input.Request.PathValue("slug"), 1)
}

// publicMonitors answers the monitors of the published status page at slug, each with its latest
// heartbeats, as many as heartbeatLimit, and its 24 hour uptime.
func (c *Controller) publicMonitors(ctx context.Context, slug string, heartbeatLimit int) (*httpx.Output[utils.ApiResponse[[]*MonitorWithHeartbeatsAndUptimeDTO]], error) {
	// First get the status page
	page, err := c.service.FindPublishedBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}

	// Get monitors for the status page
	monitors, err := c.service.GetMonitorsForStatusPage(ctx, page.ID)
	if err != nil {
		c.logger.Errorw("Failed to get monitors for status page", "error", err, "statusPageID", page.ID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	// Convert monitor_status_page models to monitor models with heartbeats and uptime
	monitorModels := make([]*MonitorWithHeartbeatsAndUptimeDTO, 0, len(monitors))
	for _, msp := range monitors {
		// Get the actual monitor data
		monitorModel, err := c.monitorService.FindByID(ctx, msp.MonitorID)
		if err != nil {
			c.logger.Errorw("Failed to get monitor by ID", "error", err, "monitorID", msp.MonitorID)
			continue
		}
		if monitorModel == nil {
			continue
		}

		// Get the latest heartbeats for this monitor
		heartbeats, err := c.heartbeatService.FindByMonitorIDPaginated(ctx, msp.MonitorID, heartbeatLimit, 0, nil, true)
		if err != nil {
			c.logger.Errorw("Failed to get heartbeats for monitor", "error", err, "monitorID", msp.MonitorID)
			heartbeats = []*heartbeat.Model{} // Empty slice if error
		}

		// Get 24h uptime for this monitor
		now := time.Now().UTC()
		periods := map[string]time.Duration{
			"24h": 24 * time.Hour,
		}
		uptimeStats, err := c.heartbeatService.FindUptimeStatsByMonitorID(ctx, msp.MonitorID, periods, now)
		if err != nil {
			c.logger.Errorw("Failed to get uptime stats for monitor", "error", err, "monitorID", msp.MonitorID)
			return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("failed to get uptime stats for monitor"))
		}

		uptime24h := 0.0
		if uptimeStats != nil {
			if uptime, exists := uptimeStats["24h"]; exists {
				uptime24h = uptime
			}
		}

		monitorWithData := &MonitorWithHeartbeatsAndUptimeDTO{
			PublicMonitorDTO: &PublicMonitorDTO{
				ID:     monitorModel.ID,
				Type:   monitorModel.Type,
				Name:   monitorModel.Name,
				Active: monitorModel.Active,
				Status: monitorModel.Status,
			},
			Heartbeats: publicHeartbeats(heartbeats),
			Uptime24h:  uptime24h,
		}
		if page.ShowLocations && len(monitorModel.Locations) > 1 {
			monitorWithData.Locations, err = c.publicLocations(ctx, monitorModel, heartbeatLimit)
			if err != nil {
				c.logger.Errorw("Failed to get locations for monitor", "error", err, "monitorID", msp.MonitorID)
				return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("failed to get locations for monitor"))
			}
		}

		monitorModels = append(monitorModels, monitorWithData)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", monitorModels))
}

// publicLocations lists a monitor's locations for a visitor: the local probe first, then the others
// by name. A location whose probe has not reported yet is Pending, as a new monitor is.
func (c *Controller) publicLocations(ctx context.Context, m *shared.Monitor, heartbeatLimit int) ([]*PublicLocationDTO, error) {
	locations := slices.Clone(m.Locations)
	slices.SortFunc(locations, func(a, b shared.MonitorLocation) int {
		if aLocal, bLocal := a.ProbeID == shared.LocalProbeID, b.ProbeID == shared.LocalProbeID; aLocal != bLocal {
			if aLocal {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ProbeName, b.ProbeName)
	})

	public := make([]*PublicLocationDTO, 0, len(locations))
	for _, location := range locations {
		// A visitor's page doesn't fail for want of a location's heartbeats, as it doesn't for the
		// monitor's own: the location is listed without them
		heartbeats, err := c.monitorService.GetHeartbeats(ctx, m.ID, location.ProbeID, heartbeatLimit, 0, nil, true)
		if err != nil {
			c.logger.Errorw("Failed to get the heartbeats of a location", "error", err, "monitorID", m.ID, "probeID", location.ProbeID)
			heartbeats = nil
		}
		// Only the last 24 hours are shown. Nor does the page fail for want of this number: the
		// location is listed with a null uptime.
		var uptime24h *float64
		if uptime, err := c.monitorService.GetUptime24h(ctx, m.ID, location.ProbeID); err != nil {
			c.logger.Errorw("Failed to get the 24h uptime of a location", "error", err, "monitorID", m.ID, "probeID", location.ProbeID)
		} else {
			uptime24h = &uptime
		}
		status := shared.MonitorStatusPending
		if location.Status != nil {
			status = *location.Status
		}
		public = append(public, &PublicLocationDTO{
			Name:       location.ProbeName,
			Status:     status,
			Uptime24h:  uptime24h,
			Heartbeats: publicHeartbeats(heartbeats),
		})
	}
	return public, nil
}

// publicHeartbeats keeps of each heartbeat what a visitor of the status page may see
func publicHeartbeats(heartbeats []*heartbeat.Model) []*PublicHeartbeatDTO {
	public := make([]*PublicHeartbeatDTO, 0, len(heartbeats))
	for _, hb := range heartbeats {
		public = append(public, &PublicHeartbeatDTO{
			ID:      hb.ID,
			Status:  hb.Status,
			Time:    hb.Time,
			EndTime: hb.EndTime,
			Ping:    hb.Ping,
		})
	}
	return public
}
