package badge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/utils"
	"strconv"

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

// requirePublic is nil for a monitor on a published status page, 404 for any other, and the 503 of
// an unavailable status page when the monitor's workspace is suspended: a badge says no more than
// its page does (PP-372)
func (c *Controller) requirePublic(ctx context.Context, monitorID string) error {
	isPublic, err := c.service.IsMonitorPublic(ctx, monitorID)
	if errors.Is(err, status_page.ErrUnavailable) {
		return status_page.Unavailable()
	}
	if err != nil {
		c.logger.Errorw("Failed to check if monitor is public", "error", err, "monitorID", monitorID)
		return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if !isPublic {
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found or not public"))
	}
	return nil
}

// parseQueryOptions parses badge options from query parameters
func (c *Controller) parseQueryOptions(r *http.Request) *BadgeOptions {
	options := DefaultBadgeOptions()

	// Parse style
	if style := r.URL.Query().Get("style"); style != "" {
		switch style {
		case "flat", "flat-square", "plastic", "for-the-badge", "social":
			options.Style = BadgeStyle(style)
		}
	}

	// Parse common options. A color that isn't one leaves the default.
	if color, ok := parseColor(r.URL.Query().Get("color")); ok {
		options.Color = color
	}
	if labelColor, ok := parseColor(r.URL.Query().Get("labelColor")); ok {
		options.LabelColor = labelColor
	}

	// Parse status badge options
	if upLabel := r.URL.Query().Get("upLabel"); upLabel != "" {
		options.UpLabel = upLabel
	}
	if downLabel := r.URL.Query().Get("downLabel"); downLabel != "" {
		options.DownLabel = downLabel
	}
	if upColor, ok := parseColor(r.URL.Query().Get("upColor")); ok {
		options.UpColor = upColor
	}
	if downColor, ok := parseColor(r.URL.Query().Get("downColor")); ok {
		options.DownColor = downColor
	}

	// Parse text customization options
	if labelPrefix := r.URL.Query().Get("labelPrefix"); labelPrefix != "" {
		options.LabelPrefix = labelPrefix
	}
	if label := r.URL.Query().Get("label"); label != "" {
		options.Label = label
	}
	if labelSuffix := r.URL.Query().Get("labelSuffix"); labelSuffix != "" {
		options.LabelSuffix = labelSuffix
	}
	if prefix := r.URL.Query().Get("prefix"); prefix != "" {
		options.Prefix = prefix
	}
	if suffix := r.URL.Query().Get("suffix"); suffix != "" {
		options.Suffix = suffix
	}

	// Parse certificate expiry options
	if warnDays := r.URL.Query().Get("warnDays"); warnDays != "" {
		if days, err := strconv.Atoi(warnDays); err == nil && days > 0 {
			options.WarnDays = days
		}
	}
	if downDays := r.URL.Query().Get("downDays"); downDays != "" {
		if days, err := strconv.Atoi(downDays); err == nil && days >= 0 {
			options.DownDays = days
		}
	}

	return options
}
func (c *Controller) GetStatusBadge(ctx context.Context, input *GetStatusBadgeInput) (*httpx.Output[httpx.SVGBody], error) {
	r := input.Request
	monitorID := r.PathValue("monitorId")
	if monitorID == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
	}
	monitorID = c.service.ResolveMonitorID(ctx, monitorID)

	// Check if monitor is public (published on a status page)
	if err := c.requirePublic(ctx, monitorID); err != nil {
		return nil, err
	}

	options := c.parseQueryOptions(r)

	svg, err := c.service.GenerateStatusBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate status badge", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
	}
	return httpx.SVG(svg)
}
func (c *Controller) GetUptimeBadge(ctx context.Context, input *GetUptimeBadgeInput) (*httpx.Output[httpx.SVGBody], error) {
	r := input.Request
	monitorID := r.PathValue("monitorId")
	if monitorID == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
	}
	monitorID = c.service.ResolveMonitorID(ctx, monitorID)

	durationStr := r.PathValue("duration")
	duration, err := strconv.Atoi(durationStr)
	fmt.Println("duration", duration)
	if err != nil || duration <= 0 {
		duration = 24 // Default to 24 hours
	}

	// Check if monitor is public
	if err := c.requirePublic(ctx, monitorID); err != nil {
		return nil, err
	}

	options := c.parseQueryOptions(r)

	svg, err := c.service.GenerateUptimeBadge(ctx, monitorID, duration, options)
	if err != nil {
		c.logger.Errorw("Failed to generate uptime badge", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
	}
	return httpx.SVG(svg)
}
func (c *Controller) GetPingBadge(ctx context.Context, input *GetPingBadgeInput) (*httpx.Output[httpx.SVGBody], error) {
	r := input.Request
	monitorID := r.PathValue("monitorId")
	if monitorID == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
	}
	monitorID = c.service.ResolveMonitorID(ctx, monitorID)

	durationStr := r.PathValue("duration")
	duration, err := strconv.Atoi(durationStr)
	if err != nil || duration <= 0 {
		duration = 24 // Default to 24 hours
	}

	// Check if monitor is public
	if err := c.requirePublic(ctx, monitorID); err != nil {
		return nil, err
	}

	options := c.parseQueryOptions(r)

	svg, err := c.service.GeneratePingBadge(ctx, monitorID, duration, options)
	if err != nil {
		c.logger.Errorw("Failed to generate ping badge", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
	}
	return httpx.SVG(svg)
}
func (c *Controller) GetCertExpBadge(ctx context.Context, input *GetCertExpBadgeInput) (*httpx.Output[httpx.SVGBody], error) {
	r := input.Request
	monitorID := r.PathValue("monitorId")
	if monitorID == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
	}
	monitorID = c.service.ResolveMonitorID(ctx, monitorID)

	// Check if monitor is public
	if err := c.requirePublic(ctx, monitorID); err != nil {
		return nil, err
	}

	options := c.parseQueryOptions(r)

	svg, err := c.service.GenerateCertExpBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate cert-exp badge", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
	}
	return httpx.SVG(svg)
}
func (c *Controller) GetResponseBadge(ctx context.Context, input *GetResponseBadgeInput) (*httpx.Output[httpx.SVGBody], error) {
	r := input.Request
	monitorID := r.PathValue("monitorId")
	if monitorID == "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor ID is required"))
	}
	monitorID = c.service.ResolveMonitorID(ctx, monitorID)

	// Check if monitor is public
	if err := c.requirePublic(ctx, monitorID); err != nil {
		return nil, err
	}

	options := c.parseQueryOptions(r)

	svg, err := c.service.GenerateResponseBadge(ctx, monitorID, options)
	if err != nil {
		c.logger.Errorw("Failed to generate response badge", "error", err, "monitorID", monitorID)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to generate badge"))
	}
	return httpx.SVG(svg)
}
