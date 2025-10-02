package badge

import (
	"fmt"
	"math"
	"strings"
)

// SVGBadgeGenerator generates SVG badges
type SVGBadgeGenerator struct{}

// NewSVGBadgeGenerator creates a new SVG badge generator
func NewSVGBadgeGenerator() *SVGBadgeGenerator {
	return &SVGBadgeGenerator{}
}

// GenerateBadge generates an SVG badge
func (g *SVGBadgeGenerator) GenerateBadge(badge *Badge) string {
	switch badge.Style {
	case BadgeStyleFlatSquare:
		return g.generateFlatSquareBadge(badge)
	case BadgeStylePlastic:
		return g.generatePlasticBadge(badge)
	case BadgeStyleForTheBadge:
		return g.generateForTheBadgeBadge(badge)
	case BadgeStyleSocial:
		return g.generateSocialBadge(badge)
	default:
		return g.generateFlatBadge(badge)
	}
}

// calculateTextWidth approximates text width in pixels with better accuracy
func (g *SVGBadgeGenerator) calculateTextWidth(text string, fontSize int) int {
	// More accurate character width calculation based on Verdana font metrics
	// Different characters have different widths
	width := 0.0
	charWidth := float64(fontSize) * 0.58 // Base character width for Verdana 11px

	for _, char := range text {
		switch char {
		case 'i', 'j', 'l', 't', '!', '|', ':', ';', ',', '.':
			width += charWidth * 0.5 // Narrow characters
		case 'm', 'w', 'M', 'W':
			width += charWidth * 1.4 // Wide characters
		case ' ':
			width += charWidth * 0.6 // Space
		default:
			width += charWidth // Regular characters
		}
	}

	return int(math.Ceil(width))
}

// generateFlatBadge generates a flat style badge with modern styling
func (g *SVGBadgeGenerator) generateFlatBadge(badge *Badge) string {
	// Better padding for modern look
	labelWidth := g.calculateTextWidth(badge.Label, 11) + 14
	valueWidth := g.calculateTextWidth(badge.Value, 11) + 14
	totalWidth := labelWidth + valueWidth

	labelX := labelWidth / 2
	valueX := labelWidth + (valueWidth / 2)

	// Ensure minimum width for better appearance
	if labelWidth < 25 {
		labelWidth = 25
	}
	if valueWidth < 25 {
		valueWidth = 25
	}
	totalWidth = labelWidth + valueWidth

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="20" role="img" aria-label="%s: %s">
    <title>%s: %s</title>
    <defs>
        <linearGradient id="s" x2="0" y2="100%%">
            <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
            <stop offset="1" stop-opacity=".1"/>
        </linearGradient>
        <clipPath id="r">
            <rect width="%d" height="20" rx="3" fill="#fff"/>
        </clipPath>
    </defs>
    <g clip-path="url(#r)">
        <rect width="%d" height="20" fill="%s"/>
        <rect x="%d" width="%d" height="20" fill="%s"/>
        <rect width="%d" height="20" fill="url(#s)"/>
    </g>
    <g fill="#fff" text-anchor="middle" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,Ubuntu,Cantarell,'Fira Sans','Droid Sans','Helvetica Neue',sans-serif" text-rendering="geometricPrecision" font-size="11" font-weight="400">
        <text aria-hidden="true" x="%d" y="15.5" fill="#010101" fill-opacity=".3">%s</text>
        <text x="%d" y="14.5" fill="#fff">%s</text>
        <text aria-hidden="true" x="%d" y="15.5" fill="#010101" fill-opacity=".3">%s</text>
        <text x="%d" y="14.5" fill="#fff">%s</text>
    </g>
</svg>`,
		totalWidth, SanitizeText(badge.Label), SanitizeText(badge.Value),
		SanitizeText(badge.Label), SanitizeText(badge.Value),
		totalWidth,
		labelWidth, badge.LabelColor,
		labelWidth, valueWidth, badge.Color,
		totalWidth,
		labelX, SanitizeText(badge.Label),
		labelX, SanitizeText(badge.Label),
		valueX, SanitizeText(badge.Value),
		valueX, SanitizeText(badge.Value))

	return svg
}

// generateFlatSquareBadge generates a flat-square style badge with modern styling
func (g *SVGBadgeGenerator) generateFlatSquareBadge(badge *Badge) string {
	labelWidth := g.calculateTextWidth(badge.Label, 11) + 14
	valueWidth := g.calculateTextWidth(badge.Value, 11) + 14
	totalWidth := labelWidth + valueWidth

	labelX := labelWidth / 2
	valueX := labelWidth + (valueWidth / 2)

	// Ensure minimum width for better appearance
	if labelWidth < 25 {
		labelWidth = 25
	}
	if valueWidth < 25 {
		valueWidth = 25
	}
	totalWidth = labelWidth + valueWidth

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="20" role="img" aria-label="%s: %s">
    <title>%s: %s</title>
    <g shape-rendering="crispEdges">
        <rect width="%d" height="20" fill="%s"/>
        <rect x="%d" width="%d" height="20" fill="%s"/>
    </g>
    <g fill="#fff" text-anchor="middle" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,Ubuntu,Cantarell,'Fira Sans','Droid Sans','Helvetica Neue',sans-serif" text-rendering="geometricPrecision" font-size="11" font-weight="400">
        <text x="%d" y="14.5" fill="#fff">%s</text>
        <text x="%d" y="14.5" fill="#fff">%s</text>
    </g>
</svg>`,
		totalWidth, SanitizeText(badge.Label), SanitizeText(badge.Value),
		SanitizeText(badge.Label), SanitizeText(badge.Value),
		labelWidth, badge.LabelColor,
		labelWidth, valueWidth, badge.Color,
		labelX, SanitizeText(badge.Label),
		valueX, SanitizeText(badge.Value))

	return svg
}

// generatePlasticBadge generates a plastic style badge
func (g *SVGBadgeGenerator) generatePlasticBadge(badge *Badge) string {
	labelWidth := g.calculateTextWidth(badge.Label, 11) + 12
	valueWidth := g.calculateTextWidth(badge.Value, 11) + 12
	totalWidth := labelWidth + valueWidth

	labelX := labelWidth / 2
	valueX := labelWidth + (valueWidth / 2)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="18" role="img" aria-label="%s: %s">
    <title>%s: %s</title>
    <linearGradient id="s" x2="0" y2="100%%">
        <stop offset="0" stop-color="#fff" stop-opacity=".7"/>
        <stop offset=".1" stop-color="#aaa" stop-opacity=".1"/>
        <stop offset=".9" stop-color="#000" stop-opacity=".3"/>
        <stop offset="1" stop-color="#000" stop-opacity=".5"/>
    </linearGradient>
    <clipPath id="r">
        <rect width="%d" height="18" rx="4" fill="#fff"/>
    </clipPath>
    <g clip-path="url(#r)">
        <rect width="%d" height="18" fill="%s"/>
        <rect x="%d" width="%d" height="18" fill="%s"/>
        <rect width="%d" height="18" fill="url(#s)"/>
    </g>
    <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110">
        <text aria-hidden="true" x="%d" y="140" fill="#010101" fill-opacity=".5" transform="scale(.1)" textLength="%d">%s</text>
        <text x="%d" y="130" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
        <text aria-hidden="true" x="%d" y="140" fill="#010101" fill-opacity=".5" transform="scale(.1)" textLength="%d">%s</text>
        <text x="%d" y="130" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
    </g>
</svg>`,
		totalWidth, SanitizeText(badge.Label), SanitizeText(badge.Value),
		SanitizeText(badge.Label), SanitizeText(badge.Value),
		totalWidth,
		labelWidth, badge.LabelColor,
		labelWidth, valueWidth, badge.Color,
		totalWidth,
		labelX*10, (labelWidth-12)*10, SanitizeText(badge.Label),
		labelX*10, (labelWidth-12)*10, SanitizeText(badge.Label),
		valueX*10, (valueWidth-12)*10, SanitizeText(badge.Value),
		valueX*10, (valueWidth-12)*10, SanitizeText(badge.Value))

	return svg
}

// generateForTheBadgeBadge generates a for-the-badge style badge
func (g *SVGBadgeGenerator) generateForTheBadgeBadge(badge *Badge) string {
	label := strings.ToUpper(badge.Label)
	value := strings.ToUpper(badge.Value)

	labelWidth := g.calculateTextWidth(label, 11) + 20
	valueWidth := g.calculateTextWidth(value, 11) + 20
	totalWidth := labelWidth + valueWidth

	labelX := labelWidth / 2
	valueX := labelWidth + (valueWidth / 2)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="28" role="img" aria-label="%s: %s">
    <title>%s: %s</title>
    <g shape-rendering="crispEdges">
        <rect width="%d" height="28" fill="%s"/>
        <rect x="%d" width="%d" height="28" fill="%s"/>
    </g>
    <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-weight="bold" font-size="100">
        <text x="%d" y="175" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
        <text x="%d" y="175" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
    </g>
</svg>`,
		totalWidth, SanitizeText(badge.Label), SanitizeText(badge.Value),
		SanitizeText(badge.Label), SanitizeText(badge.Value),
		labelWidth, badge.LabelColor,
		labelWidth, valueWidth, badge.Color,
		labelX*10, (labelWidth-20)*10, SanitizeText(label),
		valueX*10, (valueWidth-20)*10, SanitizeText(value))

	return svg
}

// generateSocialBadge generates a social style badge
func (g *SVGBadgeGenerator) generateSocialBadge(badge *Badge) string {
	labelWidth := g.calculateTextWidth(badge.Label, 11) + 12
	valueWidth := g.calculateTextWidth(badge.Value, 11) + 12
	totalWidth := labelWidth + valueWidth + 6

	labelX := labelWidth / 2
	valueX := labelWidth + 3 + (valueWidth / 2)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="20" role="img" aria-label="%s: %s">
    <title>%s: %s</title>
    <linearGradient id="s" x2="0" y2="100%%">
        <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
        <stop offset="1" stop-opacity=".1"/>
    </linearGradient>
    <clipPath id="r">
        <rect width="%d" height="20" rx="3" fill="#fff"/>
    </clipPath>
    <g clip-path="url(#r)">
        <rect width="%d" height="20" fill="%s"/>
        <rect x="%d" width="%d" height="20" fill="%s"/>
        <rect width="%d" height="20" fill="url(#s)"/>
    </g>
    <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110">
        <text aria-hidden="true" x="%d" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="%d">%s</text>
        <text x="%d" y="140" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
        <text aria-hidden="true" x="%d" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="%d">%s</text>
        <text x="%d" y="140" transform="scale(.1)" fill="#fff" textLength="%d">%s</text>
    </g>
</svg>`,
		totalWidth, SanitizeText(badge.Label), SanitizeText(badge.Value),
		SanitizeText(badge.Label), SanitizeText(badge.Value),
		totalWidth,
		labelWidth, badge.LabelColor,
		labelWidth+3, valueWidth, badge.Color,
		totalWidth,
		labelX*10, (labelWidth-12)*10, SanitizeText(badge.Label),
		labelX*10, (labelWidth-12)*10, SanitizeText(badge.Label),
		valueX*10, (valueWidth-12)*10, SanitizeText(badge.Value),
		valueX*10, (valueWidth-12)*10, SanitizeText(badge.Value))

	return svg
}
