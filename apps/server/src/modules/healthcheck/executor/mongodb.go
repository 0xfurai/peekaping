package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"peekaping/src/modules/shared"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

type MongoDBConfig struct {
	ConnectionString string `json:"connectionString" validate:"required" example:"mongodb://username:password@host:port/database"`
	Command          string `json:"command" example:"{\"ping\": 1}"`
	JsonPath         string `json:"jsonPath" example:"$"`
	ExpectedValue    string `json:"expectedValue" example:""`
}

type MongoDBExecutor struct {
	logger *zap.SugaredLogger
}

func NewMongoDBExecutor(logger *zap.SugaredLogger) *MongoDBExecutor {
	return &MongoDBExecutor{
		logger: logger,
	}
}

func (m *MongoDBExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MongoDBConfig](configJSON)
}

func (m *MongoDBExecutor) Validate(configJSON string) error {
	cfg, err := m.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	mongoCfg := cfg.(*MongoDBConfig)

	if err := ValidateConnectionStringWithOptions(mongoCfg.ConnectionString, []string{"mongodb", "mongodb+srv"}, true); err != nil {
		return fmt.Errorf("invalid connection string: %w", err)
	}

	return GenericValidator(mongoCfg)
}

func (m *MongoDBExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MongoDBConfig)

	m.logger.Debugf("execute mongodb cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Parse MongoDB command
	var command bson.M
	if cfg.Command != "" {
		err = json.Unmarshal([]byte(cfg.Command), &command)
		if err != nil {
			return DownResult(fmt.Errorf("invalid MongoDB command JSON: %w", err), startTime, time.Now().UTC())
		}
	} else {
		// Default to ping command
		command = bson.M{"ping": 1}
	}

	// Run MongoDB command
	result, err := m.runMongoDBCommand(ctx, cfg.ConnectionString, command, time.Duration(monitor.Timeout)*time.Second)
	endTime := time.Now().UTC()

	if err != nil {
		m.logger.Infof("MongoDB command failed: %s, %s", monitor.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("MongoDB command failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Check if command was successful
	if ok, exists := result["ok"]; !exists || !m.isValueEqual(ok, 1) {
		m.logger.Infof("MongoDB command failed: %s, ok field is not 1", monitor.Name)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "MongoDB command failed",
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// If no JSON path is provided, consider it successful
	if cfg.JsonPath == "" {
		m.logger.Infof("MongoDB command successful: %s", monitor.Name)
		return &Result{
			Status:    shared.MonitorStatusUp,
			Message:   "Command executed successfully",
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Evaluate JSON path
	var evaluatedResult interface{}
	evaluatedResult, err = m.evaluateJsonPath(result, cfg.JsonPath)
	if err != nil {
		m.logger.Infof("MongoDB JSON path evaluation failed: %s, %s", monitor.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("JSON path evaluation failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	if evaluatedResult == nil {
		m.logger.Infof("MongoDB JSON path returned null: %s", monitor.Name)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Queried value not found",
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Check expected value if provided
	if cfg.ExpectedValue != "" {
		if m.isValueEqual(evaluatedResult, cfg.ExpectedValue) {
			m.logger.Infof("MongoDB expected value matched: %s", monitor.Name)
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   "Command executed successfully and expected value was found",
				StartTime: startTime,
				EndTime:   endTime,
			}
		} else {
			resultStr := fmt.Sprintf("%v", evaluatedResult)
			m.logger.Infof("MongoDB expected value mismatch: %s, got %s, expected %s", monitor.Name, resultStr, cfg.ExpectedValue)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Query executed, but value is not equal to expected value, value was: [%s]", resultStr),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	}

	// JSON path evaluation successful
	m.logger.Infof("MongoDB JSON path evaluation successful: %s", monitor.Name)
	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   "Command executed successfully and the jsonata expression produces a result",
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// runMongoDBCommand connects to MongoDB and executes the given command
func (m *MongoDBExecutor) runMongoDBCommand(ctx context.Context, connectionString string, command bson.M, timeout time.Duration) (bson.M, error) {
	// Ensure proper authentication source is set
	enhancedConnectionString, err := m.enhanceConnectionString(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to enhance connection string: %w", err)
	}

	// Set connection timeout
	clientOptions := options.Client().ApplyURI(enhancedConnectionString)
	clientOptions.SetConnectTimeout(timeout)
	clientOptions.SetSocketTimeout(timeout)

	// Connect to MongoDB
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	// Extract database name from connection string
	dbName, err := m.extractDatabaseName(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to extract database name: %w", err)
	}

	// Get the database
	db := client.Database(dbName)

	// Execute the command
	var result bson.M
	err = db.RunCommand(ctx, command).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MongoDB command: %w", err)
	}

	return result, nil
}

// enhanceConnectionString adds necessary authentication parameters if missing
func (m *MongoDBExecutor) enhanceConnectionString(connectionString string) (string, error) {
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
	}

	// Get existing query parameters
	queryParams := parsedURL.Query()

	// If user is provided but no authSource is specified, default to admin database
	if parsedURL.User != nil && parsedURL.User.Username() != "" {
		if queryParams.Get("authSource") == "" {
			queryParams.Set("authSource", "admin")
		}
	}

	// Update the URL with enhanced query parameters
	parsedURL.RawQuery = queryParams.Encode()

	return parsedURL.String(), nil
}

// extractDatabaseName extracts the database name from a MongoDB connection string
func (m *MongoDBExecutor) extractDatabaseName(connectionString string) (string, error) {
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
	}

	// Extract database name from path (remove leading slash)
	dbName := strings.TrimPrefix(parsedURL.Path, "/")

	// Remove any additional path segments and query parameters
	if idx := strings.Index(dbName, "?"); idx != -1 {
		dbName = dbName[:idx]
	}

	if dbName == "" {
		return "", fmt.Errorf("database name not found in connection string")
	}

	return dbName, nil
}

// evaluateJsonPath provides basic JSON path evaluation
// This is a simplified implementation - for production use, consider using a proper JSONPath library
func (m *MongoDBExecutor) evaluateJsonPath(data bson.M, path string) (interface{}, error) {
	// Handle root path
	if path == "" || path == "$" {
		return data, nil
	}

	// Remove leading $ if present
	if path[0] == '$' {
		path = path[1:]
	}

	// Remove leading . if present
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	// If path is empty after cleanup, return root
	if path == "" {
		return data, nil
	}

	// Simple field access (e.g., "field" or "field.subfield")
	return m.getNestedValue(data, path)
}

// getNestedValue retrieves a nested value from a map using dot notation
func (m *MongoDBExecutor) getNestedValue(data interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}

	// Split path by dots
	keys := splitPath(path)

	current := data
	for _, key := range keys {
		switch v := current.(type) {
		case bson.M:
			if val, exists := v[key]; exists {
				current = val
			} else {
				return nil, fmt.Errorf("field '%s' not found", key)
			}
		case map[string]interface{}:
			if val, exists := v[key]; exists {
				current = val
			} else {
				return nil, fmt.Errorf("field '%s' not found", key)
			}
		default:
			return nil, fmt.Errorf("cannot access field '%s' on non-object type", key)
		}
	}

	return current, nil
}

// splitPath splits a path string by dots, handling escaped dots
func splitPath(path string) []string {
	parts := make([]string, 0)
	var current string
	var escaped bool

	if path == "" {
		return parts // Return empty slice for empty path
	}

	for _, char := range path {
		if escaped {
			current += string(char)
			escaped = false
			continue
		}

		if char == '\\' {
			escaped = true
			continue
		}

		if char == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
			continue
		}

		current += string(char)
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// isValueEqual checks if two values are equal, handling different numeric types
func (m *MongoDBExecutor) isValueEqual(actual interface{}, expected interface{}) bool {
	// Direct equality check
	if actual == expected {
		return true
	}

	// Try to convert both to strings and compare
	actualStr := fmt.Sprintf("%v", actual)
	expectedStr := fmt.Sprintf("%v", expected)

	if actualStr == expectedStr {
		return true
	}

	// Try numeric comparison
	actualNum, err1 := strconv.ParseFloat(actualStr, 64)
	expectedNum, err2 := strconv.ParseFloat(expectedStr, 64)

	if err1 == nil && err2 == nil {
		return actualNum == expectedNum
	}

	return false
}
