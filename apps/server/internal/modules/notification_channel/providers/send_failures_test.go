package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// sendFailure is a notification a channel fails to send, and the error it returns
type sendFailure struct {
	name   string
	sender func(client *http.Client) goldenSender
	config string
	event  goldenEvent
	// notify is the notification of a case that none of the events is, and replaces the event
	notify goldenNotify
	// status and body are what the fake endpoint answers with; the status is 200 when left out
	status int
	body   string
	// transportError makes the request fail before the endpoint answers
	transportError bool
	// wantErr is what the error Send returns contains
	wantErr string
}

// badURL is a URL that url.Parse refuses, so that a request to it can't be made
const badURL = `http://exa\u007fmple.com/hook`

// failingTransport fails every request, as a network that is down does
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset by peer")
}

var sendFailures = []sendFailure{
	{name: "discord config that is not JSON", sender: discord, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "discord thread with a webhook URL that can't be parsed", sender: discord,
		config:  `{"webhook_url": "http://[::1", "message_type": "send_to_thread", "thread_id": "456"}`,
		wantErr: `invalid webhook URL: parse "http://[::1": missing ']' in host`},
	{name: "discord request that can't be made", sender: discord,
		config:  `{"webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse",
	},
	{name: "discord endpoint that is down", sender: discord, transportError: true,
		config:  `{"webhook_url": "https://discord.com/api/webhooks/123/token"}`,
		wantErr: "failed to send Discord webhook: Post \"https://discord.com/api/webhooks/123/token\": connection reset by peer"},
	{name: "discord status that is not a success", sender: discord, status: 500,
		config:  `{"webhook_url": "https://discord.com/api/webhooks/123/token"}`,
		wantErr: "Discord webhook returned status: 500 Internal Server Error"},

	{name: "google chat config that is not JSON", sender: googleChat, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "google chat request that can't be made", sender: googleChat,
		config:  `{"webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "google chat endpoint that is down", sender: googleChat, transportError: true,
		config:  `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages"}`,
		wantErr: "failed to send request: Post \"https://chat.googleapis.com/v1/spaces/AAA/messages\": connection reset by peer"},
	{name: "google chat status that is not a success", sender: googleChat, status: 403,
		config:  `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages"}`,
		wantErr: "Google Chat API returned status code: 403"},

	{name: "gotify config that is not JSON", sender: gotify, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "gotify request that can't be made", sender: gotify,
		config:  `{"server_url": "` + badURL + `", "application_token": "app-token"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "gotify endpoint that is down", sender: gotify, transportError: true,
		config:  `{"server_url": "https://gotify.example.com", "application_token": "app-token"}`,
		wantErr: "failed to send Gotify request: Post \"https://gotify.example.com/message?token=app-token\": connection reset by peer"},
	{name: "gotify status that is not a success", sender: gotify, status: 401,
		config:  `{"server_url": "https://gotify.example.com", "application_token": "app-token"}`,
		wantErr: "Gotify request failed with status 401"},

	{name: "line config that is not JSON", sender: line, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "line template that can't be rendered", sender: line,
		config:  `{"channel_access_token": "line-token", "user_id": "U123", "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "line endpoint that is down", sender: line, transportError: true,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "failed to send LINE message: Post \"https://api.line.me/v2/bot/message/push\": connection reset by peer"},
	{name: "line error with the reason the API gives", sender: line, status: 401, body: `{"message": "Authentication failed"}`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API error: Authentication failed (status: 401)"},
	{name: "line error with a body that has no reason", sender: line, status: 500, body: `{"details": []}`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API returned status: 500 Internal Server Error"},
	{name: "line error with a body that is not JSON", sender: line, status: 502, body: `bad gateway`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API returned status: 502 Bad Gateway"},

	{name: "grafana oncall config that is not JSON", sender: grafanaOncall, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "grafana oncall request that can't be made", sender: grafanaOncall,
		config:  `{"grafana_oncall_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "grafana oncall endpoint that is down", sender: grafanaOncall, transportError: true,
		config:  `{"grafana_oncall_url": "https://oncall.example.com/hook"}`,
		wantErr: "failed to send request: Post \"https://oncall.example.com/hook\": connection reset by peer"},
	{name: "grafana oncall status that is not a success", sender: grafanaOncall, status: 500, body: "boom",
		config:  `{"grafana_oncall_url": "https://oncall.example.com/hook"}`,
		wantErr: "grafana OnCall API returned status 500: boom"},

	{name: "pagerduty config that is not JSON", sender: pagerDuty, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "pagerduty request that can't be made", sender: pagerDuty,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "` + badURL + `"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "pagerduty endpoint that is down", sender: pagerDuty, transportError: true,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`,
		wantErr: "failed to send PagerDuty notification: Post \"https://events.pagerduty.com/v2/enqueue\": connection reset by peer"},
	{name: "pagerduty status that is not a success", sender: pagerDuty, status: 400,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`,
		wantErr: "PagerDuty notification failed with status code: 400"},

	{name: "pagertree config that is not JSON", sender: pagerTree, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "pagertree request that can't be made", sender: pagerTree,
		config:  `{"integrationUrl": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "pagertree endpoint that is down", sender: pagerTree, transportError: true,
		config:  `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`,
		wantErr: "failed to send PagerTree notification: Post \"https://api.pagertree.com/integration/int_123\": connection reset by peer"},
	{name: "pagertree status that is not a success", sender: pagerTree, status: 403, body: "denied",
		config:  `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`,
		wantErr: "PagerTree API returned status 403: denied"},
}

// TestSendFailures checks the error each channel returns when it can't send a notification
func TestSendFailures(t *testing.T) {
	for _, tc := range sendFailures {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer endpoint.Close()

			client := &http.Client{Transport: &recordingTransport{endpoint: endpoint}}
			if tc.transportError {
				client = &http.Client{Transport: failingTransport{}}
			}
			message, mon, hb := notificationOf(tc.event, tc.notify)
			err := tc.sender(client).Send(context.Background(), tc.config, message, mon, hb)

			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
