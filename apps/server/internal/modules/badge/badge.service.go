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
	if found, err := s.monitorService.FindByID(ctx, id); err == nil && found != nil {
		return id
	}
	if found, err := s.monitorService.FindByID(ctx, mapped); err == nil && found != nil {
		return mapped
	}
	return id
}

func (s *ServiceImpl) IsMonitorPublic(ctx context.Context, monitorID string) (bool, error) {
	// Check if monitor exists and is active
	monitor, err := s.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return false, err
	}
	if monitor == nil || !monitor.Active {
		return false, nil
	}

	// Monitor is public if it's on at least one published status page
	return s.statusPageService.IsMonitorPublished(ctx, monitorID)
}

func (s *ServiceImpl) GetMonitorBadgeData(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	// Get monitor basic info
	monitorModel, err := s.monitorService.FindByID(ctx, monitorID)
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
	since24h := now.Add(-24 * time.Hour)
	since30d := now.Add(-30 * 24 * time.Hour)
	since90d := now.Add(-90 * 24 * time.Hour)

	// Get 24h uptime and response time
	summary24h, err := s.statsService.Summarize(ctx, monitorID, "", since24h, now)
	if err == nil {
		if summary24h.AvgPing != nil {
			data.AvgPing24h = summary24h.AvgPing
		}
		if summary24h.Uptime != nil {
			data.Uptime24h = summary24h.Uptime
		}
	}

	// Get 30d uptime and response time
	summary30d, err := s.statsService.Summarize(ctx, monitorID, "", since30d, now)
	if err == nil {
		if summary30d.AvgPing != nil {
			data.AvgPing30d = summary30d.AvgPing
		}
		if summary30d.Uptime != nil {
			data.Uptime30d = summary30d.Uptime
		}
	}

	// Get 90d uptime and response time
	summary90d, err := s.statsService.Summarize(ctx, monitorID, "", since90d, now)
	if err == nil {
		if summary90d.AvgPing != nil {
			data.AvgPing90d = summary90d.AvgPing
		}
		if summary90d.Uptime != nil {
			data.Uptime90d = summary90d.Uptime
		}
	}

	// Get latest heartbeat for last ping
	heartbeats, err := s.heartbeatService.FindByMonitorIDPaginated(ctx, monitorID, 1, 0, nil, true)
	if err == nil && len(heartbeats) > 0 {
		data.LastPing = &heartbeats[0].Ping
	}

	// Get TLS certificate info
	tlsInfo, err := s.tlsInfoService.GetTLSInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Extract certificate expiry information from TLS info
	if tlsInfo != nil && tlsInfo.CertInfo != nil {
		expiryDate := tlsInfo.CertInfo.ValidTo
		data.CertExpiryDays = &tlsInfo.CertInfo.DaysRemaining
		data.CertExpiryDate = &expiryDate
	}

	return data, nil
}

// getMonitorBasicInfo gets only basic monitor information (for status badges)
func (s *ServiceImpl) getMonitorBasicInfo(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	monitorModel, err := s.monitorService.FindByID(ctx, monitorID)
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
	if duration <= 24 {
		// Get 24h stats only
		since24h := now.Add(-24 * time.Hour)
		summary24h, err := s.statsService.Summarize(ctx, monitorID, "", since24h, now)
		if err == nil {
			if summary24h.AvgPing != nil {
				data.AvgPing24h = summary24h.AvgPing
			}
			if summary24h.Uptime != nil {
				data.Uptime24h = summary24h.Uptime
			}
		}
	} else if duration <= 720 { // 30 days
		// Get 30d stats only
		since30d := now.Add(-30 * 24 * time.Hour)
		summary30d, err := s.statsService.Summarize(ctx, monitorID, "", since30d, now)
		if err == nil {
			if summary30d.AvgPing != nil {
				data.AvgPing30d = summary30d.AvgPing
			}
			if summary30d.Uptime != nil {
				data.Uptime30d = summary30d.Uptime
			}
		}
	} else {
		// Get 90d stats only
		since90d := now.Add(-90 * 24 * time.Hour)
		summary90d, err := s.statsService.Summarize(ctx, monitorID, "", since90d, now)
		if err == nil {
			if summary90d.AvgPing != nil {
				data.AvgPing90d = summary90d.AvgPing
			}
			if summary90d.Uptime != nil {
				data.Uptime90d = summary90d.Uptime
			}
		}
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
	if tlsInfo != nil && tlsInfo.CertInfo != nil {
		expiryDate := tlsInfo.CertInfo.ValidTo
		data.CertExpiryDays = &tlsInfo.CertInfo.DaysRemaining
		data.CertExpiryDate = &expiryDate
	}

	return data, nil
}

// getMonitorWithLastPing gets basic info plus the latest heartbeat
func (s *ServiceImpl) getMonitorWithLastPing(ctx context.Context, monitorID string) (*MonitorBadgeData, error) {
	data, err := s.getMonitorBasicInfo(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	// Get latest heartbeat for last ping
	heartbeats, err := s.heartbeatService.FindByMonitorIDPaginated(ctx, monitorID, 1, 0, nil, true)
	if err == nil && len(heartbeats) > 0 {
		data.LastPing = &heartbeats[0].Ping
	}

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
	var uptime float64
	var defaultSuffix string

	if duration <= 24 {
		defaultSuffix = "24h"
		if data.Uptime24h != nil {
			uptime = *data.Uptime24h
		}
	} else if duration <= 720 { // 30 days
		defaultSuffix = "30d"
		if data.Uptime30d != nil {
			uptime = *data.Uptime30d
		}
	} else {
		defaultSuffix = "90d"
		if data.Uptime90d != nil {
			uptime = *data.Uptime90d
		}
	}

	// Format uptime percentage (removed unused variable)

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

	badge := &Badge{
		Type:       BadgeTypeUptime,
		Style:      options.Style,
		Label:      labelText,
		Value:      FormatValue(fmt.Sprintf("%.1f", uptime), options.Prefix, suffix),
		Color:      GetUptimeColor(uptime),
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
	var ping float64
	var defaultSuffix string

	if duration <= 24 {
		defaultSuffix = "24h"
		if data.AvgPing24h != nil {
			ping = *data.AvgPing24h
		}
	} else if duration <= 720 { // 30 days
		defaultSuffix = "30d"
		if data.AvgPing30d != nil {
			ping = *data.AvgPing30d
		}
	} else {
		defaultSuffix = "90d"
		if data.AvgPing90d != nil {
			ping = *data.AvgPing90d
		}
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

	badge := &Badge{
		Type:       BadgeTypePing,
		Style:      options.Style,
		Label:      labelText,
		Value:      FormatValue(fmt.Sprintf("%.0f", ping), options.Prefix, suffix),
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
