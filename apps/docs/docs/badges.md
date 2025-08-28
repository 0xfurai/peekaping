---
sidebar_position: 4
---

# Badges 🏷️

Peekaping provides real-time SVG badges that you can embed in your README files, websites, or dashboards to display the status and metrics of your monitored services.

## Overview

Badges are dynamically generated SVG images that show current status information for your monitors. They're perfect for:

- **GitHub README files** - Show service status at a glance
- **Documentation sites** - Display uptime statistics
- **Status dashboards** - Embed lightweight status indicators
- **Slack/Discord** - Share service health with your team

## Badge Types

### Status Badge

Shows the current operational status of your monitor.

```
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/status
```

**Possible values:**
- 🟢 **Up** - Service is operational
- 🔴 **Down** - Service is not responding
- 🟡 **Pending** - Monitor is checking
- 🟣 **Maintenance** - In maintenance mode
- ⚪ **Paused** - Monitor is disabled

### Uptime Badge

Displays uptime percentage for a specific time period.

```
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/uptime
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/uptime/{duration}
```

**Duration options:**
- `24` - Last 24 hours (default)
- `720` - Last 30 days (720 hours)
- `2160` - Last 90 days (2160 hours)

**Example:** `Uptime (24h): 99.9%`

### Ping Badge

Shows average response time for your monitor.

```
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/ping
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/ping/{duration}
```

**Duration options:** Same as uptime badge

**Example:** `Ping (30d): 45ms`



### Certificate Expiry Badge

Shows SSL certificate expiration information for HTTPS monitors.

```
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/cert-exp
```

**Color coding:**
- 🟢 **Green** - Certificate expires in > 14 days
- 🟡 **Orange** - Certificate expires in 7-14 days
- 🔴 **Red** - Certificate expires in < 7 days or expired

**Example:** `Cert Exp: 206 days`

### Response Time Badge

Shows the most recent response time measurement.

```
https://your-peekaping-instance.com/api/v1/badge/{monitorId}/response
```

**Example:** `Response: 2ms`

## Customization Options

All badges support query parameters for customization:

### Style Options

| Parameter | Values | Description |
|-----------|--------|-------------|
| `style` | `flat`, `flat-square`, `plastic`, `for-the-badge`, `social` | Badge visual style |

### Color Options

| Parameter | Values | Description |
|-----------|--------|-------------|
| `color` | Hex color (e.g., `4c1`, `007ec6`) | Override value background color |
| `labelColor` | Hex color (e.g., `555`, `333`) | Override label background color |

### Text Customization

| Parameter | Values | Description |
|-----------|--------|-------------|
| `label` | Any text | Custom label text |
| `labelPrefix` | Any text | Text before the label |
| `labelSuffix` | Any text | Text after the label |
| `prefix` | Any text | Text before the value |
| `suffix` | Any text | Text after the value |

### Status Badge Options

| Parameter | Values | Description |
|-----------|--------|-------------|
| `upLabel` | Any text | Custom text for "Up" status |
| `downLabel` | Any text | Custom text for "Down" status |
| `upColor` | Hex color | Color for "Up" status |
| `downColor` | Hex color | Color for "Down" status |

### Certificate Badge Options

| Parameter | Values | Description |
|-----------|--------|-------------|
| `warnDays` | Number | Days threshold for warning color (default: 14) |
| `downDays` | Number | Days threshold for critical color (default: 7) |

## Usage Examples

### Basic Status Badge

```html
<img src="https://your-peekaping-instance.com/api/v1/badge/monitor-123/status" alt="Service Status">
```

### Custom Styled Uptime Badge

```html
<img src="https://your-peekaping-instance.com/api/v1/badge/monitor-123/uptime/720?style=flat-square&color=brightgreen" alt="30-day Uptime">
```

### Markdown Usage

```markdown
![API Status](https://your-peekaping-instance.com/api/v1/badge/monitor-123/status)
![Uptime](https://your-peekaping-instance.com/api/v1/badge/monitor-123/uptime)
![Response Time](https://your-peekaping-instance.com/api/v1/badge/monitor-123/ping)
```

### Custom Labels

```html
<!-- Custom status labels -->
<img src="https://your-peekaping-instance.com/api/v1/badge/monitor-123/status?upLabel=Online&downLabel=Offline" alt="API Status">

<!-- Custom certificate warning threshold -->
<img src="https://your-peekaping-instance.com/api/v1/badge/monitor-123/cert-exp?warnDays=30&downDays=14" alt="SSL Certificate">

<!-- Custom prefix/suffix -->
<img src="https://your-peekaping-instance.com/api/v1/badge/monitor-123/ping?prefix=~&suffix=ms&label=Latency" alt="API Latency">
```

## Integration Examples

### GitHub README

```markdown
# My Service

[![Service Status](https://your-peekaping-instance.com/api/v1/badge/monitor-123/status?style=flat-square)](https://status.yourservice.com)
[![Uptime](https://your-peekaping-instance.com/api/v1/badge/monitor-123/uptime?style=flat-square)](https://status.yourservice.com)
[![Response Time](https://your-peekaping-instance.com/api/v1/badge/monitor-123/ping?style=flat-square)](https://status.yourservice.com)
[![SSL Cert](https://your-peekaping-instance.com/api/v1/badge/monitor-123/cert-exp?style=flat-square)](https://status.yourservice.com)

A reliable service with 99.9% uptime.
```

### Website Integration

```html
<div class="service-status">
  <h3>Service Health</h3>
  <p>
    <img src="https://your-peekaping-instance.com/api/v1/badge/api-monitor/status" alt="API Status" />
    <img src="https://your-peekaping-instance.com/api/v1/badge/api-monitor/uptime/720" alt="30-day Uptime" />
    <img src="https://your-peekaping-instance.com/api/v1/badge/api-monitor/ping/720" alt="Response Time" />
  </p>
</div>
```

### Status Dashboard

```html
<div class="status-grid">
  <div class="service">
    <h4>Web API</h4>
    <img src="https://your-peekaping-instance.com/api/v1/badge/web-api/status" />
    <img src="https://your-peekaping-instance.com/api/v1/badge/web-api/uptime" />
  </div>
  <div class="service">
    <h4>Database</h4>
    <img src="https://your-peekaping-instance.com/api/v1/badge/database/status" />
    <img src="https://your-peekaping-instance.com/api/v1/badge/database/ping" />
  </div>
</div>
```

## Security & Privacy

- **Public Access**: Badge endpoints are publicly accessible and don't require authentication
- **No Sensitive Data**: Badges only expose uptime metrics and status information
- **Monitor IDs**: Only monitors with public badges enabled will respond to badge requests
- **Rate Limiting**: Badge endpoints may be rate-limited to prevent abuse

## Badge Styles

### Default Styles

| Style | Description | Height |
|-------|-------------|--------|
| `flat` | Modern flat design with subtle gradients (default) | 20px |
| `flat-square` | Completely flat design with sharp corners | 20px |
| `plastic` | Glossy plastic-like appearance | 18px |
| `for-the-badge` | Large, bold badges with uppercase text | 28px |
| `social` | Social media friendly with rounded design | 20px |

### Color Schemes

**Status Colors:**
- 🟢 Up: `#4c1` (bright green)
- 🔴 Down: `#e05d44` (red)
- 🟡 Pending: `#fe7d37` (orange)
- 🟣 Maintenance: `#7c69ef` (purple)
- ⚪ Paused: `#9f9f9f` (gray)

**Uptime Colors:**
- 🟢 Excellent (≥ 99.5%): `#4c1`
- 🟢 Good (95-99.4%): `#4c1`
- 🟡 Fair (90-94.9%): `#97CA00`
- 🟠 Poor (80-89.9%): `#dfb317`
- 🔴 Critical (< 80%): `#e05d44`

## Best Practices

1. **Monitor Visibility**: Only enable public badges for monitors you want to be publicly visible
2. **Meaningful Names**: Use descriptive monitor names as they may appear in badge labels
3. **Appropriate Duration**: Choose duration periods that make sense for your service (24h for real-time, 30d for trends)
4. **Style Consistency**: Use consistent badge styles across your documentation
5. **Caching**: Badges are cached for performance - expect 1-2 minute delays for status updates
6. **Responsive Design**: Consider how badges appear on different screen sizes

## Troubleshooting

### Badge Not Loading

- Verify the monitor ID is correct
- Check that the monitor has public badges enabled
- Ensure your Peekaping instance is accessible from where you're viewing the badge

### Incorrect Data

- Badges show cached data with 1-2 minute delays
- Check that your monitor is actively collecting data
- Verify the time duration parameter is valid

### Styling Issues

- Use URL encoding for special characters in custom labels
- Test badge appearance in different contexts (light/dark backgrounds)
- Consider accessibility with appropriate color contrast
