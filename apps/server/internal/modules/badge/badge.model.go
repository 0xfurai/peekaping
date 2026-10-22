package badge

import (
	"fmt"
	"strings"
	"time"
)

// BadgeType represents the type of badge
type BadgeType string

const (
	BadgeTypeStatus   BadgeType = "status"
	BadgeTypeUptime   BadgeType = "uptime"
	BadgeTypePing     BadgeType = "ping"
	BadgeTypeCertExp  BadgeType = "cert-exp"
	BadgeTypeResponse BadgeType = "response"
)

// BadgeStyle represents the visual style of the badge
type BadgeStyle string

const (
	BadgeStyleFlat        BadgeStyle = "flat"
	BadgeStyleFlatSquare  BadgeStyle = "flat-square"
	BadgeStylePlastic     BadgeStyle = "plastic"
	BadgeStyleForTheBadge BadgeStyle = "for-the-badge"
	BadgeStyleSocial      BadgeStyle = "social"
)

// Badge represents a badge configuration
type Badge struct {
	Type       BadgeType  `json:"type"`
	Style      BadgeStyle `json:"style"`
	Label      string     `json:"label"`
	Value      string     `json:"value"`
	Color      string     `json:"color"`
	LabelColor string     `json:"label_color"`
}

// BadgeOptions represents customization options for badges
type BadgeOptions struct {
	// Common options
	Style      BadgeStyle `json:"style"`
	Color      string     `json:"color"`
	LabelColor string     `json:"label_color"`

	// Status badge options
	UpLabel   string `json:"up_label"`
	DownLabel string `json:"down_label"`
	UpColor   string `json:"up_color"`
	DownColor string `json:"down_color"`

	// Text customization options
	LabelPrefix string `json:"label_prefix"`
	Label       string `json:"label"`
	LabelSuffix string `json:"label_suffix"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`

	// Certificate expiry options
	WarnDays int `json:"warn_days"`
	DownDays int `json:"down_days"`
}

// defaultBadgeColor is the color the ping and response badges draw when no color
// is given. It's an ordinary color too: asking for blue gets the same hex.
const defaultBadgeColor = "#007ec6"

// DefaultBadgeOptions returns default badge options. Color is empty: no color was
// given, so each badge draws its own (the status badge its status color, the
// uptime badge its uptime color, and so on).
func DefaultBadgeOptions() *BadgeOptions {
	return &BadgeOptions{
		Style:      BadgeStyleFlat,
		Color:      "",
		LabelColor: "#555",
		UpLabel:    "Up",
		DownLabel:  "Down",
		UpColor:    "#4c1",    // Green for up status
		DownColor:  "#e05d44", // Red for down status
		Label:      "",
		Suffix:     "",
		WarnDays:   14,
		DownDays:   7,
	}
}

// MonitorBadgeData represents data needed for badge generation
type MonitorBadgeData struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"` // 0=down, 1=up, 2=pending, 3=maintenance
	Active bool   `json:"active"`

	// Statistics
	Uptime24h  *float64 `json:"uptime_24h"`
	Uptime30d  *float64 `json:"uptime_30d"`
	Uptime90d  *float64 `json:"uptime_90d"`
	AvgPing24h *float64 `json:"avg_ping_24h"`
	AvgPing30d *float64 `json:"avg_ping_30d"`
	AvgPing90d *float64 `json:"avg_ping_90d"`
	LastPing   *int     `json:"last_ping"`

	// Certificate info
	CertExpiryDays *int       `json:"cert_expiry_days"`
	CertExpiryDate *time.Time `json:"cert_expiry_date"`
}

// GetStatusText returns the text representation of monitor status
func (m *MonitorBadgeData) GetStatusText(options *BadgeOptions) string {
	if !m.Active {
		return "Paused"
	}

	switch m.Status {
	case 0:
		return options.DownLabel
	case 1:
		return options.UpLabel
	case 2:
		return "Pending"
	case 3:
		return "Maintenance"
	default:
		return "Unknown"
	}
}

// GetStatusColor returns the color for monitor status
func (m *MonitorBadgeData) GetStatusColor(options *BadgeOptions) string {
	if !m.Active {
		return "#9f9f9f"
	}

	switch m.Status {
	case 0:
		return options.DownColor
	case 1:
		return options.UpColor
	case 2:
		return "#fe7d37"
	case 3:
		return "#7c69ef"
	default:
		return "#9f9f9f"
	}
}

// GetUptimeColor returns color based on uptime percentage
func GetUptimeColor(uptime float64) string {
	if uptime >= 99.5 {
		return "#4c1" // Bright green for excellent uptime
	} else if uptime >= 95 {
		return "#4c1" // Green for good uptime
	} else if uptime >= 90 {
		return "#97CA00" // Light green
	} else if uptime >= 85 {
		return "#a4a61d" // Yellow-green
	} else if uptime >= 80 {
		return "#dfb317" // Yellow
	} else if uptime >= 70 {
		return "#fe7d37" // Orange
	} else {
		return "#e05d44" // Red for poor uptime
	}
}

// FormatValue formats a value with prefix and suffix
func FormatValue(value string, prefix, suffix string) string {
	result := value
	if prefix != "" {
		result = prefix + result
	}
	if suffix != "" {
		result = result + suffix
	}
	return result
}

// FormatLabel formats a label with prefix and suffix
func FormatLabel(label, prefix, suffix string) string {
	result := label
	if prefix != "" {
		result = prefix + result
	}
	if suffix != "" {
		result = result + suffix
	}
	return result
}

// colorNames are the shields.io color names a badge accepts, with the hex each is drawn as
var colorNames = map[string]string{
	"brightgreen":   "#4c1",
	"green":         "#97ca00",
	"yellowgreen":   "#a4a61d",
	"yellow":        "#dfb317",
	"orange":        "#fe7d37",
	"red":           "#e05d44",
	"blue":          "#007ec6",
	"grey":          "#555",
	"gray":          "#555",
	"lightgrey":     "#9f9f9f",
	"lightgray":     "#9f9f9f",
	"success":       "#4c1",
	"important":     "#fe7d37",
	"critical":      "#e05d44",
	"informational": "#007ec6",
	"inactive":      "#9f9f9f",
}

// parseColor turns the color of a badge URL into the hex color to draw: a hex
// color with or without its leading #, or a shields.io color name. It reports
// false for anything else, so no other text reaches the SVG.
func parseColor(value string) (string, bool) {
	hex := strings.TrimPrefix(value, "#")
	if (len(hex) == 3 || len(hex) == 6) && isHex(hex) {
		return "#" + hex, true
	}
	color, ok := colorNames[strings.ToLower(value)]
	return color, ok
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// SanitizeText sanitizes text for SVG output
func SanitizeText(text string) string {
	// Replace characters that could break SVG
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, "\"", "&quot;")
	text = strings.ReplaceAll(text, "'", "&#39;")
	return text
}

// GetCertExpiryStatus returns status based on certificate expiry days
func GetCertExpiryStatus(days int, options *BadgeOptions) (string, string) {
	if days < 0 {
		return "Expired", options.DownColor
	} else if days <= options.DownDays {
		return fmt.Sprintf("%dd", days), options.DownColor
	} else if days <= options.WarnDays {
		return fmt.Sprintf("%dd", days), "#fe7d37"
	} else {
		return fmt.Sprintf("%dd", days), options.UpColor
	}
}
