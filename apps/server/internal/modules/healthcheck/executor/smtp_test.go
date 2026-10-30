package executor

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSMTPExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		expectedHost  string
		expectedPort  int
	}{
		{
			name: "valid config",
			config: `{
				"host": "smtp.example.com",
				"port": 587,
				"use_tls": true
			}`,
			expectedError: false,
			expectedHost:  "smtp.example.com",
			expectedPort:  587,
		},
		{
			name: "valid config with auth",
			config: `{
				"host": "smtp.example.com",
				"port": 465,
				"use_tls": true,
				"username": "user@example.com",
				"password": "secret"
			}`,
			expectedError: false,
			expectedHost:  "smtp.example.com",
			expectedPort:  465,
		},
		{
			name: "invalid json",
			config: `{
				"host": "smtp.example.com",
				invalid
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedHost != "" {
					cfg := result.(*SMTPConfig)
					assert.Equal(t, tt.expectedHost, cfg.Host)
					assert.Equal(t, tt.expectedPort, cfg.Port)
				}
			}
		})
	}
}

// A config without expect_secure_relay (e.g. created through the API) expects
// the server to refuse relaying, the same default as the form
func TestSMTPExecutor_Unmarshal_ExpectSecureRelayDefault(t *testing.T) {
	executor := NewSMTPExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name     string
		config   string
		expected bool
	}{
		{"omitted", `{"host": "smtp.example.com", "port": 25, "test_open_relay": true}`, true},
		{"explicit true", `{"host": "smtp.example.com", "port": 25, "test_open_relay": true, "expect_secure_relay": true}`, true},
		{"explicit false", `{"host": "smtp.example.com", "port": 25, "test_open_relay": true, "expect_secure_relay": false}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result.(*SMTPConfig).ExpectSecureRelay)
		})
	}
}

func TestSMTPExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid config with all fields",
			config: `{
				"host": "smtp.example.com",
				"port": 587,
				"use_tls": true,
				"ignore_tls_errors": false,
				"check_cert_expiry": true,
				"username": "user@example.com",
				"password": "password123",
				"from_email": "from@example.com"
			}`,
			expectedError: false,
		},
		{
			name: "missing required host",
			config: `{
				"port": 587,
				"use_tls": true
			}`,
			expectedError: true,
		},
		{
			name: "missing required port",
			config: `{
				"host": "smtp.example.com",
				"use_tls": true
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (too high)",
			config: `{
				"host": "smtp.example.com",
				"port": 70000,
				"use_tls": true
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (zero)",
			config: `{
				"host": "smtp.example.com",
				"port": 0,
				"use_tls": true
			}`,
			expectedError: true,
		},
		{
			name: "invalid from_email",
			config: `{
				"host": "smtp.example.com",
				"port": 587,
				"use_tls": true,
				"from_email": "not-an-email"
			}`,
			expectedError: true,
		},
		{
			name: "valid config without TLS",
			config: `{
				"host": "smtp.example.com",
				"port": 25,
				"use_tls": false
			}`,
			expectedError: false,
		},
		{
			name: "valid config with port 465 and direct TLS",
			config: `{
				"host": "smtp.example.com",
				"port": 465,
				"use_tls": true,
				"use_direct_tls": true
			}`,
			expectedError: false,
		},
		{
			name: "valid config with use_direct_tls",
			config: `{
				"host": "smtp.example.com",
				"port": 587,
				"use_tls": true,
				"use_direct_tls": true
			}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSMTPExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name           string
		setupServer    func() (net.Listener, func())
		config         string
		expectedStatus shared.MonitorStatus
		expectMessage  string
	}{
		{
			name: "successful connection without TLS",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServer(t, false, false, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
		},
		{
			name: "successful connection with TLS (STARTTLS)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServer(t, true, false, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
		},
		{
			name: "successful connection with direct TLS (SMTPS)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelayAndDirectTLS(t, true, true, false, false, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": true,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
		},
		{
			name: "successful connection with TLS and auth (STARTTLS)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServer(t, true, true, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
		},
		{
			name: "successful connection with direct TLS and auth",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelayAndDirectTLS(t, true, true, true, false, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
		},
		{
			name: "successful connection with mail from test",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServer(t, false, false, true)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, cleanup := tt.setupServer()
			defer cleanup()

			port := listener.Addr().(*net.TCPAddr).Port
			config := fmt.Sprintf(tt.config, port)

			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "smtp",
				Name:     "Test SMTP Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   config,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage)
			}
		})
	}
}

func TestSMTPExecutor_Execute_FailureCases(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name           string
		config         string
		expectedStatus shared.MonitorStatus
		expectMessage  string
	}{
		{
			name: "connection failure - invalid host",
			config: `{
				"host": "127.0.0.1",
				"port": 59999,
				"use_tls": false
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "connection failed",
		},
		{
			name: "invalid config",
			config: `{
				invalid json
			}`,
			expectedStatus: shared.MonitorStatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "smtp",
				Name:     "Test SMTP Monitor",
				Interval: 30,
				Timeout:  1,
				Config:   tt.config,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
			if tt.expectMessage != "" {
				assert.Contains(t, strings.ToLower(result.Message), strings.ToLower(tt.expectMessage))
			}
		})
	}
}

func TestSMTPExecutor_Execute_TLSCertificateInfo(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	listener, cleanup := setupMockSMTPServer(t, true, false, false)
	defer cleanup()

	port := listener.Addr().(*net.TCPAddr).Port
	config := fmt.Sprintf(`{
		"host": "127.0.0.1",
		"port": %d,
		"use_tls": true,
		"ignore_tls_errors": true,
		"check_cert_expiry": true
	}`, port)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "smtp",
		Name:     "Test SMTP Monitor",
		Interval: 30,
		Timeout:  5,
		Config:   config,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, nil)
	assert.Equal(t, shared.MonitorStatusUp, result.Status)

	// Check that TLS info is captured
	if result.TLSInfo != nil && result.TLSInfo.CertInfo != nil {
		assert.NotEmpty(t, result.TLSInfo.CertInfo.Subject)
		assert.NotEmpty(t, result.TLSInfo.CertInfo.Issuer)
	}
}

// TestSMTPExecutor_Execute_IPv6Host checks that an IPv6 SMTP host works typed bare or in brackets
func TestSMTPExecutor_Execute_IPv6Host(t *testing.T) {
	requireIPv6Loopback(t)
	listener, cleanup := startMockSMTPServerOn(t, "[::1]:0", func(conn net.Conn) {
		handleSMTPConnection(conn, nil, false, false, false)
	})
	defer cleanup()
	port := listener.Addr().(*net.TCPAddr).Port

	for _, host := range []string{"::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			m := &Monitor{Name: "smtp over IPv6", Timeout: 5, Config: fmt.Sprintf(`{"host": %q, "port": %d}`, host, port)}

			result := NewSMTPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), m, nil)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})
	}
}

// TestSMTPExecutor_Execute_IPv6HostWithAuth checks that the check logs in to an IPv6 SMTP host
// typed in brackets: PlainAuth sends the password in the clear only to a host it knows as local
func TestSMTPExecutor_Execute_IPv6HostWithAuth(t *testing.T) {
	requireIPv6Loopback(t)
	listener, cleanup := startMockSMTPServerOn(t, "[::1]:0", func(conn net.Conn) {
		handleSMTPConnection(conn, nil, true, false, false)
	})
	defer cleanup()
	port := listener.Addr().(*net.TCPAddr).Port

	for _, host := range []string{"::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			m := &Monitor{Name: "smtp over IPv6", Timeout: 5,
				Config: fmt.Sprintf(`{"host": %q, "port": %d, "username": "testuser", "password": "testpass"}`, host, port)}

			result := NewSMTPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), m, nil)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			assert.Contains(t, result.Message, "Auth")
		})
	}
}

func TestNewSMTPExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewSMTPExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.logger)
}

// setupMockSMTPServer creates a mock SMTP server for testing
func setupMockSMTPServer(t *testing.T, useTLS bool, requireAuth bool, testMailFrom bool) (net.Listener, func()) {
	return setupMockSMTPServerWithRelay(t, useTLS, requireAuth, testMailFrom, false)
}

// setupMockSMTPServerWithRelay creates a mock SMTP server with configurable open relay behavior
func setupMockSMTPServerWithRelay(t *testing.T, useTLS bool, requireAuth bool, testMailFrom bool, allowOpenRelay bool) (net.Listener, func()) {
	return setupMockSMTPServerWithRelayAndDirectTLS(t, useTLS, false, requireAuth, testMailFrom, allowOpenRelay)
}

// setupMockSMTPServerWithRelayAndDirectTLS creates a mock SMTP server with configurable TLS mode
// useDirectTLS: if true, establishes TLS immediately (for port 465/SMTPS)
//
//	if false, uses STARTTLS (for ports 587/25)
func setupMockSMTPServerWithRelayAndDirectTLS(t *testing.T, useTLS bool, useDirectTLS bool, requireAuth bool, testMailFrom bool, allowOpenRelay bool) (net.Listener, func()) {
	// Generate self-signed certificate for TLS
	var tlsConfig *tls.Config
	if useTLS {
		cert, err := generateSelfSignedCert()
		if err != nil {
			t.Fatalf("Failed to generate certificate: %v", err)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	return startMockSMTPServer(t, func(conn net.Conn) {
		if useDirectTLS && tlsConfig != nil {
			// For direct TLS (SMTPS), upgrade connection immediately
			handleSMTPConnectionDirectTLS(conn, tlsConfig, requireAuth, testMailFrom, allowOpenRelay)
		} else {
			// For STARTTLS, send greeting first
			handleSMTPConnection(conn, tlsConfig, requireAuth, testMailFrom, allowOpenRelay)
		}
	})
}

// startMockSMTPServer listens on a local port and serves each connection with handle
func startMockSMTPServer(t *testing.T, handle func(net.Conn)) (net.Listener, func()) {
	return startMockSMTPServerOn(t, "127.0.0.1:0", handle)
}

// startMockSMTPServerOn listens on addr and serves each connection with handle
func startMockSMTPServerOn(t *testing.T, addr string, handle func(net.Conn)) (net.Listener, func()) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go handle(conn)
		}
	}()

	cleanup := func() {
		listener.Close()
		<-done
	}

	return listener, cleanup
}

// mockAuthPlain answers an AUTH PLAIN command, with the credentials either on the
// command line (initial response) or after a 334 prompt. Only testuser/testpass,
// encoded as RFC 4616 requires, is accepted.
func mockAuthPlain(line string, reader *bufio.Reader, writer *bufio.Writer) (bool, error) {
	var encoded string
	if fields := strings.Fields(line); len(fields) >= 3 {
		encoded = fields[2]
	} else {
		writer.WriteString("334 \r\n")
		writer.Flush()
		response, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		encoded = strings.TrimSpace(response)
	}

	creds, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(creds) != "\x00testuser\x00testpass" {
		writer.WriteString("535 Authentication failed\r\n")
		writer.Flush()
		return false, nil
	}
	writer.WriteString("235 Authentication successful\r\n")
	writer.Flush()
	return true, nil
}

// handleSMTPConnectionDirectTLS handles a direct TLS connection (SMTPS)
// TLS is established immediately, then greeting is sent over TLS
func handleSMTPConnectionDirectTLS(conn net.Conn, tlsConfig *tls.Config, requireAuth bool, testMailFrom bool, allowOpenRelay bool) {
	defer conn.Close()

	// Upgrade to TLS immediately (before sending greeting)
	tlsConn := tls.Server(conn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return
	}

	reader := bufio.NewReader(tlsConn)
	writer := bufio.NewWriter(tlsConn)

	// Send greeting over TLS connection
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	authenticated := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-test.example.com\r\n")
			// STARTTLS is not advertised for direct TLS connections
			if requireAuth {
				writer.WriteString("250-AUTH PLAIN\r\n")
			}
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "AUTH":
			if !requireAuth {
				writer.WriteString("503 Authentication not required\r\n")
				writer.Flush()
				continue
			}
			ok, err := mockAuthPlain(line, reader, writer)
			if err != nil {
				return
			}
			authenticated = ok

		case "MAIL":
			// MAIL FROM is allowed before authentication (needed for open relay test)
			// If authentication is required and user is authenticated, allow it
			// If authentication is not required, allow it
			// Only reject if authentication is required AND user is not authenticated AND this is not for open relay testing
			// For open relay testing, we need to allow MAIL FROM before authentication
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "RCPT":
			// RCPT TO logic for open relay testing:
			// - If allowOpenRelay is true, allow RCPT TO even without authentication (open relay)
			// - If allowOpenRelay is false, reject RCPT TO to external domains if not authenticated (secure server)
			// - If authenticated, always allow (authenticated users can relay)
			if allowOpenRelay {
				// Open relay: allow relaying even without authentication
				writer.WriteString("250 OK\r\n")
			} else if authenticated {
				// Authenticated users can always relay
				writer.WriteString("250 OK\r\n")
			} else {
				// Secure server: reject relay attempts to external domains without authentication
				writer.WriteString("550 Relaying denied\r\n")
			}
			writer.Flush()

		case "RSET":
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return

		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// handleSMTPConnection handles a single SMTP connection with STARTTLS
// For STARTTLS, greeting is sent first, then client sends STARTTLS command
func handleSMTPConnection(conn net.Conn, tlsConfig *tls.Config, requireAuth bool, testMailFrom bool, allowOpenRelay bool) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting (for STARTTLS, greeting is sent before TLS)
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	authenticated := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-test.example.com\r\n")
			if tlsConfig != nil {
				writer.WriteString("250-STARTTLS\r\n")
			}
			if requireAuth {
				writer.WriteString("250-AUTH PLAIN\r\n")
			}
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "STARTTLS":
			if tlsConfig == nil {
				writer.WriteString("502 Command not implemented\r\n")
				writer.Flush()
				continue
			}
			writer.WriteString("220 Ready to start TLS\r\n")
			writer.Flush()

			// Upgrade to TLS
			tlsConn := tls.Server(conn, tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)

		case "AUTH":
			if !requireAuth {
				writer.WriteString("503 Authentication not required\r\n")
				writer.Flush()
				continue
			}
			ok, err := mockAuthPlain(line, reader, writer)
			if err != nil {
				return
			}
			authenticated = ok

		case "MAIL":
			// MAIL FROM is allowed before authentication (needed for open relay test)
			// If authentication is required and user is authenticated, allow it
			// If authentication is not required, allow it
			// For open relay testing, we need to allow MAIL FROM before authentication
			if testMailFrom {
				// Validate MAIL FROM format
				if !strings.Contains(line, "FROM:") {
					writer.WriteString("501 Syntax error\r\n")
					writer.Flush()
					continue
				}
			}
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "RCPT":
			// RCPT TO logic for open relay testing:
			// - If allowOpenRelay is true, allow RCPT TO even without authentication (open relay)
			// - If allowOpenRelay is false, reject RCPT TO to external domains if not authenticated (secure server)
			// - If authenticated, always allow (authenticated users can relay)
			if allowOpenRelay {
				// Open relay: allow relaying even without authentication
				writer.WriteString("250 OK\r\n")
			} else if authenticated {
				// Authenticated users can always relay
				writer.WriteString("250 OK\r\n")
			} else {
				// Secure server: reject relay attempts to external domains without authentication
				writer.WriteString("550 Relaying denied\r\n")
			}
			writer.Flush()

		case "RSET":
			writer.WriteString("250 OK\r\n")
			writer.Flush()

		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return

		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// generateSelfSignedCert generates a self-signed certificate for testing
func generateSelfSignedCert() (tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test SMTP Server"},
			CommonName:   "127.0.0.1",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"127.0.0.1", "127.0.0.1"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// TestSMTPExecutor_Execute_ErrorCases tests various error scenarios to improve coverage
func TestSMTPExecutor_Execute_ErrorCases(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name           string
		setupServer    func() (net.Listener, func())
		config         string
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "HELO fallback when EHLO fails (legacy SMTP server)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerLegacy(t, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests that HELO fallback works when EHLO fails (legacy SMTP)",
		},
		{
			name: "STARTTLS handshake failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerTLSFailure(t, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "STARTTLS",
			description:    "Tests STARTTLS handshake failure",
		},
		{
			name: "Direct TLS handshake failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerTLSFailure(t, true)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": true,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "TLS",
			description:    "Tests direct TLS handshake failure",
		},
		{
			name: "Authentication failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerAuthFailure(t)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"username": "testuser",
				"password": "wrongpass"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "authentication",
			description:    "Tests authentication failure",
		},
		{
			name: "MAIL FROM failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerMailFromFailure(t)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "MAIL FROM",
			description:    "Tests MAIL FROM command failure",
		},
		{
			name: "EHLO failure when auth required",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerEHLOFailure(t)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"username": "testuser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "hello",
			description:    "Tests EHLO failure when authentication is required (must fail, no HELO fallback)",
		},
		{
			name: "Direct TLS greeting read failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerDirectTLSGreetingFailure(t)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": true,
				"use_direct_tls": true,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "client",
			description:    "Tests failure to read greeting after direct TLS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, cleanup := tt.setupServer()
			defer cleanup()

			port := listener.Addr().(*net.TCPAddr).Port
			config := fmt.Sprintf(tt.config, port)

			monitor := &Monitor{
				ID:       "monitor-error",
				Type:     "smtp",
				Name:     "Test SMTP Monitor Error",
				Interval: 30,
				Timeout:  5,
				Config:   config,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, strings.ToLower(result.Message), strings.ToLower(tt.expectMessage), tt.description)
			}
		})
	}
}

// setupMockSMTPServerLegacy creates a legacy SMTP server that only supports HELO (not EHLO)
func setupMockSMTPServerLegacy(t *testing.T, useTLS bool) (net.Listener, func()) {
	return startMockSMTPServer(t, handleSMTPConnectionLegacy)
}

// handleSMTPConnectionLegacy handles a legacy SMTP connection (only HELO, no EHLO)
func handleSMTPConnectionLegacy(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting
	writer.WriteString("220 test.example.com SMTP\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO":
			// Legacy server doesn't support EHLO - return error
			writer.WriteString("500 Command not recognized\r\n")
			writer.Flush()
		case "HELO":
			// Legacy server supports HELO
			writer.WriteString("250 test.example.com\r\n")
			writer.Flush()
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// setupMockSMTPServerTLSFailure creates a server that fails TLS handshake
func setupMockSMTPServerTLSFailure(t *testing.T, useDirectTLS bool) (net.Listener, func()) {
	return startMockSMTPServer(t, func(conn net.Conn) {
		if useDirectTLS {
			// For direct TLS, close connection immediately to simulate handshake failure
			conn.Close()
			return
		}
		handleSMTPConnectionTLSFailure(conn)
	})
}

// handleSMTPConnectionTLSFailure handles a connection that fails during STARTTLS
func handleSMTPConnectionTLSFailure(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-test.example.com\r\n")
			writer.WriteString("250-STARTTLS\r\n")
			writer.WriteString("250 OK\r\n")
			writer.Flush()
		case "STARTTLS":
			writer.WriteString("220 Ready to start TLS\r\n")
			writer.Flush()
			// Close connection to simulate handshake failure
			conn.Close()
			return
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// setupMockSMTPServerAuthFailure creates a server that rejects authentication
func setupMockSMTPServerAuthFailure(t *testing.T) (net.Listener, func()) {
	return startMockSMTPServer(t, handleSMTPConnectionAuthFailure)
}

// handleSMTPConnectionAuthFailure handles a connection that rejects authentication
func handleSMTPConnectionAuthFailure(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-test.example.com\r\n")
			writer.WriteString("250-AUTH PLAIN\r\n")
			writer.WriteString("250 OK\r\n")
			writer.Flush()
		case "AUTH":
			// The test sends a wrong password, so this always rejects
			if _, err := mockAuthPlain(line, reader, writer); err != nil {
				return
			}
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// setupMockSMTPServerMailFromFailure creates a server that rejects MAIL FROM
func setupMockSMTPServerMailFromFailure(t *testing.T) (net.Listener, func()) {
	return startMockSMTPServer(t, handleSMTPConnectionMailFromFailure)
}

// handleSMTPConnectionMailFromFailure handles a connection that rejects MAIL FROM
func handleSMTPConnectionMailFromFailure(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-test.example.com\r\n")
			writer.WriteString("250 OK\r\n")
			writer.Flush()
		case "MAIL":
			// Reject MAIL FROM
			writer.WriteString("550 Mail from address rejected\r\n")
			writer.Flush()
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// setupMockSMTPServerEHLOFailure creates a server that fails EHLO
func setupMockSMTPServerEHLOFailure(t *testing.T) (net.Listener, func()) {
	return startMockSMTPServer(t, handleSMTPConnectionEHLOFailure)
}

// handleSMTPConnectionEHLOFailure handles a connection that fails EHLO
func handleSMTPConnectionEHLOFailure(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Send greeting
	writer.WriteString("220 test.example.com ESMTP\r\n")
	writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		cmd := strings.ToUpper(strings.Split(line, " ")[0])

		switch cmd {
		case "EHLO":
			// Fail EHLO
			writer.WriteString("500 Command not recognized\r\n")
			writer.Flush()
		case "HELO":
			// Also fail HELO to ensure we test the error path
			writer.WriteString("500 Command not recognized\r\n")
			writer.Flush()
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Command not implemented\r\n")
			writer.Flush()
		}
	}
}

// setupMockSMTPServerDirectTLSGreetingFailure creates a server that fails to send greeting after direct TLS
func setupMockSMTPServerDirectTLSGreetingFailure(t *testing.T) (net.Listener, func()) {
	// Generate self-signed certificate
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	return startMockSMTPServer(t, func(conn net.Conn) {
		handleSMTPConnectionDirectTLSGreetingFailure(conn, tlsConfig)
	})
}

// handleSMTPConnectionDirectTLSGreetingFailure handles direct TLS but fails to send greeting
func handleSMTPConnectionDirectTLSGreetingFailure(conn net.Conn, tlsConfig *tls.Config) {
	defer conn.Close()

	// Upgrade to TLS immediately
	tlsConn := tls.Server(conn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return
	}

	// Close connection without sending greeting to simulate failure
	tlsConn.Close()
}

// TestSMTPExecutor_Execute_Postfix_Integration tests comprehensive scenarios against the Postfix test container.
// These tests require the Postfix container to be running:
//
//	docker compose -f test_utils/smtp/docker-compose.smtp-test.yml up -d postfix
//
// Run with: POSTFIX_TESTS=1 go test -v ./internal/modules/healthcheck/executor -run TestSMTPExecutor_Execute_Postfix_Integration
//
// Postfix test server configuration:
//   - Port 1027: Plain SMTP (container port 25)
//   - Port 1028: STARTTLS/Submission (container port 587)
//   - Port 1029: Direct TLS/SMTPS (container port 465)
//   - Authentication: testuser / testpass (realm: test-smtp.local)
//   - Self-signed certificates (CN: test-smtp.local)
func TestSMTPExecutor_Execute_Postfix_Integration(t *testing.T) {
	if os.Getenv("POSTFIX_TESTS") != "1" {
		t.Skip("Skipping Postfix integration tests. Run with POSTFIX_TESTS=1 go test to enable.")
	}

	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name           string
		config         string
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
		skipIfDown     bool // If true, skip test if server is down (don't fail)
		timeout        int  // Custom timeout in seconds (default: 5)
	}{
		// ========== Plain SMTP (Port 1027) ==========
		{
			name: "Plain SMTP - basic connection",
			config: `{
				"host": "127.0.0.1",
				"port": 1027,
				"use_tls": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests basic plain SMTP connection to Postfix on port 25",
		},
		{
			name: "Plain SMTP - with MAIL FROM",
			config: `{
				"host": "127.0.0.1",
				"port": 1027,
				"use_tls": false,
				"from_email": "monitor@example.com"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests MAIL FROM command on plain SMTP",
		},
		{
			name: "Plain SMTP - open relay test (reject expected)",
			config: `{
				"host": "127.0.0.1",
				"port": 1027,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests open relay detection on plain SMTP (port 25 may allow relay in test environment)",
		},
		{
			name: "Plain SMTP - open relay test with custom RCPT TO",
			config: `{
				"host": "127.0.0.1",
				"port": 1027,
				"use_tls": false,
				"from_email": "test@example.com",
				"rcpt_to_email": "custom@external-domain.com",
				"test_open_relay": true,
				"expect_secure_relay": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests open relay with custom RCPT TO email (port 25 may allow relay)",
		},

		// ========== STARTTLS (Port 1028) ==========
		{
			name: "STARTTLS - without authentication",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests STARTTLS connection without authentication",
		},
		{
			name: "STARTTLS - with authentication",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
			description:    "Tests STARTTLS with correct authentication credentials",
		},
		{
			name: "STARTTLS - with wrong password",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "wrongpass"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "authentication",
			description:    "Tests STARTTLS with incorrect password (should fail)",
			skipIfDown:     true,
		},
		{
			name: "STARTTLS - with wrong username",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "wronguser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "authentication",
			description:    "Tests STARTTLS with incorrect username (should fail)",
			skipIfDown:     true,
		},
		{
			name: "STARTTLS - with certificate expiry check",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"check_cert_expiry": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests STARTTLS with certificate expiry checking enabled",
		},
		{
			name: "STARTTLS - with MAIL FROM",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass",
				"from_email": "monitor@example.com"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
			description:    "Tests STARTTLS with authentication and MAIL FROM",
		},
		{
			name: "STARTTLS - open relay test (reject expected)",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests that Postfix correctly rejects open relay on STARTTLS port",
		},
		{
			name: "STARTTLS - with auth should allow relay",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass",
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests that Postfix allows relay for authenticated users on STARTTLS",
		},
		{
			name: "STARTTLS - with read timeout",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true,
				"read_timeout": 10
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests STARTTLS with custom read timeout",
		},

		// ========== Direct TLS/SMTPS (Port 1029) ==========
		{
			name: "Direct TLS - without authentication",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests direct TLS (SMTPS) connection without authentication",
		},
		{
			name: "Direct TLS - with authentication",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
			description:    "Tests direct TLS (SMTPS) with correct authentication credentials",
		},
		{
			name: "Direct TLS - with wrong password",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "wrongpass"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "authentication",
			description:    "Tests direct TLS (SMTPS) with incorrect password (should fail)",
			skipIfDown:     true,
		},
		{
			name: "Direct TLS - with certificate expiry check",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"check_cert_expiry": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests direct TLS (SMTPS) with certificate expiry checking enabled",
		},
		{
			name: "Direct TLS - with MAIL FROM",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass",
				"from_email": "monitor@example.com"
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS + Auth)",
			description:    "Tests direct TLS (SMTPS) with authentication and MAIL FROM",
		},
		{
			name: "Direct TLS - open relay test (reject expected)",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests that Postfix correctly rejects open relay on SMTPS port",
		},
		{
			name: "Direct TLS - with auth should allow relay",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"username": "testuser",
				"password": "testpass",
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
			description:    "Tests that Postfix allows relay for authenticated users on SMTPS",
		},
		{
			name: "Direct TLS - with read timeout",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true,
				"read_timeout": 10
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable (TLS)",
			description:    "Tests direct TLS (SMTPS) with custom read timeout",
		},

		// ========== Error Cases ==========
		{
			name: "Error - wrong port/TLS combination (port 1028 with direct TLS)",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "TLS",
			description:    "Tests that using direct TLS on STARTTLS port fails",
			skipIfDown:     true,
		},
		{
			name: "Error - wrong port/TLS combination (port 1029 with STARTTLS)",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "", // Accept any error - timeout is expected when using STARTTLS on direct TLS port
			description:    "Tests that using STARTTLS on direct TLS port fails (timeout expected)",
			skipIfDown:     true,
			timeout:        2, // Short timeout since this will hang
		},
		{
			name: "Error - STARTTLS without ignore_tls_errors (self-signed cert)",
			config: `{
				"host": "127.0.0.1",
				"port": 1028,
				"use_tls": true,
				"use_direct_tls": false,
				"ignore_tls_errors": false
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "certificate",
			description:    "Tests that STARTTLS fails without ignore_tls_errors (self-signed cert)",
			skipIfDown:     true,
		},
		{
			name: "Error - Direct TLS without ignore_tls_errors (self-signed cert)",
			config: `{
				"host": "127.0.0.1",
				"port": 1029,
				"use_tls": false,
				"use_direct_tls": true,
				"ignore_tls_errors": false
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "certificate",
			description:    "Tests that direct TLS fails without ignore_tls_errors (self-signed cert)",
			skipIfDown:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use custom timeout if specified, otherwise default to 5
			timeout := tt.timeout
			if timeout == 0 {
				timeout = 5
			}

			monitor := &Monitor{
				ID:       "monitor-postfix-integration",
				Type:     "smtp",
				Name:     "Postfix Integration Test",
				Interval: 30,
				Timeout:  timeout,
				Config:   tt.config,
			}

			// Context timeout should be longer than monitor timeout
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+5)*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			assert.NotNil(t, result)

			// If containers are not running, handle gracefully
			if result.Status == shared.MonitorStatusDown && strings.Contains(strings.ToLower(result.Message), "connection") {
				if tt.skipIfDown {
					t.Skipf("Postfix container not available - skipping test: %s", result.Message)
					return
				}
				// For tests that expect failure, check if it's the expected failure type
				if tt.expectedStatus == shared.MonitorStatusDown {
					// Check if the error message matches what we expect
					if tt.expectMessage != "" {
						assert.Contains(t, strings.ToLower(result.Message), strings.ToLower(tt.expectMessage), tt.description)
					}
					return
				}
				// If test expects success but container is down, fail the test
				t.Fatalf("Postfix container not available - test requires running container: %s", result.Message)
			}

			// Verify the result matches expectations
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}

			// For successful TLS connections, verify TLS info is captured
			if result.Status == shared.MonitorStatusUp && strings.Contains(tt.config, `"use_tls": true`) {
				if strings.Contains(tt.config, `"check_cert_expiry": true`) {
					// Certificate info should be available when checking expiry
					if result.TLSInfo != nil && result.TLSInfo.CertInfo != nil {
						assert.NotEmpty(t, result.TLSInfo.CertInfo.Subject, "TLS certificate subject should be captured")
						assert.NotEmpty(t, result.TLSInfo.CertInfo.Issuer, "TLS certificate issuer should be captured")
					}
				}
			}
		})
	}
}

func TestSMTPExecutor_Execute_OpenRelay(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	tests := []struct {
		name           string
		setupServer    func() (net.Listener, func())
		config         string
		expectedStatus shared.MonitorStatus
		expectMessage  string
	}{
		{
			name: "open relay detected - treated as failure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelay(t, false, false, true, true)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "open relay",
		},
		{
			name: "open relay detected - treated as success",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelay(t, false, false, true, true)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
		},
		{
			name: "relay rejected - treated as success (expect_secure_relay=true)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelay(t, false, false, true, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
		},
		{
			name: "relay rejected - treated as failure (expect_secure_relay=false)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelay(t, false, false, true, false)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": true,
				"expect_secure_relay": false
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "expected open relay",
		},
		{
			name: "MAIL FROM rejected - treated as secure (expect_secure_relay=true)",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerMailFromFailure(t)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "open relay rejected (secure)",
		},
		{
			name: "connection dropped at RCPT TO - not treated as secure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerRcptReply(t, "")
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "open relay test failed",
		},
		{
			name: "temporary RCPT TO failure - not treated as secure",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerRcptReply(t, "451 Greylisted, try again later")
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"test_open_relay": true,
				"expect_secure_relay": true
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "open relay test failed",
		},
		{
			name: "open relay test disabled",
			setupServer: func() (net.Listener, func()) {
				return setupMockSMTPServerWithRelay(t, false, false, true, true)
			},
			config: `{
				"host": "127.0.0.1",
				"port": %d,
				"use_tls": false,
				"from_email": "test@example.com",
				"test_open_relay": false
			}`,
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SMTP server is reachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, cleanup := tt.setupServer()
			defer cleanup()

			port := listener.Addr().(*net.TCPAddr).Port
			config := fmt.Sprintf(tt.config, port)

			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "smtp",
				Name:     "Test SMTP Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   config,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
			if tt.expectMessage != "" {
				assert.Contains(t, strings.ToLower(result.Message), strings.ToLower(tt.expectMessage))
			}
		})
	}
}

// TestSMTPExecutor_Execute_ContextCancellation tests graceful handling of context cancellation
func TestSMTPExecutor_Execute_ContextCancellation(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	monitor := &Monitor{
		ID:      "test-monitor",
		Name:    "Test SMTP Monitor",
		Type:    "smtp",
		Timeout: 5,
		Config: `{
			"host": "smtp.example.com",
			"port": 587,
			"use_tls": false
		}`,
	}

	result := executor.Execute(ctx, monitor, nil)

	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "cancelled")
}

// setupMockSMTPServerRcptReply creates a server that answers RCPT TO with reply,
// or drops the connection when reply is empty
func setupMockSMTPServerRcptReply(t *testing.T, reply string) (net.Listener, func()) {
	return startMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		writer.WriteString("220 test.example.com ESMTP\r\n")
		writer.Flush()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			cmd := strings.ToUpper(strings.Split(strings.TrimSpace(line), " ")[0])
			switch cmd {
			case "EHLO", "HELO", "MAIL", "RSET":
				writer.WriteString("250 OK\r\n")
			case "RCPT":
				if reply == "" {
					return
				}
				writer.WriteString(reply + "\r\n")
			case "QUIT":
				writer.WriteString("221 Bye\r\n")
				writer.Flush()
				return
			default:
				writer.WriteString("502 Command not implemented\r\n")
			}
			writer.Flush()
		}
	})
}

// TestSMTPExecutor_Execute_StopsAtCheckDeadline tests that a large read_timeout
// cannot keep a check running past its context deadline
func TestSMTPExecutor_Execute_StopsAtCheckDeadline(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	// Server sends the greeting, then never answers
	listener, cleanup := startMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 test.example.com ESMTP\r\n"))
		io.Copy(io.Discard, conn)
	})
	defer cleanup()

	port := listener.Addr().(*net.TCPAddr).Port
	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "smtp",
		Name:     "Test SMTP Monitor",
		Interval: 30,
		Timeout:  1,
		Config:   fmt.Sprintf(`{"host": "127.0.0.1", "port": %d, "use_tls": false, "read_timeout": 300}`, port),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	result := executor.Execute(ctx, monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "HELLO")
	assert.Less(t, time.Since(start), 3*time.Second)
}

// TestSMTPExecutor_Execute_SameServerBackToBack tests that checks of the same server
// in quick succession (e.g. two monitors on one host) all run
func TestSMTPExecutor_Execute_SameServerBackToBack(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	listener, cleanup := setupMockSMTPServer(t, false, false, false)
	defer cleanup()

	port := listener.Addr().(*net.TCPAddr).Port
	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "smtp",
		Name:     "Test SMTP Monitor",
		Interval: 30,
		Timeout:  5,
		Config:   fmt.Sprintf(`{"host": "127.0.0.1", "port": %d, "use_tls": false}`, port),
	}

	for i := 1; i <= 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result := executor.Execute(ctx, monitor, nil)
		cancel()
		assert.Equal(t, shared.MonitorStatusUp, result.Status, "check %d: %s", i, result.Message)
	}
}

// TestSMTPExecutor_Execute_UsernameWithoutPassword tests that the result does not
// claim authentication when no AUTH was sent
func TestSMTPExecutor_Execute_UsernameWithoutPassword(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	listener, cleanup := setupMockSMTPServer(t, false, true, false)
	defer cleanup()

	port := listener.Addr().(*net.TCPAddr).Port
	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "smtp",
		Name:     "Test SMTP Monitor",
		Interval: 30,
		Timeout:  5,
		Config:   fmt.Sprintf(`{"host": "127.0.0.1", "port": %d, "use_tls": false, "username": "testuser"}`, port),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, nil)
	assert.Equal(t, shared.MonitorStatusUp, result.Status)
	assert.Equal(t, "SMTP server is reachable", result.Message)
}

// TestSMTPExecutor_Execute_EHLOName tests that the client introduces itself with its
// own name, not the target server's hostname
func TestSMTPExecutor_Execute_EHLOName(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSMTPExecutor(logger)

	ehlo := make(chan string, 1)
	listener, cleanup := startMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()

		reader := bufio.NewReader(conn)
		conn.Write([]byte("220 test.example.com ESMTP\r\n"))

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "QUIT") {
				conn.Write([]byte("221 Bye\r\n"))
				return
			}
			if strings.HasPrefix(line, "EHLO") {
				ehlo <- line
			}
			conn.Write([]byte("250 OK\r\n"))
		}
	})
	defer cleanup()

	port := listener.Addr().(*net.TCPAddr).Port
	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "smtp",
		Name:     "Test SMTP Monitor",
		Interval: 30,
		Timeout:  5,
		Config:   fmt.Sprintf(`{"host": "127.0.0.1", "port": %d, "use_tls": false}`, port),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, nil)
	assert.Equal(t, shared.MonitorStatusUp, result.Status)

	select {
	case line := <-ehlo:
		assert.Equal(t, "EHLO localhost", line)
	default:
		t.Fatal("server received no EHLO")
	}
}
