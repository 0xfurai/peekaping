# WhatsApp Notifications

Peekaping supports WhatsApp notifications through the WAHA (WhatsApp HTTP API) integration.

## Prerequisites

1. **WAHA Server**: You need a running WAHA server instance
2. **WhatsApp Session**: An active WhatsApp session connected to WAHA

## Setup

### 1. Install WAHA Server

You can run WAHA using Docker:

```bash
docker run -d \
  --name waha \
  -p 3000:3000 \
  devlikeapro/waha:latest
```

### 2. Access WAHA Dashboard

Open your browser and navigate to `http://localhost:3000` to access the WAHA dashboard.

### 3. Create a Session

1. In the WAHA dashboard, create a new session (usually named "default")
2. Scan the QR code with your WhatsApp mobile app
3. Wait for the connection to be established

## Configuration

### Required Fields

- **API URL**: The URL of your WAHA server (e.g., `http://localhost:3000`)
- **Session**: The session name you created in WAHA dashboard (e.g., "default")
- **Phone Number**: The recipient's phone number

### Optional Fields

- **API Key**: If your WAHA server requires authentication
- **Use Template**: Enable to use custom message templates
- **Template**: Custom message template with Liquid syntax
- **Custom Message**: Custom message content

## Phone Number Formats

You can use the following formats for the phone number:

- **Phone number with country code**: `1234567890`
- **Contact ID format**: `1234567890@c.us`
- **Group ID format**: `123456789012345678@g.us`

## Message Templates

When using templates, you can include the following variables:

- `{{ monitor.name }}` - Monitor name
- `{{ status }}` - Current status (up/down)
- `{{ msg }}` - Status message
- `{{ heartbeat.created_at }}` - Timestamp

### Example Template

```
🚨 Peekaping Alert

Monitor: {{ monitor.name }}
Status: {{ status }}
Message: {{ msg }}

Time: {{ heartbeat.created_at }}
```

## Testing

1. Create a WhatsApp notification channel in Peekaping
2. Configure the required fields
3. Click "Test" to send a test message
4. Check your WhatsApp for the received message

## Troubleshooting

### Common Issues

1. **Connection Refused**: Make sure WAHA server is running and accessible
2. **Session Not Found**: Verify the session name matches exactly
3. **Phone Number Invalid**: Check the phone number format
4. **Message Not Delivered**: Ensure WhatsApp is connected to the session

### Debug Logs

Check the Peekaping server logs for detailed debugging information:

```bash
docker logs peekaping-server-1
```

The logs will show:

- WAHA API request details
- Response status and body
- Any errors encountered

## Security Notes

- Keep your WAHA API key secure
- Use HTTPS in production environments
- Regularly update your WAHA server
- Monitor session status in WAHA dashboard

## Links

- [WAHA GitHub Repository](https://github.com/devlikeapro/waha)
- [WAHA Documentation](https://waha.devlike.pro/)
