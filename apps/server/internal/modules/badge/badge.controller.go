package badge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	query := r.URL.Query()

	// Parse style
	switch style := query.Get("style"); style {
	case "flat", "flat-square", "plastic", "for-the-badge", "social":
		options.Style = BadgeStyle(style)
	}

	parseColorOptions(query, options)
	parseTextOptions(query, options)
	parseCertExpiryOptions(query, options)

	return options
}

// parseColorOptions reads the color options. A color that isn't one leaves the default.
func parseColorOptions(query url.Values, options *BadgeOptions) {
	for _, option := range []struct {
		key    string
		target *string
	}{
		{"color", &options.Color},
		{"labelColor", &options.LabelColor},
		{"upColor", &options.UpColor},
		{"downColor", &options.DownColor},
	} {
		if color, ok := parseColor(query.Get(option.key)); ok {
			*option.target = color
		}
	}
}

// parseTextOptions reads the status badge labels and the text customization options. An empty
// one leaves the default.
func parseTextOptions(query url.Values, options *BadgeOptions) {
	for _, option := range []struct {
		key    string
		target *string
	}{
		{"upLabel", &options.UpLabel},
		{"downLabel", &options.DownLabel},
		{"labelPrefix", &options.LabelPrefix},
		{"label", &options.Label},
		{"labelSuffix", &options.LabelSuffix},
		{"prefix", &options.Prefix},
		{"suffix", &options.Suffix},
	} {
		if text := query.Get(option.key); text != "" {
			*option.target = text
		}
	}
}

// parseCertExpiryOptions reads the certificate expiry options: warnDays above zero, downDays zero
// or more
func parseCertExpiryOptions(query url.Values, options *BadgeOptions) {
	if warnDays := query.Get("warnDays"); warnDays != "" {
		if days, err := strconv.Atoi(warnDays); err == nil && days > 0 {
			options.WarnDays = days
		}
	}
	if downDays := query.Get("downDays"); downDays != "" {
		if days, err := strconv.Atoi(downDays); err == nil && days >= 0 {
			options.DownDays = days
		}
	}
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
