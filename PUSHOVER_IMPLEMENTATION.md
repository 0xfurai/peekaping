# Pushover Notification Channel Implementation

## Overview
This implementation adds Pushover notification support to the Peekaping monitoring system, based on the provided Node.js example.

## Backend Implementation (Go)

### Files Created/Modified:
1. `apps/server/src/modules/notification_channel/providers/pushover.go` - New Pushover provider
2. `apps/server/src/modules/notification_channel/listener.go` - Registered the Pushover provider

### PushoverConfig Structure:
```go
type PushoverConfig struct {
    UserKey    string `json:"pushover_user_key" validate:"required"`
    AppToken   string `json:"pushover_app_token" validate:"required"`
    Device     string `json:"pushover_device"`
    Title      string `json:"pushover_title"`
    Priority   int    `json:"pushover_priority" validate:"min=-2,max=2"`
    Sounds     string `json:"pushover_sounds"`
    SoundsUp   string `json:"pushover_sounds_up"`
    TTL        int    `json:"pushover_ttl" validate:"min=0"`
}
```

### Key Features:
- **Required fields**: User key and app token
- **Optional fields**: Device, title, priority, sounds, TTL
- **Priority levels**: -2 (lowest) to 2 (emergency)
- **Sound differentiation**: Different sounds for up/down status
- **HTML support**: Messages are sent with HTML formatting enabled
- **Timestamp**: Automatically adds timestamp to messages when heartbeat is available
- **Sound selection**: Uses different sounds for up vs down status

## Frontend Implementation (React)

### Files Created/Modified:
1. `apps/web/src/app/notification-channels/integrations/pushover-form.tsx` - New Pushover form
2. `apps/web/src/app/notification-channels/components/create-edit-notification-channel.tsx` - Registered Pushover form

### Form Features:
- **User Key**: Required field with password input type
- **App Token**: Required field with password input type
- **Device**: Optional field for specific device targeting
- **Title**: Optional custom message title
- **Priority**: Dropdown with -2 to 2 priority levels
- **Sounds**: Dropdown with all available Pushover sounds
- **Sounds Up**: Separate sound selection for recovery notifications
- **TTL**: Time-to-live setting for message expiration

### Sound Options:
The form includes all standard Pushover sounds: pushover, bike, bugle, cashregister, classical, cosmic, falling, gamelan, incoming, intermission, magic, mechanical, pianobar, siren, spacealarm, tugboat, alien, climb, persistent, echo, updown, vibrate, none.

## API Integration

### Pushover API Details:
- **Endpoint**: `https://api.pushover.net/1/messages.json`
- **Method**: POST
- **Content-Type**: application/json
- **Authentication**: Via user key and app token

### Payload Structure:
```json
{
  "message": "Alert message with timestamp",
  "user": "user_key",
  "token": "app_token",
  "html": 1,
  "retry": "30",
  "expire": "3600",
  "title": "Custom title or 'Peekaping Notification'",
  "priority": 0,
  "sound": "pushover",
  "device": "optional_device",
  "ttl": 0
}
```

## Testing

The implementation includes:
- Form validation for required fields
- Test notification functionality
- Proper error handling for API failures
- Responsive form design matching the existing UI

## Usage

1. **Setup**: Users need to create a Pushover account and application
2. **Configuration**: Enter user key and app token in the form
3. **Customization**: Optionally configure device, title, priority, sounds, and TTL
4. **Testing**: Use the built-in test functionality to verify configuration

## References

- [Pushover API Documentation](https://pushover.net/api)
- [Pushover App Creation](https://pushover.net/apps/build)
- [Pushover Dashboard](https://pushover.net/)

## Notes

- The implementation follows the existing patterns in the codebase
- Both Go backend and React frontend are fully integrated
- Supports all major Pushover features including priority levels and sound selection
- HTML formatting is enabled for rich message display
- Automatic timestamp addition for heartbeat notifications