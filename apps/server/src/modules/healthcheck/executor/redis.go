package executor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"peekaping/src/modules/shared"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type RedisConfig struct {
	DatabaseConnectionString string `json:"databaseConnectionString" validate:"required" example:"redis://user:password@host:port"`
	IgnoreTls                bool   `json:"ignoreTls" example:"false"`
	CaCert                   string `json:"caCert,omitempty" example:"-----BEGIN CERTIFICATE-----\n..."`
	ClientCert               string `json:"clientCert,omitempty" example:"-----BEGIN CERTIFICATE-----\n..."`
	ClientKey                string `json:"clientKey,omitempty" example:"-----BEGIN PRIVATE KEY-----\n..."`
}

type RedisExecutor struct {
	logger *zap.SugaredLogger
}

// Redis connection string validation regex (same as client-side)
// Updated to support IPv6 addresses in brackets [::1] or without brackets ::1
var redisConnectionStringRegex = regexp.MustCompile(`^(rediss?://)([^@]*@)?(\[[^\]]+\]|[^:/]+)(:\d{1,5})?(/[0-9]*)?$`)

// validateRedisConnectionString performs comprehensive validation of Redis connection strings
func (r *RedisExecutor) validateRedisConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string is required")
	}

	// Basic format check with regex
	if !redisConnectionStringRegex.MatchString(connectionString) {
		return fmt.Errorf("invalid connection string format. Expected: redis://[user:password@]host[:port][/db] or rediss://[user:password@]host[:port][/db]")
	}

	// Parse URL to validate components
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Validate protocol
	if !isValidRedisProtocol(parsedURL.Scheme) {
		return fmt.Errorf("invalid protocol '%s'. Use 'redis://' for non-TLS or 'rediss://' for TLS", parsedURL.Scheme)
	}

	// Validate hostname
	if err := r.validateHostname(parsedURL.Hostname()); err != nil {
		return fmt.Errorf("invalid hostname: %w", err)
	}

	// Validate port
	if err := r.validatePort(parsedURL.Port()); err != nil {
		return fmt.Errorf("invalid port: %w", err)
	}

	// Validate database number
	if err := r.validateDatabaseNumber(parsedURL.Path); err != nil {
		return fmt.Errorf("invalid database number: %w", err)
	}

	// Validate authentication (if present)
	if err := r.validateAuthentication(parsedURL.User); err != nil {
		return fmt.Errorf("invalid authentication: %w", err)
	}

	// Additional validation for authentication format in the original string
	if err := r.validateAuthFormat(connectionString); err != nil {
		return fmt.Errorf("invalid authentication format: %w", err)
	}

	return nil
}

// isValidRedisProtocol checks if the protocol is valid for Redis
func isValidRedisProtocol(protocol string) bool {
	return protocol == "redis" || protocol == "rediss"
}

// validateHostname validates the Redis hostname
func (r *RedisExecutor) validateHostname(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname is required")
	}

	// Check for valid hostname characters
	if len(hostname) > 253 {
		return fmt.Errorf("hostname too long (max 253 characters)")
	}

	// Check for valid hostname format
	if !isValidHostname(hostname) {
		return fmt.Errorf("hostname contains invalid characters")
	}

	return nil
}

// isValidHostname checks if a hostname is valid
func isValidHostname(hostname string) bool {
	// Basic hostname validation
	if hostname == "" {
		return false
	}

	// Special handling for IPv6 addresses
	if strings.Contains(hostname, ":") {
		// Check if it's a valid IPv6 address
		if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
			// IPv6 in brackets format
			ipv6 := strings.Trim(hostname, "[]")
			return isValidIPv6(ipv6)
		}
		// Check if it's a valid IPv6 address without brackets
		return isValidIPv6(hostname)
	}

	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if label == "" {
			return false
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') || char == '-') {
				return false
			}
		}
	}

	return true
}

// isValidIPv6 checks if a string is a valid IPv6 address
func isValidIPv6(ip string) bool {
	// Use Go's standard library to parse and validate IPv6 addresses
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}

// validatePort validates the Redis port
func (r *RedisExecutor) validatePort(port string) error {
	if port == "" {
		return nil // Port is optional, defaults to 6379
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("port must be a number")
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", portNum)
	}

	return nil
}

// validateDatabaseNumber validates the Redis database number
func (r *RedisExecutor) validateDatabaseNumber(path string) error {
	if path == "" || path == "/" {
		return nil // Database number is optional
	}

	// Remove leading slash
	dbStr := strings.TrimPrefix(path, "/")
	if dbStr == "" {
		return nil // Empty database number is valid (defaults to 0)
	}

	dbNum, err := strconv.Atoi(dbStr)
	if err != nil {
		return fmt.Errorf("database number must be a number")
	}

	if dbNum < 0 {
		return fmt.Errorf("database number must be 0 or positive, got %d", dbNum)
	}

	// Redis typically supports 0-15 databases by default
	if dbNum > 15 {
		r.logger.Warnf("database number %d is outside typical Redis range (0-15)", dbNum)
	}

	return nil
}

// validateAuthentication validates Redis authentication credentials
func (r *RedisExecutor) validateAuthentication(user *url.Userinfo) error {
	if user == nil {
		return nil // Authentication is optional
	}

	username := user.Username()
	password, hasPassword := user.Password()

	// Validate username if present
	if username != "" {
		if len(username) > 255 {
			return fmt.Errorf("username too long (max 255 characters)")
		}
		if strings.Contains(username, ":") {
			return fmt.Errorf("username cannot contain ':' character")
		}
	}

	// Validate password if present
	if hasPassword {
		if len(password) > 255 {
			return fmt.Errorf("password too long (max 255 characters)")
		}
		if strings.Contains(password, "@") {
			return fmt.Errorf("password cannot contain '@' character")
		}
	}

	return nil
}

// validateAuthFormat validates the authentication format in the connection string
func (r *RedisExecutor) validateAuthFormat(connectionString string) error {
	// Check for malformed authentication with multiple colons
	if strings.Contains(connectionString, "@") {
		// Extract the auth part before @
		atIndex := strings.Index(connectionString, "@")
		if atIndex > 0 {
			authPart := connectionString[strings.Index(connectionString, "://")+3 : atIndex]
			if strings.Count(authPart, ":") > 1 {
				return fmt.Errorf("invalid authentication format: username cannot contain ':' character")
			}
		}
	}
	return nil
}

// loadCACertPool loads CA certificate from PEM string
func loadCACertPool(caCertPEM string) (*x509.CertPool, error) {
	if caCertPEM == "" {
		return nil, nil
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM([]byte(caCertPEM)) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	return caCertPool, nil
}

// loadClientCertificate loads client certificate and key from PEM strings
func loadClientCertificate(clientCertPEM, clientKeyPEM string) (*tls.Certificate, error) {
	if clientCertPEM == "" && clientKeyPEM == "" {
		return nil, nil
	}

	if clientCertPEM == "" || clientKeyPEM == "" {
		return nil, fmt.Errorf("both client certificate and key must be provided")
	}

	cert, err := tls.X509KeyPair([]byte(clientCertPEM), []byte(clientKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}
	return &cert, nil
}

// configureTLS configures TLS settings for Redis connection
func (r *RedisExecutor) configureTLS(cfg *RedisConfig, opts *redis.Options) error {
	parsedURL, err := url.Parse(cfg.DatabaseConnectionString)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Only configure TLS for rediss:// scheme
	if parsedURL.Scheme != "rediss" {
		return nil
	}

	tlsConfig := &tls.Config{}

	if cfg.IgnoreTls {
		// Skip certificate verification
		tlsConfig.InsecureSkipVerify = true
		r.logger.Infof("TLS certificate verification disabled (IgnoreTls=true)")
	} else {
		tlsConfig.InsecureSkipVerify = false
		// Load CA certificate if provided
		if cfg.CaCert != "" {
			caCertPool, err := loadCACertPool(cfg.CaCert)
			if err != nil {
				return fmt.Errorf("failed to load CA certificate: %w", err)
			}
			tlsConfig.RootCAs = caCertPool
			r.logger.Infof("CA certificate loaded for TLS verification")
		}

		// Load client certificate if provided
		if cfg.ClientCert != "" || cfg.ClientKey != "" {
			clientCert, err := loadClientCertificate(cfg.ClientCert, cfg.ClientKey)
			if err != nil {
				return fmt.Errorf("failed to load client certificate: %w", err)
			}
			if clientCert != nil {
				tlsConfig.Certificates = []tls.Certificate{*clientCert}
				r.logger.Infof("Client certificate loaded for mutual TLS")
			}
		}

		if cfg.CaCert == "" {
			r.logger.Warnf("No CA certificate provided for TLS connection, skipping certificate verification")
		}
	}

	opts.TLSConfig = tlsConfig
	return nil
}

func NewRedisExecutor(logger *zap.SugaredLogger) *RedisExecutor {
	return &RedisExecutor{
		logger: logger,
	}
}

func (r *RedisExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[RedisConfig](configJSON)
}

func (r *RedisExecutor) Validate(configJSON string) error {
	cfg, err := r.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	redisConfig := cfg.(*RedisConfig)

	// Validate basic structure
	if err := GenericValidator(redisConfig); err != nil {
		return err
	}

	// Validate connection string format and components
	if err := r.validateRedisConnectionString(redisConfig.DatabaseConnectionString); err != nil {
		return fmt.Errorf("connection string validation failed: %w", err)
	}

	// Validate TLS settings
	if err := r.configureTLS(redisConfig, &redis.Options{}); err != nil {
		return fmt.Errorf("TLS configuration validation failed: %w", err)
	}

	return nil
}

func (r *RedisExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := r.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*RedisConfig)

	r.logger.Debugf("execute redis cfg: %+v", cfg)

	// Validate connection string before attempting connection
	if err := r.validateRedisConnectionString(cfg.DatabaseConnectionString); err != nil {
		r.logger.Infof("Redis connection string validation failed: %s, %s", m.Name, err.Error())
		return DownResult(fmt.Errorf("connection string validation failed: %w", err), time.Now().UTC(), time.Now().UTC())
	}

	startTime := time.Now().UTC()

	// Parse Redis connection string
	opts, err := redis.ParseURL(cfg.DatabaseConnectionString)
	if err != nil {
		r.logger.Infof("Redis connection string parse failed: %s, %s", m.Name, err.Error())
		return DownResult(fmt.Errorf("invalid Redis connection string: %w", err), startTime, time.Now().UTC())
	}

	// Configure TLS settings
	if err := r.configureTLS(cfg, opts); err != nil {
		r.logger.Infof("Redis TLS configuration failed: %s, %s", m.Name, err.Error())
		return DownResult(fmt.Errorf("TLS configuration failed: %w", err), startTime, time.Now().UTC())
	}

	// Set connection timeouts
	opts.DialTimeout = time.Duration(m.Timeout) * time.Second
	opts.ReadTimeout = time.Duration(m.Timeout) * time.Second
	opts.WriteTimeout = time.Duration(m.Timeout) * time.Second

	// Create Redis client
	client := redis.NewClient(opts)
	defer client.Close()

	// Create context with timeout for the ping operation
	pingCtx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()

	// Perform ping
	pong, err := client.Ping(pingCtx).Result()
	endTime := time.Now().UTC()

	if err != nil {
		r.logger.Infof("Redis ping failed: %s, %s", m.Name, err.Error())

		// Check if it's a connection error
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Redis connection timeout: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Redis ping failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	r.logger.Infof("Redis ping successful: %s, response: %s", m.Name, pong)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Redis ping successful: %s", pong),
		StartTime: startTime,
		EndTime:   endTime,
	}
}
