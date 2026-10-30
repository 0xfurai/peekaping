package badge

import (
	"context"
	"fmt"
	"peekaping/internal/legacyid"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type Service interface {
	GenerateStatusBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error)
	GenerateUptimeBadge(ctx context.Context, monitorID string, duration int, options *BadgeOptions) (string, error)
	GeneratePingBadge(ctx context.Context, monitorID string, duration int, options *BadgeOptions) (string, error)
	GenerateCertExpBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error)
	GenerateResponseBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error)

	// Helper methods
	// ResolveMonitorID returns the ID of the monitor that a badge URL names
	ResolveMonitorID(ctx context.Context, id string) string
	GetMonitorBadgeData(ctx context.Context, monitorID string) (*MonitorBadgeData, error)
	// IsMonitorPublic is whether the monitor is active and on a published status page. It is
	// status_page.ErrUnavailable for one whose workspace is suspended.
	IsMonitorPublic(ctx context.Context, monitorID string) (bool, error)
}

type ServiceImpl struct {
	monitorService    monitor.Service
	heartbeatService  heartbeat.Service
	statsService      stats.Service
	tlsInfoService    monitor_tls_info.Service
	statusPageService status_page.Service
	svgGenerator      *SVGBadgeGenerator
	logger            *zap.SugaredLogger
}

func NewService(
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	statsService stats.Service,
	tlsInfoService monitor_tls_info.Service,
	statusPageService status_page.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		monitorService:    monitorService,
		heartbeatService:  heartbeatService,
		statsService:      statsService,
		tlsInfoService:    tlsInfoService,
		statusPageService: statusPageService,
		svgGenerator:      NewSVGBadgeGenerator(),
		logger:            logger.Named("[badge-service]"),
	}
}

// getLabel returns the provided label or defaultLabel if label is empty
func getLabel(label, defaultLabel string) string {
	if label == "" {
		return defaultLabel
	}
	return label
}

// ResolveMonitorID returns the ID of the monitor that a badge URL names. A MongoDB
// install that was imported into SQL (`bun db import-mongo`) keeps its badge URLs,
// which carry the monitor's ObjectID, while the monitor's row has the UUID that
// ObjectID maps to. So a 24-hex ID that names no monitor is mapped to that UUID,
// if a monitor has it. Any other ID is returned as it is.
func (s *ServiceImpl) ResolveMonitorID(ctx context.Context, id string) string {
	mapped, ok := legacyid.FromObjectID(id)
	if !ok {
		return id
	}
	// The lookup of the ObjectID may fail instead of finding nothing: PostgreSQL
	// rejects it as a UUID.
	if found, err := s.monitorService.FindByIDAnyWorkspace(ctx, id); err == nil && found != nil {
		return id
	}
	if found, err := s.monitorService.FindByIDAnyWorkspace(ctx, mapped); err == nil && found != nil {
		return mapped
	}
	return id
}

func (s *ServiceImpl) IsMonitorPublic(ctx context.Context, monitorID string) (bool, error) {
	// Check if monitor exists and is active
	monitor, err := s.monitorService.FindByIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		return false, err
	}
	if monitor == nil || !monitor.Active {
		return false, nil
	}

	// Monitor is public if it's on at least one published status page
	published, err := s.statusPageService.IsMonitorPublishedAnyWorkspace(ctx, monitorID)
	if err != nil || !published {
		return false, err
	}
	// A badge says no more than its status page does: unavailable while the workspace is suspended
	if monitor.WorkspaceSuspended {
		return false, status_page.ErrUnavailable
	}
	return true, nil
}

func (s *ServiceImpl) GetMonitorBadgeData(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	// Get monitor basic info
	monitorModel, err := s.monitorService.FindByIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitor: %w", err)
	}
	if monitorModel == nil {
		return nil, fmt.Errorf("monitor not found")
	}

	data := &MonitorBadgeData{
		ID:     monitorModel.ID,
		Name:   monitorModel.Name,
		Status: int(monitorModel.Status),
		Active: monitorModel.Active,
	}

	// Get uptime statistics
	now := time.Now().UTC()

	// Get uptime and response time from the stats service. A badge shows the monitor as a whole,
	// so it counts the checks of every probe (the empty probe id).
	s.fillStats(ctx, monitorID, 24*time.Hour, now, &data.AvgPing24h, &data.Uptime24h)
	s.fillStats(ctx, monitorID, 30*24*time.Hour, now, &data.AvgPing30d, &data.Uptime30d)
	s.fillStats(ctx, monitorID, 90*24*time.Hour, now, &data.AvgPing90d, &data.Uptime90d)

	// Get latest heartbeat for last ping
	s.fillLastPing(ctx, monitorID, data)

	// Get TLS certificate info
	tlsInfo, err := s.tlsInfoService.GetTLSInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Extract certificate expiry information from TLS info
	fillCertExpiry(tlsInfo, data)

	return data, nil
}

// fillStats sets the uptime and response time of the window of the given age that ends at now,
// for the figures the stats have. A window that can't be read leaves them as they are.
func (s *ServiceImpl) fillStats(ctx context.Context, monitorID string, age time.Duration, now time.Time, avgPing, uptime **float64) {
	summary, err := s.statsService.Summarize(ctx, monitorID, "", now.Add(-age), now)
	if err != nil {
		return
	}
	if summary.AvgPing != nil {
		*avgPing = summary.AvgPing
	}
	if summary.Uptime != nil {
		*uptime = summary.Uptime
	}
}

// fillLastPing sets the ping of the latest heartbeat, if it can be read and there is one
func (s *ServiceImpl) fillLastPing(ctx context.Context, monitorID string, data *MonitorBadgeData) {
	heartbeats, err := s.heartbeatService.FindByMonitorIDPaginated(ctx, monitorID, 1, 0, nil, true)
	if err == nil && len(heartbeats) > 0 {
		data.LastPing = &heartbeats[0].Ping
	}
}

// fillCertExpiry sets the certificate expiry of the monitor, if its TLS info has a certificate
func fillCertExpiry(tlsInfo *monitor_tls_info.TLSInfo, data *MonitorBadgeData) {
	if tlsInfo != nil && tlsInfo.CertInfo != nil {
		expiryDate := tlsInfo.CertInfo.ValidTo
		data.CertExpiryDays = &tlsInfo.CertInfo.DaysRemaining
		data.CertExpiryDate = &expiryDate
	}
}

// getMonitorBasicInfo gets only basic monitor information (for status badges)
func (s *ServiceImpl) getMonitorBasicInfo(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	monitorModel, err := s.monitorService.FindByIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitor: %w", err)
	}
	if monitorModel == nil {
		return nil, fmt.Errorf("monitor not found")
	}

	return &MonitorBadgeData{
		ID:     monitorModel.ID,
		Name:   monitorModel.Name,
		Status: int(monitorModel.Status),
		Active: monitorModel.Active,
	}, nil
}

// getMonitorWithStats gets basic info plus stats for the specified duration
func (s *ServiceImpl) getMonitorWithStats(ctx context.Context, monitorID string, duration int) (*MonitorBadgeData, error) {
	data, err := s.getMonitorBasicInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Only fetch the specific duration stats needed
	switch {
	case duration <= 24:
		// Get 24h stats only
		s.fillStats(ctx, monitorID, 24*time.Hour, now, &data.AvgPing24h, &data.Uptime24h)
	case duration <= 720: // 30 days
		// Get 30d stats only
		s.fillStats(ctx, monitorID, 30*24*time.Hour, now, &data.AvgPing30d, &data.Uptime30d)
	default:
		// Get 90d stats only
		s.fillStats(ctx, monitorID, 90*24*time.Hour, now, &data.AvgPing90d, &data.Uptime90d)
	}

	return data, nil
}

// getMonitorWithCertInfo gets basic info plus certificate information
func (s *ServiceImpl) getMonitorWithCertInfo(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	data, err := s.getMonitorBasicInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Get TLS certificate info
	tlsInfo, err := s.tlsInfoService.GetTLSInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Extract certificate expiry information from TLS info
	fillCertExpiry(tlsInfo, data)

	return data, nil
}

// getMonitorWithLastPing gets basic info plus the latest heartbeat
func (s *ServiceImpl) getMonitorWithLastPing(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	data, err := s.getMonitorBasicInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Get latest heartbeat for last ping
	s.fillLastPing(ctx, monitorID, data)

	return data, nil
}

func (s *ServiceImpl) GenerateStatusBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error) {
	data, err := s.getMonitorBasicInfo(ctx, monitorID)
	if err != nil {
		return "", err
	}

	badge := &Badge{
		Type:       BadgeTypeStatus,
		Style:      options.Style,
		Label:      FormatLabel(getLabel(options.Label, "status"), options.LabelPrefix, options.LabelSuffix),
		Value:      data.GetStatusText(options),
		Color:      data.GetStatusColor(options),
		LabelColor: options.LabelColor,
	}

	if options.Color != "" {
		badge.Color = options.Color
	}

	return s.svgGenerator.GenerateBadge(badge), nil
}

func (s *ServiceImpl) GenerateUptimeBadge(ctx context.Context, monitorID string, duration int, options *BadgeOptions) (string, error) {
	data, err := s.getMonitorWithStats(ctx, monitorID, duration)
	if err != nil {
		return "", err
	}

	// Determine which uptime to use based on duration
	var uptime *float64
	var defaultSuffix string

	if duration <= 24 {
		defaultSuffix = "24h"
		uptime = data.Uptime24h
	} else if duration <= 720 { // 30 days
		defaultSuffix = "30d"
		uptime = data.Uptime30d
	} else {
		defaultSuffix = "90d"
		uptime = data.Uptime90d
	}

	// Use custom suffix if provided, otherwise use default
	suffix := options.Suffix
	if suffix == "" {
		suffix = "%"
	}

	label := getLabel(options.Label, "uptime")

	// Format label with period in parentheses
	labelText := FormatLabel(label, options.LabelPrefix, options.LabelSuffix)
	if defaultSuffix != "" {
		labelText = labelText + " (" + defaultSuffix + ")"
	}

	var value, color string
	if uptime != nil {
		value, color = fmt.Sprintf("%.1f", *uptime), GetUptimeColor(*uptime)
	} else {
		// A period with no heartbeats is not 0% uptime: show it as missing, in grey, with no unit
		value, suffix, color = "N/A", "", "#9f9f9f"
	}

	badge := &Badge{
		Type:       BadgeTypeUptime,
		Style:      options.Style,
		Label:      labelText,
		Value:      FormatValue(value, options.Prefix, suffix),
		Color:      color,
		LabelColor: options.LabelColor,
	}

	if options.Color != "" {
		badge.Color = options.Color
	}

	return s.svgGenerator.GenerateBadge(badge), nil
}

func (s *ServiceImpl) GeneratePingBadge(ctx context.Context, monitorID string, duration int, options *BadgeOptions) (string, error) {
	data, err := s.getMonitorWithStats(ctx, monitorID, duration)
	if err != nil {
		return "", err
	}

	// Determine which ping to use based on duration
	var ping *float64
	var defaultSuffix string

	if duration <= 24 {
		defaultSuffix = "24h"
		ping = data.AvgPing24h
	} else if duration <= 720 { // 30 days
		defaultSuffix = "30d"
		ping = data.AvgPing30d
	} else {
		defaultSuffix = "90d"
		ping = data.AvgPing90d
	}

	// Use custom suffix if provided, otherwise use default
	suffix := options.Suffix
	if suffix == "" {
		suffix = "ms"
	}

	label := getLabel(options.Label, "ping")

	// Format label with period in parentheses
	labelText := FormatLabel(label, options.LabelPrefix, options.LabelSuffix)
	if defaultSuffix != "" {
		labelText = labelText + " (" + defaultSuffix + ")"
	}

	value := "N/A"
	if ping != nil {
		value = fmt.Sprintf("%.0f", *ping)
	} else {
		// A missing figure takes no unit
		suffix = ""
	}

	badge := &Badge{
		Type:       BadgeTypePing,
		Style:      options.Style,
		Label:      labelText,
		Value:      FormatValue(value, options.Prefix, suffix),
		Color:      options.Color,
		LabelColor: options.LabelColor,
	}

	if badge.Color == "" {
		badge.Color = defaultBadgeColor
	}

	return s.svgGenerator.GenerateBadge(badge), nil
}

func (s *ServiceImpl) GenerateCertExpBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error) {
	data, err := s.getMonitorWithCertInfo(ctx, monitorID)
	if err != nil {
		return "", err
	}
	fmt.Println("data", data.CertExpiryDays)

	var value, color string

	if data.CertExpiryDays == nil {
		value = "N/A"
		color = "#9f9f9f"
	} else {
		value, color = GetCertExpiryStatus(*data.CertExpiryDays, options)
		fmt.Println("value", value)
	}

	label := getLabel(options.Label, "cert exp")

	badge := &Badge{
		Type:       BadgeTypeCertExp,
		Style:      options.Style,
		Label:      FormatLabel(label, options.LabelPrefix, options.LabelSuffix),
		Value:      FormatValue(value, options.Prefix, options.Suffix),
		Color:      color,
		LabelColor: options.LabelColor,
	}

	if options.Color != "" {
		badge.Color = options.Color
	}

	return s.svgGenerator.GenerateBadge(badge), nil
}

func (s *ServiceImpl) GenerateResponseBadge(ctx context.Context, monitorID string, options *BadgeOptions) (string, error) {
	data, err := s.getMonitorWithLastPing(ctx, monitorID)
	if err != nil {
		return "", err
	}

	// Use custom suffix if provided, otherwise use default
	suffix := options.Suffix
	if suffix == "" {
		suffix = "ms"
	}

	var value string
	if data.LastPing != nil {
		value = strconv.Itoa(*data.LastPing)
	} else {
		// A missing figure takes no unit
		value, suffix = "N/A", ""
	}

	label := getLabel(options.Label, "response")

	badge := &Badge{
		Type:       BadgeTypeResponse,
		Style:      options.Style,
		Label:      FormatLabel(label, options.LabelPrefix, options.LabelSuffix),
		Value:      FormatValue(value, options.Prefix, suffix),
		Color:      options.Color,
		LabelColor: options.LabelColor,
	}

	if badge.Color == "" {
		badge.Color = defaultBadgeColor
	}

	return s.svgGenerator.GenerateBadge(badge), nil
}
