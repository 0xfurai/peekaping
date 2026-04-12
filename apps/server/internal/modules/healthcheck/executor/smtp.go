package executor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/shared"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	authMethodPlain        = "PLAIN"
	defaultTestFromEmail   = "test@example.com"
	defaultTestRelayEmail  = "test@external-domain.com"
	plainAuthNullSeparator = "\x00"
	minRateLimitInterval   = 500 * time.Millisecond
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
	// ReadTimeout specifies the timeout for individual SMTP read operations in seconds.
	// If not set, defaults to the monitor's global Timeout value.
	// Applies to: greeting, EHLO/HELO, STARTTLS, AUTH responses, MAIL FROM, RCPT TO.
	// Note: The connection timeout uses the monitor's Timeout, while read operations
	// use ReadTimeout. Total operation time may span multiple read operations.
	ReadTimeout int    `json:"read_timeout,omitempty" validate:"omitempty,min=1,max=300" example:"10"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	// TestOpenRelay: If true, attempts to send to external domain without authentication
	// to test if the server is configured as an open relay
	TestOpenRelay bool `json:"test_open_relay"`
	// ExpectSecureRelay: If true, monitor expects relay to be REJECTED (secure configuration)
	// If false, monitor expects relay to be ALLOWED (intentional open relay)
	// Only used when TestOpenRelay is true
	ExpectSecureRelay bool `json:"expect_secure_relay"`
}

type SMTPExecutor struct {
	logger        *zap.SugaredLogger
	lastAttempts  map[string]time.Time
	attemptsMutex sync.RWMutex
}

// customPlainAuth implements smtp.Auth for PLAIN authentication with two-step flow
// This matches the behavior expected by many SMTP servers where:
// 1. Client sends: AUTH PLAIN
// 2. Server responds: 334 (continue)
// 3. Client sends: base64-encoded credentials
type customPlainAuth struct {
	identity string
	username string
	password string
	host     string
}

func (a *customPlainAuth) Start(server *smtp.ServerInfo) (proto string, toServer []byte, err error) {
	// Require TLS for all authentication (no exceptions)
	if !server.TLS {
		return "", nil, fmt.Errorf("authentication requires TLS connection (server: %s)", server.Name)
	}

	// Strict hostname validation
	if server.Name != a.host {
		return "", nil, fmt.Errorf("server hostname mismatch: expected %s, got %s", a.host, server.Name)
	}

	// Return PLAIN without credentials - we'll send them in Next()
	return authMethodPlain, nil, nil
}

func (a *customPlainAuth) Next(fromServer []byte, more bool) (toServer []byte, err error) {
	if more {
		// Server sent 334, now send credentials
		// Format: \0username\0password
		creds := fmt.Sprintf("%s%s%s%s%s",
			plainAuthNullSeparator,
			a.username,
			plainAuthNullSeparator,
			a.password,
			plainAuthNullSeparator)
		return []byte(creds), nil
	}
	return nil, nil
}

func NewSMTPExecutor(logger *zap.SugaredLogger) *SMTPExecutor {
	return &SMTPExecutor{
		logger:       logger,
		lastAttempts: make(map[string]time.Time),
	}
}

func (s *SMTPExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SMTPConfig](configJSON)
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

	s.logger.Debugf("execute smtp config: %+v", config)

	// Simple rate limiting: prevent flooding the same host
	hostKey := fmt.Sprintf("%s:%d", config.Host, config.Port)
	s.attemptsMutex.Lock()
	lastAttempt, exists := s.lastAttempts[hostKey]
	if exists && time.Since(lastAttempt) < minRateLimitInterval {
		s.attemptsMutex.Unlock()
		s.logger.Warnf("Rate limited SMTP check for %s (host: %s)", m.Name, hostKey)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Rate limited - too many attempts to same host",
			StartTime: time.Now().UTC(),
			EndTime:   time.Now().UTC(),
		}
	}
	s.lastAttempts[hostKey] = time.Now()
	s.attemptsMutex.Unlock()

	address := net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port))
	startTime := time.Now().UTC()

	var tlsInfo *certificate.TLSInfo
	var tlsConnection *tls.Conn

	// Determine if direct TLS (SMTPS) is configured
	// Direct TLS is used for port 465, where TLS is established immediately after connection
	isDirectTLS := config.UseDirectTLS

	// Determine read timeout (use ReadTimeout if specified, otherwise fall back to monitor timeout)
	// dialTimeout: Maximum time to establish TCP connection (uses monitor's Timeout)
	dialTimeout := time.Duration(m.Timeout) * time.Second

	// readTimeout: Maximum time to wait for server responses for each SMTP operation
	// Defaults to monitor's Timeout if not specified in config
	readTimeout := dialTimeout
	if config.ReadTimeout > 0 {
		readTimeout = time.Duration(config.ReadTimeout) * time.Second

		// Warn if readTimeout is significantly larger than dialTimeout
		if config.ReadTimeout > m.Timeout*2 {
			s.logger.Warnf("ReadTimeout (%ds) is significantly larger than monitor timeout (%ds) for %s",
				config.ReadTimeout, m.Timeout, m.Name)
		}
	}

	// Create TLS configuration if TLS is enabled
	var tlsConfig *tls.Config
	if isDirectTLS || config.UseTLS {
		tlsConfig = &tls.Config{
			ServerName:         config.Host,
			InsecureSkipVerify: config.IgnoreTlsErrors,
		}

		// Log warning when TLS verification is disabled
		if config.IgnoreTlsErrors {
			s.logger.Warnf("TLS certificate verification DISABLED for %s - vulnerable to man-in-the-middle attacks", m.Name)
		}
	}

	// Create a single dialer with timeout for all connection types
	dialer := &net.Dialer{
		Timeout: time.Duration(m.Timeout) * time.Second,
	}

	// Check for context cancellation before expensive operations
	select {
	case <-ctx.Done():
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Health check cancelled",
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
		}
	default:
	}

	// Dial TCP connection
	tcpConnection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		endTime := time.Now().UTC()
		s.logger.Debugf("SMTP connection failed for %s: %v", m.Name, err)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("SMTP connection failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Ensure connection cleanup on all exit paths
	var smtpClient *smtp.Client
	defer func() {
		if smtpClient != nil {
			smtpClient.Close()
		} else if tcpConnection != nil {
			tcpConnection.Close()
		}
	}()

	// Helper function to reset read deadline before each SMTP operation
	// This ensures each operation gets the full timeout, rather than sharing a single deadline
	setOperationDeadline := func() {
		if readTimeout > 0 {
			tcpConnection.SetReadDeadline(time.Now().Add(readTimeout))
		}
	}

	// Set initial read deadline
	setOperationDeadline()

	// Handle Direct TLS (SMTPS - port 465)
	if isDirectTLS {
		// Upgrade to TLS immediately (before SMTP handshake)
		setOperationDeadline() // Reset deadline for TLS handshake
		tlsConnection = tls.Client(tcpConnection, tlsConfig)
		if err := tlsConnection.Handshake(); err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP direct TLS handshake failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP direct TLS handshake failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		// Extract TLS certificate information if cert expiry checking is enabled
		if config.CheckCertExpiry {
			tlsInfo = certificate.ExtractCertificateFromTLSConn(tlsConnection)
		}

		// Create SMTP client from TLS connection
		setOperationDeadline() // Reset deadline for greeting read
		smtpClient, err = smtp.NewClient(tlsConnection, config.Host)
		if err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP client creation failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP client creation failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}
	} else {
		// Create SMTP client from TCP connection (for plain SMTP or STARTTLS)
		setOperationDeadline() // Reset deadline for greeting read
		smtpClient, err = smtp.NewClient(tcpConnection, config.Host)
		if err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP smtpClient creation failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP smtpClient creation failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	}

	// Send EHLO/HELO command (required before STARTTLS)
	if !isDirectTLS {
		setOperationDeadline() // Reset deadline for EHLO/HELO
		if err := smtpClient.Hello(config.Host); err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP HELLO failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP HELLO failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}
	}

	// Handle STARTTLS if TLS is enabled (but not direct TLS)
	if config.UseTLS && !isDirectTLS {
		setOperationDeadline() // Reset deadline for STARTTLS
		if err := smtpClient.StartTLS(tlsConfig); err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP STARTTLS failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP STARTTLS failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		// Extract TLS certificate information if cert expiry checking is enabled
		if config.CheckCertExpiry {
			state, ok := smtpClient.TLSConnectionState()
			if ok && len(state.PeerCertificates) > 0 {
				// Create a temporary TLS connection wrapper to use existing extraction logic
				// Note: We don't have direct access to tls.Conn via net/smtp, but we can extract from ConnectionState
				serverCert := state.PeerCertificates[0]
				verified := len(state.VerifiedChains) > 0
				tlsInfo = certificate.ParseCertificateChain(serverCert, verified)
			}
		}
	}

	// Track open relay status for success message
	var isOpenRelay *bool

	// Test for open relay BEFORE authentication
	// Open relay test checks if server allows relaying WITHOUT authentication (security risk)
	if config.TestOpenRelay {
		// Use configured FromEmail or default for open relay testing
		fromEmail := config.FromEmail
		if fromEmail == "" {
			fromEmail = defaultTestFromEmail
		}

		setOperationDeadline() // Reset deadline for MAIL FROM
		if err := smtpClient.Mail(fromEmail); err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP MAIL FROM failed during open relay test for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP MAIL FROM failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}

		// Use configured recipient email or default test email
		testRecipient := config.RcptToEmail
		if testRecipient == "" {
			testRecipient = defaultTestRelayEmail
		}
		s.logger.Debugf("Testing open relay with RCPT TO: %s (before authentication)", testRecipient)

		// Try RCPT TO and check if it's accepted
		setOperationDeadline() // Reset deadline for RCPT TO
		rcptErr := smtpClient.Rcpt(testRecipient)
		relayDetected := rcptErr == nil
		isOpenRelay = &relayDetected

		// Extract more details from error if available
		var lastResponse string
		if rcptErr != nil {
			lastResponse = rcptErr.Error()
		}
		s.logger.Debugf("Open relay test result: error=%v, detected=%v, response: %s", rcptErr, relayDetected, lastResponse)

		// Reset the connection to not actually send anything
		setOperationDeadline() // Reset deadline for RSET
		if err := smtpClient.Reset(); err != nil {
			s.logger.Debugf("SMTP RSET failed (non-critical) for %s: %v", m.Name, err)
		}

		// Determine if open relay configuration matches expectations
		// Simplified logic: compare relay status with expectations
		relayAllowed := relayDetected

		if config.ExpectSecureRelay {
			// We expect the server to REJECT relay (secure configuration)
			if relayAllowed {
				endTime := time.Now().UTC()
				s.logger.Warnf("SECURITY: SMTP server allows open relay for %s (expected rejection)", m.Name)
				return &Result{
					Status:    shared.MonitorStatusDown,
					Message:   fmt.Sprintf("SECURITY: Server allows open relay (expected rejection) - response: %s", lastResponse),
					StartTime: startTime,
					EndTime:   endTime,
					TLSInfo:   tlsInfo,
				}
			}
			// Relay correctly rejected (secure) - continue
			s.logger.Debugf("SMTP relay correctly rejected (secure) for %s", m.Name)
		} else {
			// We expect the server to ALLOW relay (intentional open relay)
			if !relayAllowed {
				endTime := time.Now().UTC()
				s.logger.Debugf("SMTP relay rejected for %s (expected open relay)", m.Name)
				return &Result{
					Status:    shared.MonitorStatusDown,
					Message:   fmt.Sprintf("Server rejected relay (expected open relay) - response: %s", lastResponse),
					StartTime: startTime,
					EndTime:   endTime,
					TLSInfo:   tlsInfo,
				}
			}
			// Open relay working as intended - continue
			s.logger.Debugf("SMTP open relay working as intended for %s", m.Name)
		}
	}

	// Authenticate if credentials are provided
	// Note: Authentication requires EHLO (ESMTP), which was already sent above
	// Authentication happens AFTER open relay test to ensure we test relaying without auth
	if config.Username != "" && config.Password != "" {
		auth := &customPlainAuth{
			username: config.Username,
			password: config.Password,
			host:     config.Host,
		}
		setOperationDeadline() // Reset deadline for AUTH
		if err := smtpClient.Auth(auth); err != nil {
			endTime := time.Now().UTC()
			s.logger.Warnf("SMTP authentication failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP authentication failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}
	}

	// Send MAIL FROM if FromEmail is configured (but not for open relay test, which is already done)
	if config.FromEmail != "" && !config.TestOpenRelay {
		setOperationDeadline() // Reset deadline for MAIL FROM
		if err := smtpClient.Mail(config.FromEmail); err != nil {
			endTime := time.Now().UTC()
			s.logger.Debugf("SMTP MAIL FROM failed for %s: %v", m.Name, err)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("SMTP MAIL FROM failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}

		// Reset the connection to not actually send anything
		setOperationDeadline() // Reset deadline for RSET
		if err := smtpClient.Reset(); err != nil {
			s.logger.Debugf("SMTP RSET failed (non-critical) for %s: %v", m.Name, err)
		}
	}

	// Send QUIT command
	if err := smtpClient.Quit(); err != nil {
		// QUIT errors are non-critical, just log them
		s.logger.Debugf("SMTP QUIT failed (non-critical) for %s: %v", m.Name, err)
	}

	endTime := time.Now().UTC()
	s.logger.Debugf("SMTP connection successful: %s", m.Name)

	var message string
	if isDirectTLS || config.UseTLS {
		if config.Username != "" {
			message = "SMTP server is reachable (TLS + Auth)"
		} else {
			message = "SMTP server is reachable (TLS)"
		}
	} else {
		if config.Username != "" {
			message = "SMTP server is reachable (Auth)"
		} else {
			message = "SMTP server is reachable"
		}
	}

	// Add open relay status to message if testing was enabled
	if config.TestOpenRelay && isOpenRelay != nil {
		if *isOpenRelay {
			message += ", Open relay detected"
		} else {
			message += ", Open relay rejected (secure)"
		}
	}

	// Add certificate info to message if available
	if tlsInfo != nil && tlsInfo.Valid && tlsInfo.CertInfo != nil {
		daysRemaining := tlsInfo.CertInfo.DaysRemaining
		message = fmt.Sprintf("%s, Certificate expires in %d days", message, daysRemaining)
	}

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
		TLSInfo:   tlsInfo,
	}
}
