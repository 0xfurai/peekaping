# Google Chat Notification Channel Implementation

## Overview

This implementation adds Google Chat notification support to the Peekaping monitoring system, allowing users to receive monitor alerts directly in their Google Chat spaces through webhooks.

## Implementation Details

### Backend Implementation (Go)

**File:** `apps/server/src/modules/notification_channel/providers/google_chat.go`

The Google Chat provider implements the `NotificationChannelProvider` interface with the following features:

- **Configuration**: Simple webhook URL configuration
- **Validation**: URL validation using struct tags and the existing validation system
- **Message Format**: Rich card format following Google Chat's message structure
- **Error Handling**: Proper error handling and logging
- **HTTP Client**: 30-second timeout for reliability

#### Key Features:
- ✅ Rich card messages with headers and sections
- ✅ Dynamic titles based on monitor status (up/down)
- ✅ Structured message layout with bold formatting
- ✅ Timestamp information
- ✅ Monitor link buttons (placeholder for base URL)
- ✅ Proper HTTP headers and user agent
- ✅ Error handling for API responses

#### Message Structure:
```json
{
  "fallbackText": "Peekaping Alert",
  "cardsV2": [
    {
      "card": {
        "header": {
          "title": "🔴 Monitor Name went down"
        },
        "sections": [
          {
            "widgets": [
              {
                "textParagraph": {
                  "text": "<b>Message:</b>\nAlert message here"
                }
              },
              {
                "textParagraph": {
                  "text": "<b>Time:</b>\n2024-01-15 14:30:00"
                }
              },
              {
                "buttonList": {
                  "buttons": [
                    {
                      "text": "Visit Peekaping",
                      "onClick": {
                        "openLink": {
                          "url": "/monitors/monitor-id"
                        }
                      }
                    }
                  ]
                }
              }
            ]
          }
        ]
      }
    }
  ]
}
```

### Backend Registration

**File:** `apps/server/src/modules/notification_channel/listener.go`

The Google Chat provider is registered in the notification system:

```go
RegisterNotificationChannelProvider("google_chat", providers.NewGoogleChatSender(p.Logger))
```

### Frontend Implementation (React/TypeScript)

**File:** `apps/web/src/app/notification-channels/integrations/google-chat-form.tsx`

The frontend form component provides:

- **Validation**: Zod schema validation for webhook URL
- **User Interface**: Clean, consistent form field with proper labels
- **Documentation**: Inline link to Google Chat webhook documentation
- **Type Safety**: TypeScript types for form values

#### Form Features:
- ✅ Required webhook URL field with URL validation
- ✅ Placeholder text with proper Google Chat URL format
- ✅ Required field indicators
- ✅ Documentation link to Google Chat webhooks
- ✅ Consistent styling with other notification forms

### Frontend Registration

**File:** `apps/web/src/app/notification-channels/components/create-edit-notification-channel.tsx`

The Google Chat form is integrated into the main notification system:

```typescript
// Import
import * as GoogleChatForm from "../integrations/google-chat-form";

// Registry
const typeFormRegistry = {
  // ... other forms
  google_chat: GoogleChatForm,
};

// Schema
z.discriminatedUnion("type", [
  // ... other schemas
  GoogleChatForm.schema,
] as const)
```

## Configuration

### Required Fields

- **webhook_url**: The Google Chat webhook URL (required)
  - Format: `https://chat.googleapis.com/v1/spaces/[SPACE_ID]/messages?key=[KEY]&token=[TOKEN]`
  - Validation: Must be a valid URL

### Setup Instructions

1. **Create a Google Chat Webhook:**
   - Go to Google Chat
   - Navigate to the space where you want to receive notifications
   - Click on the space name > "Manage webhooks"
   - Click "Add webhook"
   - Configure the webhook and copy the URL

2. **Configure in Peekaping:**
   - Navigate to "Notification Channels"
   - Click "Create New"
   - Select "Google Chat"
   - Enter a friendly name (e.g., "My Google Chat Alert")
   - Paste the webhook URL
   - Test the notification
   - Save the configuration

## Message Format

### Monitor Down Alert
```
🔴 Website Monitor went down

Message:
HTTP check failed: Connection timeout

Time:
2024-01-15 14:30:00

[Visit Peekaping] (button)
```

### Monitor Up Alert
```
✅ Website Monitor is back online

Message:
HTTP check succeeded

Time:
2024-01-15 14:35:00

[Visit Peekaping] (button)
```

## Integration Points

### Database Schema
The Google Chat notification is stored in the `notification_channels` table with:
- `type`: "google_chat"
- `config`: JSON string containing the webhook URL

### API Endpoints
- `POST /notification-channels` - Create Google Chat notification
- `PUT /notification-channels/{id}` - Update Google Chat notification
- `POST /notification-channels/test` - Test Google Chat notification

## Error Handling

The implementation includes comprehensive error handling:

- **Configuration Validation**: Invalid webhook URLs are rejected
- **HTTP Errors**: API errors are logged and reported
- **Timeout Handling**: 30-second timeout prevents hanging requests
- **JSON Marshaling**: Proper error handling for message formatting

## Testing

### Test Configuration
```json
{
  "webhook_url": "https://chat.googleapis.com/v1/spaces/AAAAA/messages?key=test&token=test"
}
```

### Test Scenarios
- ✅ Configuration validation
- ✅ Message formatting
- ✅ Error handling
- ✅ Frontend form validation
- ✅ Integration with notification system

## Files Created/Modified

### New Files:
1. `apps/server/src/modules/notification_channel/providers/google_chat.go`
2. `apps/web/src/app/notification-channels/integrations/google-chat-form.tsx`

### Modified Files:
1. `apps/server/src/modules/notification_channel/listener.go` - Added provider registration
2. `apps/web/src/app/notification-channels/components/create-edit-notification-channel.tsx` - Added form integration

## Dependencies

### Backend:
- No additional dependencies required (uses existing HTTP client and validation)

### Frontend:
- Uses existing form components and validation (Zod, React Hook Form)

## Future Enhancements

1. **Base URL Configuration**: Implement proper base URL handling for monitor links
2. **Custom Message Templates**: Add support for custom message formatting
3. **Threading**: Support for threaded conversations
4. **Mentions**: Add support for @mentions in notifications
5. **Rich Formatting**: Enhanced message formatting options

## Security Considerations

- Webhook URLs contain sensitive tokens and should be stored securely
- HTTPS is required for webhook URLs
- Rate limiting should be considered for high-frequency notifications
- Webhook URL validation prevents basic injection attacks

---

The Google Chat notification channel is now fully implemented and ready for use in the Peekaping monitoring system.