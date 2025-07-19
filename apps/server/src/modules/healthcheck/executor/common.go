package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"peekaping/src/modules/shared"
	"peekaping/src/utils"
	"time"
)

func GenericValidator[T any](cfg *T) error {
	return utils.Validate.Struct(cfg)
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

	// Check if the scheme is supported
	schemeSupported := false
	for _, scheme := range supportedSchemes {
		if parsedURL.Scheme == scheme {
			schemeSupported = true
			break
		}
	}

	if !schemeSupported {
		if len(supportedSchemes) == 1 {
			return fmt.Errorf("connection string must use %s:// scheme, got: %s", supportedSchemes[0], parsedURL.Scheme)
		} else {
			return fmt.Errorf("connection string must use one of %v schemes, got: %s", supportedSchemes, parsedURL.Scheme)
		}
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fmt.Errorf("connection string must include host")
	}

	// Only require username if allowNoUsername is false
	if !allowNoUsername {
		if parsedURL.User == nil {
			return fmt.Errorf("connection string must include username")
		}

		if parsedURL.User.Username() == "" {
			return fmt.Errorf("connection string must include username")
		}
	}

	if parsedURL.Path == "" || parsedURL.Path == "/" {
		return fmt.Errorf("connection string must include database name")
	}

	// Validate port if provided
	if port := parsedURL.Port(); port != "" {
		if port == "0" {
			return fmt.Errorf("invalid port: 0")
		}
	}

	return nil
}
