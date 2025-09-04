package incident

import (
	"net/http"
	"peekaping/src/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(service Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service: service,
		logger:  logger,
	}
}

// @Router    /incidents [post]
// @Summary   Create a new incident
// @Tags      Incidents
// @Accept    json
// @Produce   json
// @Security  BearerAuth
// @Param     body body CreateIncidentDTO true "Incident object"
// @Success   201  {object} utils.ApiResponse[Model]
// @Failure   400  {object} utils.APIError[any]
// @Failure   500  {object} utils.APIError[any]
func (c *Controller) Create(ctx *gin.Context) {
	var dto CreateIncidentDTO
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	created, err := c.service.Create(ctx, &dto)
	if err != nil {
		c.logger.Errorw("Failed to create incident", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Incident created successfully", created))
}

// @Router    /incidents/{id} [get]
// @Summary   Get an incident by ID
// @Tags      Incidents
// @Produce   json
// @Security  BearerAuth
// @Param     id   path      string  true  "Incident ID"
// @Success   200  {object}  utils.ApiResponse[Model]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("ID parameter is required"))
		return
	}

	incident, err := c.service.FindByID(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to find incident", "error", err, "id", id)
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Incident not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", incident))
}

// @Router    /incidents [get]
// @Summary   Get all incidents
// @Tags      Incidents
// @Produce   json
// @Security  BearerAuth
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

	incidents, err := c.service.FindAll(ctx, page, limit, q)
	if err != nil {
		c.logger.Errorw("Failed to fetch incidents", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", incidents))
}

// @Router    /incidents/status-page/{statusPageId} [get]
// @Summary   Get incidents by status page ID
// @Tags      Incidents
// @Produce   json
// @Param     statusPageId path string true "Status Page ID"
// @Success   200  {object}  utils.ApiResponse[[]Model]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) FindByStatusPageID(ctx *gin.Context) {
	statusPageID := ctx.Param("statusPageId")
	if statusPageID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Status page ID parameter is required"))
		return
	}

	incidents, err := c.service.FindByStatusPageID(ctx, statusPageID)
	if err != nil {
		c.logger.Errorw("Failed to fetch incidents by status page ID", "error", err, "status_page_id", statusPageID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", incidents))
}

// @Router    /incidents/{id} [patch]
// @Summary   Update an incident
// @Tags      Incidents
// @Accept    json
// @Produce   json
// @Security  BearerAuth
// @Param     id   path      string             true  "Incident ID"
// @Param     body body      UpdateIncidentDTO  true  "Incident update object"
// @Success   200  {object}  utils.ApiResponse[Model]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("ID parameter is required"))
		return
	}

	var dto UpdateIncidentDTO
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	updated, err := c.service.Update(ctx, id, &dto)
	if err != nil {
		c.logger.Errorw("Failed to update incident", "error", err, "id", id)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Incident updated successfully", updated))
}

// @Router    /incidents/{id} [delete]
// @Summary   Delete an incident
// @Tags      Incidents
// @Produce   json
// @Security  BearerAuth
// @Param     id   path      string  true  "Incident ID"
// @Success   200  {object}  utils.ApiResponse[any]
// @Failure   400  {object}  utils.APIError[any]
// @Failure   404  {object}  utils.APIError[any]
// @Failure   500  {object}  utils.APIError[any]
func (c *Controller) Delete(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("ID parameter is required"))
		return
	}

	err := c.service.Delete(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to delete incident", "error", err, "id", id)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Incident deleted successfully", struct{}{}))
}
