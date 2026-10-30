package executor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSMTPExecutor_Execute_ResultMessages(t *testing.T) {
	const credentials = `"username": "testuser", "password": "testpass"`

	tests := []struct {
		name          string
		useTLS        bool
		requireAuth   bool
		allowRelay    bool
		config        string
		wantStatus    shared.MonitorStatus
		wantMessage   string
		wantTLSInfo   bool
		messagePrefix bool
	}{
		{name: "plain", config: `"use_tls": false`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable"},
		{name: "authenticated", requireAuth: true, config: `"use_tls": false, ` + credentials, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable (Auth)"},
		{name: "STARTTLS", useTLS: true, config: `"use_tls": true, "ignore_tls_errors": true`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable (TLS)"},
		{name: "STARTTLS and authenticated", useTLS: true, requireAuth: true, config: `"use_tls": true, "ignore_tls_errors": true, ` + credentials, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable (TLS + Auth)"},
		{name: "a user name without a password isn't authenticated", requireAuth: true, config: `"use_tls": false, "username": "testuser"`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable"},
		{name: "sender checked", config: `"use_tls": false, "from_email": "sender@example.com"`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable"},
		{name: "open relay detected and expected", allowRelay: true, config: `"use_tls": false, "test_open_relay": true, "expect_secure_relay": false`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable, Open relay detected"},
		{name: "open relay rejected and expected", config: `"use_tls": false, "test_open_relay": true, "expect_secure_relay": true`, wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable, Open relay rejected (secure)"},
		{
			name: "STARTTLS, authenticated and open relay rejected", useTLS: true, requireAuth: true,
			config:     `"use_tls": true, "ignore_tls_errors": true, "test_open_relay": true, "expect_secure_relay": true, ` + credentials,
			wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable (TLS + Auth), Open relay rejected (secure)",
		},
		{name: "open relay detected but not expected", allowRelay: true, config: `"use_tls": false, "test_open_relay": true, "expect_secure_relay": true`, wantStatus: shared.MonitorStatusDown, wantMessage: "SECURITY: Server allows open relay (expected rejection)"},
		{
			name: "open relay rejected but expected", config: `"use_tls": false, "test_open_relay": true, "expect_secure_relay": false`,
			wantStatus: shared.MonitorStatusDown, wantMessage: "Server rejected relay (expected open relay) - response: 550 \"Relaying denied\"",
		},
		{
			name: "authentication refused, with the certificate checked", useTLS: true, requireAuth: true,
			config:     `"use_tls": true, "ignore_tls_errors": true, "check_cert_expiry": true, "username": "testuser", "password": "wrong"`,
			wantStatus: shared.MonitorStatusDown, wantMessage: "SMTP authentication failed: ", messagePrefix: true, wantTLSInfo: true,
		},
		{
			name: "certificate checked without TLS", config: `"use_tls": false, "check_cert_expiry": true`,
			wantStatus: shared.MonitorStatusUp, wantMessage: "SMTP server is reachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, cleanup := setupMockSMTPServerWithRelay(t, tt.useTLS, tt.requireAuth, true, tt.allowRelay)
			defer cleanup()
			port := listener.Addr().(*net.TCPAddr).Port
			monitor := &Monitor{
				ID: "monitor1", Type: "smtp", Name: "Test SMTP Monitor", Interval: 30, Timeout: 5,
				Config: fmt.Sprintf(`{"host": "127.0.0.1", "port": %d, %s}`, port, tt.config),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := NewSMTPExecutor(zap.NewNop().Sugar()).Execute(ctx, monitor, nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			if tt.messagePrefix {
				assert.True(t, strings.HasPrefix(result.Message, tt.wantMessage), result.Message)
			} else {
				assert.Equal(t, tt.wantMessage, result.Message)
			}
			if tt.wantTLSInfo {
				require.NotNil(t, result.TLSInfo)
				assert.NotNil(t, result.TLSInfo.CertInfo)
			} else {
				assert.Nil(t, result.TLSInfo)
			}
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

// The mock server's certificate is self-signed, so no check here ends with a verified chain: the
// certificate's expiry is added to the message only for one.
func TestSMTPCheck_SuccessMessage_CertificateExpiry(t *testing.T) {
	verified := &shared.TLSInfo{Valid: true, CertInfo: &shared.CertificateInfo{DaysRemaining: 12}}
	unverified := &shared.TLSInfo{Valid: false, CertInfo: &shared.CertificateInfo{DaysRemaining: 12}}

	assert.Equal(t, "SMTP server is reachable, Certificate expires in 12 days",
		(&smtpCheck{config: &SMTPConfig{}, tlsInfo: verified}).successMessage())
	assert.Equal(t, "SMTP server is reachable (TLS + Auth), Open relay rejected (secure), Certificate expires in 12 days",
		(&smtpCheck{config: &SMTPConfig{TestOpenRelay: true}, tlsConfig: &tls.Config{}, authenticated: true, tlsInfo: verified}).successMessage())
	assert.Equal(t, "SMTP server is reachable (TLS)",
		(&smtpCheck{config: &SMTPConfig{}, tlsConfig: &tls.Config{}, tlsInfo: unverified}).successMessage())
	assert.Equal(t, "SMTP server is reachable",
		(&smtpCheck{config: &SMTPConfig{}, tlsInfo: &shared.TLSInfo{Valid: true}}).successMessage())
}
