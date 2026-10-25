package executor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"peekaping/internal/modules/shared"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTestFromEmail  = "test@example.com"
	defaultTestRelayEmail = "test@external-domain.com"
	// smtpClientName is the name sent in EHLO/HELO, the same default net/smtp uses.
	// Sending the target's own hostname instead trips servers' HELO checks.
	smtpClientName = "localhost"
)

type SMTPConfig struct {
	Host            string `json:"host" validate:"required" example:"smtp.example.com"`
	Port            int    `json:"port" validate:"required,min=1,max=65535" example:"587"`
	FromEmail       string `json:"from_email,omitempty" validate:"omitempty,email"`
	RcptToEmail     string `json:"rcpt_to_email,omitempty" validate:"omitempty,email"`
	UseTLS          bool   `json:"use_tls"`
	UseDirectTLS    bool   `json:"use_direct_tls"` // Direct TLS (SMTPS) for port 465, instead of STARTTLS
	IgnoreTlsErrors bool   `json:"ignore_tls_errors"`
	CheckCertExpiry bool   `json:"check_cert_expiry"`
	// ReadTimeout specifies the timeout for individual SMTP operations in seconds.
	// If not set, defaults to the monitor's global Timeout value.
	// Applies to: TLS handshake, greeting, EHLO/HELO, STARTTLS, AUTH, MAIL FROM, RCPT TO.
	// The whole check still ends when the monitor's Timeout runs out.
	ReadTimeout int    `json:"read_timeout,omitempty" validate:"omitempty,min=1,max=300" example:"10"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	// TestOpenRelay: If true, attempts to send to external domain without authentication
	// to test if the server is configured as an open relay
	TestOpenRelay bool `json:"test_open_relay"`
	// ExpectSecureRelay: If true, monitor expects relay to be REJECTED (secure configuration)
	// If false, monitor expects relay to be ALLOWED (intentional open relay)
	// Only used when TestOpenRelay is true. Defaults to true when omitted
	// (see Unmarshal)
	ExpectSecureRelay bool `json:"expect_secure_relay"`
}

type SMTPExecutor struct {
	logger *zap.SugaredLogger
}

func NewSMTPExecutor(logger *zap.SugaredLogger) *SMTPExecutor {
	return &SMTPExecutor{
		logger: logger,
	}
}

func (s *SMTPExecutor) Unmarshal(configJSON string) (any, error) {
	config, err := GenericUnmarshal[SMTPConfig](configJSON)
	if err != nil {
		return nil, err
	}

	// A config without expect_secure_relay expects the server to refuse
	// relaying, the same default as the form
	var relay struct {
		ExpectSecureRelay *bool `json:"expect_secure_relay"`
	}
	if err := json.Unmarshal([]byte(configJSON), &relay); err == nil && relay.ExpectSecureRelay == nil {
		config.ExpectSecureRelay = true
	}
	return config, nil
}

func (s *SMTPExecutor) Validate(configJSON string) error {
	config, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(config.(*SMTPConfig))
}

func (s *SMTPExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	configAny, err := s.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	config := configAny.(*SMTPConfig)

	s.logger.Debugf("execute smtp check for %s: %s:%d", m.Name, config.Host, config.Port)

	address := net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port))
	startTime := time.Now().UTC()

	var tlsInfo *shared.TLSInfo

	down := func(format string, args ...any) *Result {
		message := fmt.Sprintf(format, args...)
		s.logger.Debugf("SMTP check failed for %s: %s", m.Name, message)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   message,
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
			TLSInfo:   tlsInfo,
		}
	}

	// readTimeout: Maximum time to wait for each SMTP operation
	// Defaults to monitor's Timeout if not specified in config
	readTimeout := time.Duration(m.Timeout) * time.Second
	if config.ReadTimeout > 0 {
		readTimeout = time.Duration(config.ReadTimeout) * time.Second
	}

	// Create TLS configuration if TLS is enabled
	var tlsConfig *tls.Config
	if config.UseDirectTLS || config.UseTLS {
		tlsConfig = &tls.Config{
			ServerName:         config.Host,
			InsecureSkipVerify: config.IgnoreTlsErrors,
		}

		// Log warning when TLS verification is disabled
		if config.IgnoreTlsErrors {
			s.logger.Warnf("TLS certificate verification DISABLED for %s - vulnerable to man-in-the-middle attacks", m.Name)
		}
	}

	// Check for context cancellation before expensive operations
	select {
	case <-ctx.Done():
		return down("Health check cancelled")
	default:
	}

	dialer := &net.Dialer{
		Timeout: time.Duration(m.Timeout) * time.Second,
	}
	tcpConnection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return down("SMTP connection failed: %v", err)
	}
	// Closing the TCP connection also closes any TLS layer and SMTP client on top of it
	defer tcpConnection.Close()

	// setOperationDeadline gives the next SMTP operation readTimeout to finish,
	// but never lets it run past the deadline of the whole check
	setOperationDeadline := func() {
		var deadline time.Time
		if readTimeout > 0 {
			deadline = time.Now().Add(readTimeout)
		}
		if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
			deadline = ctxDeadline
		}
		tcpConnection.SetDeadline(deadline)
	}

	var smtpClient *smtp.Client
	setOperationDeadline()
	if config.UseDirectTLS {
		// Direct TLS (SMTPS - port 465): upgrade to TLS before the SMTP greeting
		tlsConnection := tls.Client(tcpConnection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return down("SMTP direct TLS handshake failed: %v", err)
		}
		setOperationDeadline()
		smtpClient, err = smtp.NewClient(tlsConnection, config.Host)
	} else {
		// Plain SMTP or STARTTLS: the greeting is read over the TCP connection
		smtpClient, err = smtp.NewClient(tcpConnection, config.Host)
	}
	if err != nil {
		return down("SMTP client creation failed: %v", err)
	}

	// Send EHLO/HELO command (required before STARTTLS)
	setOperationDeadline()
	if err := smtpClient.Hello(smtpClientName); err != nil {
		return down("SMTP HELLO failed: %v", err)
	}

	if config.UseTLS && !config.UseDirectTLS {
		setOperationDeadline()
		if err := smtpClient.StartTLS(tlsConfig); err != nil {
			return down("SMTP STARTTLS failed: %v", err)
		}
	}

	// Extract TLS certificate information if cert expiry checking is enabled
	if config.CheckCertExpiry {
		if state, ok := smtpClient.TLSConnectionState(); ok && len(state.PeerCertificates) > 0 {
			tlsInfo = shared.ParseCertificateChain(state.PeerCertificates[0], len(state.VerifiedChains) > 0)
		}
	}

	// Test for open relay BEFORE authentication
	// Open relay test checks if server allows relaying WITHOUT authentication (security risk)
	relayed := false
	if config.TestOpenRelay {
		fromEmail := config.FromEmail
		if fromEmail == "" {
			fromEmail = defaultTestFromEmail
		}
		testRecipient := config.RcptToEmail
		if testRecipient == "" {
			testRecipient = defaultTestRelayEmail
		}
		s.logger.Debugf("Testing open relay with RCPT TO: %s (before authentication)", testRecipient)

		// A 5xx reply to MAIL FROM or RCPT TO means the server refused to relay.
		// Any other error (timeout, dropped connection, 4xx) leaves the answer unknown.
		setOperationDeadline()
		relayErr := smtpClient.Mail(fromEmail)
		if relayErr == nil {
			setOperationDeadline()
			relayErr = smtpClient.Rcpt(testRecipient)
		}
		if relayErr != nil && !isSMTPRejection(relayErr) {
			return down("SMTP open relay test failed: %v", relayErr)
		}
		relayed = relayErr == nil
		s.logger.Debugf("Open relay test result for %s: relayed=%v, error=%v", m.Name, relayed, relayErr)

		// Reset the connection to not actually send anything
		setOperationDeadline()
		if err := smtpClient.Reset(); err != nil {
			s.logger.Debugf("SMTP RSET failed (non-critical) for %s: %v", m.Name, err)
		}

		if config.ExpectSecureRelay && relayed {
			s.logger.Warnf("SECURITY: SMTP server allows open relay for %s (expected rejection)", m.Name)
			return down("SECURITY: Server allows open relay (expected rejection)")
		}
		if !config.ExpectSecureRelay && !relayed {
			return down("Server rejected relay (expected open relay) - response: %v", relayErr)
		}
	}

	// Authenticate if credentials are provided
	// Authentication happens AFTER open relay test to ensure we test relaying without auth
	// smtp.PlainAuth refuses to send credentials without TLS, except to localhost
	authenticated := false
	if config.Username != "" && config.Password != "" {
		setOperationDeadline()
		if err := smtpClient.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return down("SMTP authentication failed: %v", err)
		}
		authenticated = true
	}

	// Send MAIL FROM if FromEmail is configured (but not for open relay test, which is already done)
	if config.FromEmail != "" && !config.TestOpenRelay {
		setOperationDeadline()
		if err := smtpClient.Mail(config.FromEmail); err != nil {
			return down("SMTP MAIL FROM failed: %v", err)
		}

		// Reset the connection to not actually send anything
		setOperationDeadline()
		if err := smtpClient.Reset(); err != nil {
			s.logger.Debugf("SMTP RSET failed (non-critical) for %s: %v", m.Name, err)
		}
	}

	setOperationDeadline()
	if err := smtpClient.Quit(); err != nil {
		// QUIT errors are non-critical, just log them
		s.logger.Debugf("SMTP QUIT failed (non-critical) for %s: %v", m.Name, err)
	}

	endTime := time.Now().UTC()
	s.logger.Debugf("SMTP connection successful: %s", m.Name)

	message := "SMTP server is reachable"
	switch {
	case tlsConfig != nil && authenticated:
		message += " (TLS + Auth)"
	case tlsConfig != nil:
		message += " (TLS)"
	case authenticated:
		message += " (Auth)"
	}

	// Add open relay status to message if testing was enabled
	if config.TestOpenRelay {
		if relayed {
			message += ", Open relay detected"
		} else {
			message += ", Open relay rejected (secure)"
		}
	}

	// Add certificate info to message if available
	if tlsInfo != nil && tlsInfo.Valid && tlsInfo.CertInfo != nil {
		message = fmt.Sprintf("%s, Certificate expires in %d days", message, tlsInfo.CertInfo.DaysRemaining)
	}

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
		TLSInfo:   tlsInfo,
	}
}

// isSMTPRejection reports whether err is a permanent (5xx) SMTP reply,
// as opposed to a temporary refusal or a broken connection.
func isSMTPRejection(err error) bool {
	var protoErr *textproto.Error
	return errors.As(err, &protoErr) && protoErr.Code >= 500
}
