package status_page

import (
	"context"
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
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
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(&dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	created, err := c.service.Create(ctx, workspaceID, &dto)
	if err != nil {
		if errors.Is(err, monitor_status_page.ErrUnknownMonitor) {
			return nil, unknownMonitorResponse()
		}
		// Surface domain uniqueness validation errors as 400
		var reached *limits.ReachedError
		if errors.As(err, &reached) {
			return nil, limits.Fail(reached)
		}
		var domainErr *DomainAlreadyUsedError
		if errors.As(err, &domainErr) {
			return nil, httpx.Fail(http.StatusBadRequest, DomainErrorResponse{Error: domainErr})
		}
		var slugErr *SlugAlreadyUsedError
		if errors.As(err, &slugErr) {
			return nil, httpx.Fail(http.StatusBadRequest, SlugErrorResponse{Error: slugErr})
		}
		c.logger.Errorw("Failed to create status page", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Status page created successfully", created))
}
func (c *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*StatusPageWithMonitorsResponseDTO]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")
	page, err := c.service.FindByIDWithMonitors(ctx, workspaceID, id)
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
	page, err := c.service.FindPublishedBySlugAnyWorkspace(ctx, slug)
	if errors.Is(err, ErrUnavailable) {
		return nil, Unavailable()
	}
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
	page, err := c.service.FindPublishedByDomainAnyWorkspace(ctx, domain)
	if errors.Is(err, ErrUnavailable) {
		return nil, Unavailable()
	}
	if err != nil {
		c.logger.Errorw("Failed to get status page by domain", "error", err, "domain", domain)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", page))
}

// TLSAsk answers Caddy's on_demand_tls ask: 200 and no body for a host on the custom domain list,
// 404 for any other. It names no page and says nothing else.
func (c *Controller) TLSAsk(ctx context.Context, input *TLSAskInput) (*httpx.Output[httpx.EmptyBody], error) {
	domain := input.Request.URL.Query().Get("domain")
	served, err := c.service.IsServedAnyWorkspace(ctx, domain)
	if err != nil {
		c.logger.Errorw("Failed to check a domain for the TLS ask", "error", err, "domain", domain)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if !served {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Not found"))
	}
	return httpx.Empty(http.StatusOK)
}
func (c *Controller) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*Model]], error) {
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

	pages, err := c.service.FindAll(ctx, workspaceID, page, limit, q)
	if err != nil {
		c.logger.Errorw("Failed to get all status pages", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", pages))
}
func (c *Controller) Update(ctx context.Context, input *UpdateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, c.logger, "Status page", findIn(c.service, workspaceID), id); err != nil {
		return nil, err
	}

	updated, err := c.service.Update(ctx, workspaceID, id, &dto)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, statusPageNotFoundResponse()
		}
		if errors.Is(err, monitor_status_page.ErrUnknownMonitor) {
			return nil, unknownMonitorResponse()
		}
		// Surface domain uniqueness validation errors as 400
		var reached *limits.ReachedError
		if errors.As(err, &reached) {
			return nil, limits.Fail(reached)
		}
		var domainErr *DomainAlreadyUsedError
		if errors.As(err, &domainErr) {
			return nil, httpx.Fail(http.StatusBadRequest, DomainErrorResponse{Error: domainErr})
		}
		var slugErr *SlugAlreadyUsedError
		if errors.As(err, &slugErr) {
			return nil, httpx.Fail(http.StatusBadRequest, SlugErrorResponse{Error: slugErr})
		}
		c.logger.Errorw("Failed to update status page", "error", err, "id", id)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Status page updated successfully", updated))
}
func (c *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")
	if _, err := utils.RequireRecord(ctx, c.logger, "Status page", findIn(c.service, workspaceID), id); err != nil {
		return nil, err
	}

	err = c.service.Delete(ctx, workspaceID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, statusPageNotFoundResponse()
		}
		c.logger.Errorw("Failed to delete status page", "error", err, "id", id)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Status page deleted successfully", nil))
}

// FindDomains answers the custom domains of a page of the request's workspace with their state
func (c *Controller) FindDomains(ctx context.Context, input *FindDomainsInput) (*httpx.Output[utils.ApiResponse[*DomainStatusesDTO]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	id := input.Request.PathValue("id")
	domains, err := c.service.DomainStatuses(ctx, workspaceID, id)
	if err != nil {
		return nil, c.domainFailure(err, "Failed to get the custom domains of a status page", id)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", domains))
}

// VerifyDomain checks one custom domain of a page of the request's workspace now and answers its
// state after the check
func (c *Controller) VerifyDomain(ctx context.Context, input *VerifyDomainInput) (*httpx.Output[utils.ApiResponse[*domain_status_page.Status]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	id, domain := input.Request.PathValue("id"), input.Request.PathValue("domain")
	status, err := c.service.CheckDomain(ctx, workspaceID, id, domain)
	if err != nil {
		return nil, c.domainFailure(err, "Failed to check a custom domain of a status page", id)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Custom domain checked", status))
}

// domainFailure is the answer for a failed domain request: 404 for a page that isn't in the
// workspace and for a domain the page doesn't list, and otherwise 500
func (c *Controller) domainFailure(err error, message, id string) error {
	if errors.Is(err, ErrNotFound) {
		return statusPageNotFoundResponse()
	}
	if errors.Is(err, domain_status_page.ErrDomainNotFound) {
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Custom domain not found"))
	}
	c.logger.Errorw(message, "error", err, "id", id)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// findIn is the lookup of a page in the workspace of the request: a page of another workspace is
// not found, as one that doesn't exist is
func findIn(service Service, workspaceID workspace.ID) func(context.Context, string) (*Model, error) {
	return func(ctx context.Context, id string) (*Model, error) {
		return service.FindByID(ctx, workspaceID, id)
	}
}

// statusPageNotFoundResponse is the answer for a page that doesn't exist and for one that belongs
// to another workspace, which a caller can't tell apart
func statusPageNotFoundResponse() error {
	return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
}

// unknownMonitorResponse is the 400 for a request that puts on the page a monitor that isn't in
// its workspace. The message names the kind and not the ID, so a monitor of another workspace and
// one that doesn't exist get the same answer (docs/adr/0005 section 8).
func unknownMonitorResponse() error {
	return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor not found"))
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
	page, err := c.publishedPage(ctx, slug)
	if err != nil {
		return nil, err
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
		monitorWithData, err := c.publicMonitor(ctx, page, msp.MonitorID, heartbeatLimit)
		if err != nil {
			return nil, err
		}
		if monitorWithData != nil {
			monitorModels = append(monitorModels, monitorWithData)
		}
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", monitorModels))
}

// publishedPage is the published status page at slug, or the error response for one that is not
// there: 503 when its workspace is suspended, 404 when it doesn't exist or isn't published
func (c *Controller) publishedPage(ctx context.Context, slug string) (*Model, error) {
	page, err := c.service.FindPublishedBySlugAnyWorkspace(ctx, slug)
	if errors.Is(err, ErrUnavailable) {
		return nil, Unavailable()
	}
	if err != nil {
		c.logger.Errorw("Failed to get status page by slug", "error", err, "slug", slug)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if page == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Status page not found"))
	}
	return page, nil
}

// publicMonitor is a monitor of the page with its latest heartbeats and its uptime. It is nil, with
// no error, for a monitor that can't be found, which the page leaves out.
func (c *Controller) publicMonitor(ctx context.Context, page *Model, monitorID string, heartbeatLimit int) (*MonitorWithHeartbeatsAndUptimeDTO, error) {
	// Get the actual monitor data
	monitorModel, err := c.monitorService.FindByIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to get monitor by ID", "error", err, "monitorID", monitorID)
		return nil, nil
	}
	if monitorModel == nil {
		return nil, nil
	}

	// Get the latest heartbeats for this monitor
	heartbeats, err := c.heartbeatService.FindByMonitorIDPaginated(ctx, monitorID, heartbeatLimit, 0, nil, true)
	if err != nil {
		c.logger.Errorw("Failed to get heartbeats for monitor", "error", err, "monitorID", monitorID)
		heartbeats = []*heartbeat.Model{} // Empty slice if error
	}

	uptime24h, err := c.uptime24h(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	monitorWithData := &MonitorWithHeartbeatsAndUptimeDTO{
		PublicMonitorDTO: &PublicMonitorDTO{
			ID:   monitorModel.ID,
			Type: monitorModel.Type,
			Name: monitorModel.Name,
			// A monitor of a type the install turns off is not checked, so a visitor sees it
			// paused, whatever its own setting is
			Active: monitorModel.Active && !monitorModel.TypeDisabled,
			Status: monitorModel.Status,
		},
		Heartbeats: publicHeartbeats(heartbeats),
		Uptime24h:  uptime24h,
	}
	if page.ShowLocations && len(monitorModel.Locations) > 1 {
		monitorWithData.Locations, err = c.publicLocations(ctx, monitorModel, heartbeatLimit)
		if err != nil {
			c.logger.Errorw("Failed to get locations for monitor", "error", err, "monitorID", monitorID)
			return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("failed to get locations for monitor"))
		}
	}

	return monitorWithData, nil
}

// uptime24h is the 24 hour uptime of the monitor, or the error response when it can't be read
func (c *Controller) uptime24h(ctx context.Context, monitorID string) (*float64, error) {
	now := time.Now().UTC()
	periods := map[string]time.Duration{
		"24h": 24 * time.Hour,
	}
	uptimeStats, err := c.heartbeatService.FindUptimeStatsByMonitorID(ctx, monitorID, periods, now)
	if err != nil {
		c.logger.Errorw("Failed to get uptime stats for monitor", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("failed to get uptime stats for monitor"))
	}

	// nil when the monitor has no heartbeat in the day: not 0%, which would read as an outage
	if uptimeStats == nil {
		return nil, nil
	}
	return uptimeStats["24h"], nil
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
		heartbeats, err := c.monitorService.GetHeartbeatsAnyWorkspace(ctx, m.ID, location.ProbeID, heartbeatLimit, 0, nil, true)
		if err != nil {
			c.logger.Errorw("Failed to get the heartbeats of a location", "error", err, "monitorID", m.ID, "probeID", location.ProbeID)
			heartbeats = nil
		}
		// Only the last 24 hours are shown. Nor does the page fail for want of this number: the
		// location is listed with a null uptime.
		uptime24h, err := c.monitorService.GetUptime24hAnyWorkspace(ctx, m.ID, location.ProbeID)
		if err != nil {
			c.logger.Errorw("Failed to get the 24h uptime of a location", "error", err, "monitorID", m.ID, "probeID", location.ProbeID)
			uptime24h = nil
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
