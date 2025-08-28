package badge

import (
	"fmt"
	"net/http"
	"peekaping/src/utils"
	"strconv"

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
		logger:  logger.Named("[badge-controller]"),
	}
}

// parseQueryOptions parses badge options from query parameters
func (c *Controller) parseQueryOptions(ctx *gin.Context) *BadgeOptions {
	options := DefaultBadgeOptions()

	// Parse style
	if style := ctx.Query("style"); style != "" {
		switch style {
		case "flat", "flat-square", "plastic", "for-the-badge", "social":
			options.Style = BadgeStyle(style)
		}
	}

	// Parse common options
	if color := ctx.Query("color"); color != "" {
		options.Color = color
	}
	if labelColor := ctx.Query("labelColor"); labelColor != "" {
		options.LabelColor = labelColor
	}

	// Parse status badge options
	if upLabel := ctx.Query("upLabel"); upLabel != "" {
		options.UpLabel = upLabel
	}
	if downLabel := ctx.Query("downLabel"); downLabel != "" {
		options.DownLabel = downLabel
	}
	if upColor := ctx.Query("upColor"); upColor != "" {
		options.UpColor = upColor
	}
	if downColor := ctx.Query("downColor"); downColor != "" {
		options.DownColor = downColor
	}

	// Parse text customization options
	if labelPrefix := ctx.Query("labelPrefix"); labelPrefix != "" {
		options.LabelPrefix = labelPrefix
	}
	if label := ctx.Query("label"); label != "" {
		options.Label = label
	}
	if labelSuffix := ctx.Query("labelSuffix"); labelSuffix != "" {
		options.LabelSuffix = labelSuffix
	}
	if prefix := ctx.Query("prefix"); prefix != "" {
		options.Prefix = prefix
	}
	if suffix := ctx.Query("suffix"); suffix != "" {
		options.Suffix = suffix
	}

	// Parse certificate expiry options
	if warnDays := ctx.Query("warnDays"); warnDays != "" {
		if days, err := strconv.Atoi(warnDays); err == nil && days > 0 {
			options.WarnDays = days
		}
	}
	if downDays := ctx.Query("downDays"); downDays != "" {
		if days, err := strconv.Atoi(downDays); err == nil && days >= 0 {
			options.DownDays = days
		}
	}

	return options
}

// @Router		/badge/{monitorId}/status [get]
// @Summary		Get status badge
// @Tags			Badges
// @Produce		image/svg+xml
// @Param			monitorId	path	string	true	"Monitor ID"
// @Param			style		query	string	false	"Badge style (flat, flat-square, plastic, for-the-badge, social)"
// @Param			upLabel		query	string	false	"Label when monitor is up"
// @Param			downLabel	query	string	false	"Label when monitor is down"
// @Param			upColor		query	string	false	"Color when monitor is up"
// @Param			downColor	query	string	false	"Color when monitor is down"
// @Success		200	{string}	string	"SVG badge"
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) GetStatusBadge(ctx *gin.Context) {
	monitorID := ctx.Param("monitorId")
	if monitorID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
		return
	}

	// Check if monitor is public (published on a status page)
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if !isPublic {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
		return
	}

	options := c.parseQueryOptions(ctx)

	svg, err := c.service.GenerateStatusBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate status badge", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
		return
	}

	ctx.Header("Content-Type", "image/svg+xml")
	ctx.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	ctx.String(http.StatusOK, svg)
}

// @Router		/badge/{monitorId}/uptime/{duration} [get]
// @Summary		Get uptime badge
// @Tags			Badges
// @Produce		image/svg+xml
// @Param			monitorId	path	string	true	"Monitor ID"
// @Param			duration	path	int		true	"Duration in hours (24, 720, 2160)"
// @Param			style		query	string	false	"Badge style"
// @Param			label		query	string	false	"Custom label"
// @Param			suffix		query	string	false	"Value suffix"
// @Param			color		query	string	false	"Badge color"
// @Success		200	{string}	string	"SVG badge"
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) GetUptimeBadge(ctx *gin.Context) {
	monitorID := ctx.Param("monitorId")
	if monitorID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
		return
	}

	durationStr := ctx.Param("duration")
	duration, err := strconv.Atoi(durationStr)
	fmt.Println("duration", duration)
	if err != nil || duration <= 0 {
		duration = 24 // Default to 24 hours
	}

	// Check if monitor is public
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if !isPublic {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
		return
	}

	options := c.parseQueryOptions(ctx)

	svg, err := c.service.GenerateUptimeBadge(ctx, monitorID, duration, options)
	if err != nil {
		c.logger.Errorw("Failed to generate uptime badge", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
		return
	}

	ctx.Header("Content-Type", "image/svg+xml")
	ctx.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	ctx.String(http.StatusOK, svg)
}

// @Router		/badge/{monitorId}/ping/{duration} [get]
// @Summary		Get ping badge
// @Tags			Badges
// @Produce		image/svg+xml
// @Param			monitorId	path	string	true	"Monitor ID"
// @Param			duration	path	int		true	"Duration in hours"
// @Param			style		query	string	false	"Badge style"
// @Param			label		query	string	false	"Custom label"
// @Param			suffix		query	string	false	"Value suffix"
// @Param			color		query	string	false	"Badge color"
// @Success		200	{string}	string	"SVG badge"
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) GetPingBadge(ctx *gin.Context) {
	monitorID := ctx.Param("monitorId")
	if monitorID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
		return
	}

	durationStr := ctx.Param("duration")
	duration, err := strconv.Atoi(durationStr)
	if err != nil || duration <= 0 {
		duration = 24 // Default to 24 hours
	}

	// Check if monitor is public
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if !isPublic {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
		return
	}

	options := c.parseQueryOptions(ctx)

	svg, err := c.service.GeneratePingBadge(ctx, monitorID, duration, options)
	if err != nil {
		c.logger.Errorw("Failed to generate ping badge", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
		return
	}

	ctx.Header("Content-Type", "image/svg+xml")
	ctx.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	ctx.String(http.StatusOK, svg)
}

// @Router		/badge/{monitorId}/cert-exp [get]
// @Summary		Get certificate expiry badge
// @Tags			Badges
// @Produce		image/svg+xml
// @Param			monitorId	path	string	true	"Monitor ID"
// @Param			style		query	string	false	"Badge style"
// @Param			label		query	string	false	"Custom label"
// @Param			warnDays	query	int		false	"Days before expiry to show warning"
// @Param			downDays	query	int		false	"Days before expiry to show as down"
// @Success		200	{string}	string	"SVG badge"
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) GetCertExpBadge(ctx *gin.Context) {
	monitorID := ctx.Param("monitorId")
	if monitorID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
		return
	}

	// Check if monitor is public
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if !isPublic {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
		return
	}

	options := c.parseQueryOptions(ctx)

	svg, err := c.service.GenerateCertExpBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate cert-exp badge", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
		return
	}

	ctx.Header("Content-Type", "image/svg+xml")
	ctx.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	ctx.String(http.StatusOK, svg)
}

// @Router		/badge/{monitorId}/response [get]
// @Summary		Get response time badge
// @Tags			Badges
// @Produce		image/svg+xml
// @Param			monitorId	path	string	true	"Monitor ID"
// @Param			style		query	string	false	"Badge style"
// @Param			label		query	string	false	"Custom label"
// @Param			suffix		query	string	false	"Value suffix"
// @Param			color		query	string	false	"Badge color"
// @Success		200	{string}	string	"SVG badge"
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) GetResponseBadge(ctx *gin.Context) {
	monitorID := ctx.Param("monitorId")
	if monitorID == "" {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
		return
	}

	// Check if monitor is public
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if !isPublic {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
		return
	}

	options := c.parseQueryOptions(ctx)

	svg, err := c.service.GenerateResponseBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate response badge", "error", err, "monitorID", monitorID)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
		return
	}

	ctx.Header("Content-Type", "image/svg+xml")
	ctx.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	ctx.String(http.StatusOK, svg)
}
