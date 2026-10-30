package executor

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"peekaping/internal/hostport"
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
	guarded
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

// Targets lists the host an SMTP check dials
func (s *SMTPExecutor) Targets(configJSON string) ([]string, error) {
	config, err := GenericUnmarshal[SMTPConfig](configJSON)
	if err != nil {
		return nil, err
	}
	return []string{config.Host}, nil
}

func (s *SMTPExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	configAny, err := s.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	config := configAny.(*SMTPConfig)

	s.logger.Debugf("execute smtp check for %s: %s:%d", m.Name, config.Host, config.Port)

	address := hostport.Join(config.Host, config.Port)
	check := s.newSMTPCheck(ctx, m, config)

	// Check for context cancellation before expensive operations
	select {
	case <-ctx.Done():
		return check.down("Health check cancelled")
	default:
	}

	dialer := s.dialer(&net.Dialer{
		Timeout: time.Duration(m.Timeout) * time.Second,
	})
	check.tcpConnection, err = dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return check.down("SMTP connection failed: %v", err)
	}
	// Closing the TCP connection also closes any TLS layer and SMTP client on top of it
	defer check.tcpConnection.Close()

	return check.run()
}

// smtpCheck is one run of an SMTP check: the connection it makes to the server, and what it has
// learned of it so far
type smtpCheck struct {
	logger        *zap.SugaredLogger
	ctx           context.Context
	monitor       *Monitor
	config        *SMTPConfig
	startTime     time.Time
	readTimeout   time.Duration
	tlsConfig     *tls.Config
	tcpConnection net.Conn
	tlsInfo       *shared.TLSInfo
	authenticated bool
	relayed       bool
}

func (s *SMTPExecutor) newSMTPCheck(ctx context.Context, m *Monitor, config *SMTPConfig) *smtpCheck {
	check := &smtpCheck{
		logger:    s.logger,
		ctx:       ctx,
		monitor:   m,
		config:    config,
		startTime: time.Now().UTC(),
	}

	// readTimeout: Maximum time to wait for each SMTP operation
	// Defaults to monitor's Timeout if not specified in config
	check.readTimeout = time.Duration(m.Timeout) * time.Second
	if config.ReadTimeout > 0 {
		check.readTimeout = time.Duration(config.ReadTimeout) * time.Second
	}

	// Create TLS configuration if TLS is enabled
	if config.UseDirectTLS || config.UseTLS {
		check.tlsConfig = &tls.Config{
			ServerName:         hostport.Bare(config.Host),
			InsecureSkipVerify: config.IgnoreTlsErrors,
		}

		// Log warning when TLS verification is disabled
		if config.IgnoreTlsErrors {
			s.logger.Warnf("TLS certificate verification DISABLED for %s - vulnerable to man-in-the-middle attacks", m.Name)
		}
	}

	return check
}

// down is the Down result of the check, with the certificate information the check has so far
func (c *smtpCheck) down(format string, args ...any) *Result {
	message := fmt.Sprintf(format, args...)
	c.logger.Debugf("SMTP check failed for %s: %s", c.monitor.Name, message)
	return &Result{
		Status:    shared.MonitorStatusDown,
		Message:   message,
		StartTime: c.startTime,
		EndTime:   time.Now().UTC(),
		TLSInfo:   c.tlsInfo,
	}
}

// setOperationDeadline gives the next SMTP operation readTimeout to finish,
// but never lets it run past the deadline of the whole check
func (c *smtpCheck) setOperationDeadline() {
	var deadline time.Time
	if c.readTimeout > 0 {
		deadline = time.Now().Add(c.readTimeout)
	}
	if ctxDeadline, ok := c.ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	c.tcpConnection.SetDeadline(deadline)
}

// run talks to the server over the connection, and returns the result of the check
func (c *smtpCheck) run() *Result {
	smtpClient, failure := c.openClient()
	if failure != nil {
		return failure
	}

	steps := []func(*smtp.Client) *Result{c.greet, c.testOpenRelay, c.authenticate, c.checkMailFrom}
	for _, step := range steps {
		if failure := step(smtpClient); failure != nil {
			return failure
		}
	}

	c.setOperationDeadline()
	if err := smtpClient.Quit(); err != nil {
		// QUIT errors are non-critical, just log them
		c.logger.Debugf("SMTP QUIT failed (non-critical) for %s: %v", c.monitor.Name, err)
	}

	endTime := time.Now().UTC()
	c.logger.Debugf("SMTP connection successful: %s", c.monitor.Name)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   c.successMessage(),
		StartTime: c.startTime,
		EndTime:   endTime,
		TLSInfo:   c.tlsInfo,
	}
}

// openClient creates the SMTP client, which reads the server's greeting. With direct TLS, the
// connection is upgraded first.
func (c *smtpCheck) openClient() (*smtp.Client, *Result) {
	var smtpClient *smtp.Client
	var err error
	c.setOperationDeadline()
	if c.config.UseDirectTLS {
		// Direct TLS (SMTPS - port 465): upgrade to TLS before the SMTP greeting
		tlsConnection := tls.Client(c.tcpConnection, c.tlsConfig)
		if err := tlsConnection.HandshakeContext(c.ctx); err != nil {
			return nil, c.down("SMTP direct TLS handshake failed: %v", err)
		}
		c.setOperationDeadline()
		smtpClient, err = smtp.NewClient(tlsConnection, hostport.Bare(c.config.Host))
	} else {
		// Plain SMTP or STARTTLS: the greeting is read over the TCP connection
		smtpClient, err = smtp.NewClient(c.tcpConnection, hostport.Bare(c.config.Host))
	}
	if err != nil {
		return nil, c.down("SMTP client creation failed: %v", err)
	}
	return smtpClient, nil
}

// greet introduces the client to the server and, when the config asks for it, upgrades the
// connection with STARTTLS
func (c *smtpCheck) greet(smtpClient *smtp.Client) *Result {
	// Send EHLO/HELO command (required before STARTTLS)
	c.setOperationDeadline()
	if err := smtpClient.Hello(smtpClientName); err != nil {
		return c.down("SMTP HELLO failed: %v", err)
	}

	if c.config.UseTLS && !c.config.UseDirectTLS {
		c.setOperationDeadline()
		if err := smtpClient.StartTLS(c.tlsConfig); err != nil {
			return c.down("SMTP STARTTLS failed: %v", err)
		}
	}

	c.captureTLSInfo(smtpClient)
	return nil
}

// captureTLSInfo extracts TLS certificate information if cert expiry checking is enabled
func (c *smtpCheck) captureTLSInfo(smtpClient *smtp.Client) {
	if c.config.CheckCertExpiry {
		if state, ok := smtpClient.TLSConnectionState(); ok && len(state.PeerCertificates) > 0 {
			c.tlsInfo = shared.ParseCertificateChain(state.PeerCertificates[0], len(state.VerifiedChains) > 0)
		}
	}
}

// reset resets the connection to not actually send anything
func (c *smtpCheck) reset(smtpClient *smtp.Client) {
	c.setOperationDeadline()
	if err := smtpClient.Reset(); err != nil {
		c.logger.Debugf("SMTP RSET failed (non-critical) for %s: %v", c.monitor.Name, err)
	}
}

// testOpenRelay tests for open relay BEFORE authentication, if the config asks for it.
// Open relay test checks if server allows relaying WITHOUT authentication (security risk)
func (c *smtpCheck) testOpenRelay(smtpClient *smtp.Client) *Result {
	if !c.config.TestOpenRelay {
		return nil
	}

	fromEmail := cmp.Or(c.config.FromEmail, defaultTestFromEmail)
	testRecipient := cmp.Or(c.config.RcptToEmail, defaultTestRelayEmail)
	c.logger.Debugf("Testing open relay with RCPT TO: %s (before authentication)", testRecipient)

	// A 5xx reply to MAIL FROM or RCPT TO means the server refused to relay.
	// Any other error (timeout, dropped connection, 4xx) leaves the answer unknown.
	c.setOperationDeadline()
	relayErr := smtpClient.Mail(fromEmail)
	if relayErr == nil {
		c.setOperationDeadline()
		relayErr = smtpClient.Rcpt(testRecipient)
	}
	if relayErr != nil && !isSMTPRejection(relayErr) {
		return c.down("SMTP open relay test failed: %v", relayErr)
	}
	c.relayed = relayErr == nil
	c.logger.Debugf("Open relay test result for %s: relayed=%v, error=%v", c.monitor.Name, c.relayed, relayErr)

	c.reset(smtpClient)

	return c.relayVerdict(relayErr)
}

// relayVerdict is the Down result for an answer to the open relay test that isn't the one the
// config expects, and nil for the one it does
func (c *smtpCheck) relayVerdict(relayErr error) *Result {
	if c.config.ExpectSecureRelay && c.relayed {
		c.logger.Warnf("SECURITY: SMTP server allows open relay for %s (expected rejection)", c.monitor.Name)
		return c.down("SECURITY: Server allows open relay (expected rejection)")
	}
	if !c.config.ExpectSecureRelay && !c.relayed {
		return c.down("Server rejected relay (expected open relay) - response: %v", relayErr)
	}
	return nil
}

// authenticate logs in if credentials are provided.
// Authentication happens AFTER open relay test to ensure we test relaying without auth
// smtp.PlainAuth refuses to send credentials without TLS, except to localhost
func (c *smtpCheck) authenticate(smtpClient *smtp.Client) *Result {
	if c.config.Username == "" || c.config.Password == "" {
		return nil
	}

	c.setOperationDeadline()
	if err := smtpClient.Auth(smtp.PlainAuth("", c.config.Username, c.config.Password, hostport.Bare(c.config.Host))); err != nil {
		return c.down("SMTP authentication failed: %v", err)
	}
	c.authenticated = true
	return nil
}

// checkMailFrom sends MAIL FROM if FromEmail is configured (but not for open relay test, which is already done)
func (c *smtpCheck) checkMailFrom(smtpClient *smtp.Client) *Result {
	if c.config.FromEmail == "" || c.config.TestOpenRelay {
		return nil
	}

	c.setOperationDeadline()
	if err := smtpClient.Mail(c.config.FromEmail); err != nil {
		return c.down("SMTP MAIL FROM failed: %v", err)
	}

	c.reset(smtpClient)
	return nil
}

// successMessage describes the connection: its TLS and authentication, the open relay test and the
// certificate's expiry, as far as the check found out
func (c *smtpCheck) successMessage() string {
	message := "SMTP server is reachable" + c.securitySuffix() + c.relaySuffix()

	// Add certificate info to message if available
	if c.tlsInfo != nil && c.tlsInfo.Valid && c.tlsInfo.CertInfo != nil {
		message = fmt.Sprintf("%s, Certificate expires in %d days", message, c.tlsInfo.CertInfo.DaysRemaining)
	}

	return message
}

// securitySuffix says whether the connection used TLS and authentication
func (c *smtpCheck) securitySuffix() string {
	switch {
	case c.tlsConfig != nil && c.authenticated:
		return " (TLS + Auth)"
	case c.tlsConfig != nil:
		return " (TLS)"
	case c.authenticated:
		return " (Auth)"
	}
	return ""
}

// relaySuffix gives the open relay status if testing was enabled
func (c *smtpCheck) relaySuffix() string {
	if !c.config.TestOpenRelay {
		return ""
	}
	if c.relayed {
		return ", Open relay detected"
	}
	return ", Open relay rejected (secure)"
}

// isSMTPRejection reports whether err is a permanent (5xx) SMTP reply,
// as opposed to a temporary refusal or a broken connection.
func isSMTPRejection(err error) bool {
	var protoErr *textproto.Error
	return errors.As(err, &protoErr) && protoErr.Code >= 500
}
