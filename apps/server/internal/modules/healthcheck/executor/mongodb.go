//go:build !slim

package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"peekaping/internal/modules/shared"
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
	guarded
	logger *zap.SugaredLogger
}

func NewMongoDBExecutor(logger *zap.SugaredLogger) *MongoDBExecutor {
	return &MongoDBExecutor{
		logger: logger,
	}
}

func init() { registerOptional("mongodb", NewMongoDBExecutor) }

func (m *MongoDBExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MongoDBConfig](configJSON)
}

func (m *MongoDBExecutor) Validate(configJSON string) error {
	cfg, err := m.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	mongoCfg := cfg.(*MongoDBConfig)

	if err := validateMongoConnectionString(mongoCfg.ConnectionString); err != nil {
		return fmt.Errorf("invalid connection string: %w", err)
	}

	return GenericValidator(mongoCfg)
}

// Targets lists the hosts a MongoDB check connects to: those of a seed list, or the name an SRV
// record is looked up for (what it points to is checked when the driver dials it). A host that is a
// Unix socket is left out: the dialer refuses it.
func (m *MongoDBExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[MongoDBConfig](configJSON)
	if err != nil {
		return nil, err
	}
	parsed, ok := splitMongoConnectionString(cfg.ConnectionString)
	if !ok {
		return nil, nil
	}
	var hosts []string
	for _, entry := range parsed.hosts {
		if host, err := url.PathUnescape(strings.TrimSpace(entry)); err == nil && !strings.HasSuffix(host, ".sock") && host != "" {
			hosts = append(hosts, hostOf(host))
		}
	}
	return hosts, nil
}

func (m *MongoDBExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MongoDBConfig)

	startTime := time.Now().UTC()

	command, err := parseMongoDBCommand(cfg.Command)
	if err != nil {
		return DownResult(fmt.Errorf("invalid MongoDB command JSON: %w", err), startTime, time.Now().UTC())
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

	return m.checkCommandResult(monitor, cfg, result, startTime, endTime)
}

// parseMongoDBCommand parses the command as JSON. An empty command is a ping.
func parseMongoDBCommand(commandJSON string) (bson.M, error) {
	if commandJSON == "" {
		// Default to ping command
		return bson.M{"ping": 1}, nil
	}

	var command bson.M
	err := json.Unmarshal([]byte(commandJSON), &command)
	return command, err
}

// checkCommandResult is the check's result for the command's reply: Down unless the command
// succeeded and, when the config asks for it, the value at its JSON path exists and is the expected one
func (m *MongoDBExecutor) checkCommandResult(monitor *Monitor, cfg *MongoDBConfig, result bson.M, startTime, endTime time.Time) *Result {
	resultOf := func(status shared.MonitorStatus, message string) *Result {
		return &Result{
			Status:    status,
			Message:   message,
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Check if command was successful
	if ok, exists := result["ok"]; !exists || !m.isValueEqual(ok, 1) {
		m.logger.Infof("MongoDB command failed: %s, ok field is not 1", monitor.Name)
		return resultOf(shared.MonitorStatusDown, "MongoDB command failed")
	}

	// If no JSON path is provided, consider it successful
	if cfg.JsonPath == "" {
		m.logger.Infof("MongoDB command successful: %s", monitor.Name)
		return resultOf(shared.MonitorStatusUp, "Command executed successfully")
	}

	// Evaluate JSON path
	evaluatedResult, err := m.evaluateJsonPath(result, cfg.JsonPath)
	if err != nil {
		m.logger.Infof("MongoDB JSON path evaluation failed: %s, %s", monitor.Name, err.Error())
		return resultOf(shared.MonitorStatusDown, fmt.Sprintf("JSON path evaluation failed: %v", err))
	}

	if evaluatedResult == nil {
		m.logger.Infof("MongoDB JSON path returned null: %s", monitor.Name)
		return resultOf(shared.MonitorStatusDown, "Queried value not found")
	}

	// Check expected value if provided
	if cfg.ExpectedValue != "" {
		status, message := m.checkExpectedValue(monitor, cfg, evaluatedResult)
		return resultOf(status, message)
	}

	// JSON path evaluation successful
	m.logger.Infof("MongoDB JSON path evaluation successful: %s", monitor.Name)
	return resultOf(shared.MonitorStatusUp, "Command executed successfully and the jsonata expression produces a result")
}

// checkExpectedValue is Up when the value at the JSON path is the expected one, and Down otherwise
func (m *MongoDBExecutor) checkExpectedValue(monitor *Monitor, cfg *MongoDBConfig, evaluatedResult interface{}) (shared.MonitorStatus, string) {
	if m.isValueEqual(evaluatedResult, cfg.ExpectedValue) {
		m.logger.Infof("MongoDB expected value matched: %s", monitor.Name)
		return shared.MonitorStatusUp, "Command executed successfully and expected value was found"
	}

	resultStr := fmt.Sprintf("%v", evaluatedResult)
	m.logger.Infof("MongoDB expected value mismatch: %s, got %s, expected %s", monitor.Name, resultStr, cfg.ExpectedValue)
	return shared.MonitorStatusDown, fmt.Sprintf("Query executed, but value is not equal to expected value, value was: [%s]", resultStr)
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
	if timeout > 0 {
		// The driver waits 30s for a server it can't select, whatever the
		// connect timeout says. The deadline bounds connecting and the
		// command together, so a check never outlasts the monitor's timeout.
		clientOptions.SetServerSelectionTimeout(timeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Dial through the guard: every connection the driver makes goes through the dialer, including
	// those to the servers an SRV record or the replica set's configuration names, and a Unix
	// socket is a connection it makes in the same way.
	clientOptions.SetDialer(m.dialer(&net.Dialer{Timeout: timeout}))

	// Connect to MongoDB
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() {
		// The deadline may have passed, which would stop a clean disconnect.
		disconnectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()

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

// enhanceConnectionString adds necessary authentication parameters if missing. The string is
// kept as written, since the driver reads its seed list and escapes itself.
func (m *MongoDBExecutor) enhanceConnectionString(connectionString string) (string, error) {
	parsed, ok := splitMongoConnectionString(connectionString)
	if !ok {
		return "", fmt.Errorf("invalid connection string format: missing ://")
	}

	// If user is provided but no authSource is specified, default to admin database
	user, _, _ := strings.Cut(parsed.userinfo, ":")
	if user == "" {
		return connectionString, nil
	}
	if options, _ := url.ParseQuery(parsed.query); options.Get("authSource") != "" {
		return connectionString, nil
	}
	switch {
	case !parsed.hasQuery:
		return connectionString + "?authSource=admin", nil
	case parsed.query == "":
		return connectionString + "authSource=admin", nil
	}
	return connectionString + "&authSource=admin", nil
}

// extractDatabaseName extracts the database name from a MongoDB connection string
func (m *MongoDBExecutor) extractDatabaseName(connectionString string) (string, error) {
	parsed, ok := splitMongoConnectionString(connectionString)
	if !ok {
		return "", fmt.Errorf("invalid connection string format: missing ://")
	}

	dbName, err := url.PathUnescape(parsed.database)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
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
