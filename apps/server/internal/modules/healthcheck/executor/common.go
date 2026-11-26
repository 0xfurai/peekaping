package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"peekaping/internal/modules/shared"
	"peekaping/internal/validation"
	"slices"
	"time"
)

func GenericValidator[T any](cfg *T) error {
	return validation.Validate.Struct(cfg)
}

func GenericUnmarshal[T any](configJSON string) (*T, error) {
	var cfg T
	dec := json.NewDecoder(bytes.NewReader([]byte(configJSON)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// Helper to create a down result
func DownResult(err error, startTime, endTime time.Time) *Result {
	return &Result{
		Status:    shared.MonitorStatusDown,
		Message:   err.Error(),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// ValidateConnectionString validates database connection strings for various database types
func ValidateConnectionString(connectionString string, supportedSchemes []string) error {
	return ValidateConnectionStringWithOptions(connectionString, supportedSchemes, false)
}

// ValidateConnectionStringWithOptions validates database connection strings with optional username requirement
func ValidateConnectionStringWithOptions(connectionString string, supportedSchemes []string, allowNoUsername bool) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return fmt.Errorf("invalid connection string format: %w", err)
	}

	if err := validateConnectionScheme(parsedURL.Scheme, supportedSchemes); err != nil {
		return err
	}

	if err := validateConnectionAuthority(parsedURL, allowNoUsername); err != nil {
		return err
	}

	if parsedURL.Path == "" || parsedURL.Path == "/" {
		return fmt.Errorf("connection string must include database name")
	}

	if parsedURL.Port() == "0" {
		return fmt.Errorf("invalid port: 0")
	}

	return nil
}

// validateConnectionScheme checks that the scheme is one of the supported ones
func validateConnectionScheme(scheme string, supportedSchemes []string) error {
	if slices.Contains(supportedSchemes, scheme) {
		return nil
	}
	if len(supportedSchemes) == 1 {
		return fmt.Errorf("connection string must use %s:// scheme, got: %s", supportedSchemes[0], scheme)
	}
	return fmt.Errorf("connection string must use one of %v schemes, got: %s", supportedSchemes, scheme)
}

// validateConnectionAuthority checks the host, and the username unless allowNoUsername is set
func validateConnectionAuthority(parsedURL *url.URL, allowNoUsername bool) error {
	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fmt.Errorf("connection string must include host")
	}

	if !allowNoUsername && (parsedURL.User == nil || parsedURL.User.Username() == "") {
		return fmt.Errorf("connection string must include username")
	}

	return nil
}

// connectionStringHosts returns the host of a connection string written as a URL, such as a
// postgres:// or mysql:// one. A string that isn't a URL has none: the check reports it.
func connectionStringHosts(connectionString string) []string {
	u, err := url.Parse(connectionString)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return []string{u.Hostname()}
}
