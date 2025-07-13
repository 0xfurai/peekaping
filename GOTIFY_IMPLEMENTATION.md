# Gotify Notification Channel Implementation

## Overview

This implementation adds Gotify notification support to the Peekaping monitoring system, allowing users to receive monitor alerts directly through their Gotify server.

## Implementation Details

### Backend Implementation (Go)

**File:** `apps/server/src/modules/notification_channel/providers/gotify.go`

The Gotify provider implements the `NotificationChannelProvider` interface with the following features:

- **Configuration**: Server URL, application token, priority, custom title, and custom message
- **Validation**: URL validation, required fields validation using struct tags
- **Message Format**: JSON payload with title, message, and priority
- **Error Handling**: Proper error handling and logging
- **HTTP Client**: 30-second timeout for reliability
- **Template Support**: Basic template variable replacement for custom messages

#### Key Features:
- ✅ Configurable server URL with trailing slash cleanup
- ✅ Application token authentication
- ✅ Priority levels (0-10, default 8)
- ✅ Custom title support
- ✅ Custom message templates with variables
- ✅ Template variable replacement for `{{ msg }}`, `{{ name }}`, and `{{ status }}`
- ✅ Proper HTTP headers and user agent
- ✅ Error handling for API responses

#### Configuration Structure:
```go
type GotifyConfig struct {
    ServerURL           string `json:"server_url" validate:"required,url"`
    ApplicationToken    string `json:"application_token" validate:"required"`
    Priority            int    `json:"priority" validate:"min=0,max=10"`
    Title               string `json:"title"`
    CustomMessage       string `json:"custom_message"`
}
```

#### API Request Format:
```json
{
    "title": "Peekaping",
    "message": "Monitor alert message",
    "priority": 8
}
```

### Frontend Implementation (React/TypeScript)

**File:** `apps/web/src/app/notification-channels/integrations/gotify-form.tsx`

The Gotify form component provides an intuitive interface for configuring Gotify notifications:

#### Form Fields:
- **Server URL**: URL of the Gotify server (validated as URL)
- **Application Token**: Token from Gotify application (hidden input)
- **Priority**: Priority level 0-10 (default 8)
- **Custom Title**: Optional custom title with template variables
- **Custom Message**: Optional custom message template with template variables

#### Validation:
- Server URL must be a valid URL
- Application token is required
- Priority must be between 0 and 10
- Custom fields are optional

#### Template Variables:
The form supports the following template variables:
- `{{ msg }}` - The notification message
- `{{ name }}` - The monitor name
- `{{ status }}` - The monitor status (UP/DOWN/PENDING/MAINTENANCE)

### Integration Points

1. **Backend Registration**: Added to `apps/server/src/modules/notification_channel/listener.go`
2. **Frontend Registration**: Added to `apps/web/src/app/notification-channels/components/create-edit-notification-channel.tsx`
3. **Schema Integration**: Included in the discriminated union for notification types

## Usage

1. **Setup Gotify Server**: Deploy a Gotify server instance
2. **Create Application**: Create an application in Gotify and get the token
3. **Configure in Peekaping**: 
   - Navigate to Notification Channels
   - Create new channel
   - Select "Gotify" type
   - Enter server URL and application token
   - Configure priority and optional custom messages

## Testing

- Backend compiles successfully with Go build
- Frontend compiles successfully with TypeScript
- Form validation works correctly
- Template variable replacement implemented

## Security Considerations

- Application token is masked in the UI
- Server URL is validated to prevent injection attacks
- HTTP client has reasonable timeout to prevent hanging requests
- Proper error handling to avoid information leakage

## Future Enhancements

Possible future improvements:
- Rich message formatting support
- Message attachment support
- Multiple application token support
- Advanced template engine integration (Liquid templates)
- Webhook-style message customization