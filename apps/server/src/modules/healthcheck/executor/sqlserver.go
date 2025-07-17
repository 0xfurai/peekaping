package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"peekaping/src/modules/shared"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb" // Microsoft SQL Server driver
	"go.uber.org/zap"
)

type SQLServerConfig struct {
	DatabaseConnectionString string `json:"database_connection_string" validate:"required" example:"Server=localhost,1433;Database=master;User Id=sa;Password=password;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"`
	DatabaseQuery            string `json:"database_query" validate:"omitempty" example:"SELECT 1"`
}

type SQLServerExecutor struct {
	logger *zap.SugaredLogger
}

func NewSQLServerExecutor(logger *zap.SugaredLogger) *SQLServerExecutor {
	return &SQLServerExecutor{
		logger: logger,
	}
}

func (s *SQLServerExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SQLServerConfig](configJSON)
}

// Regex to validate SQL Server connection string format
var sqlServerConnectionStringRegex = regexp.MustCompile(`^Server=([^;,]+)(,\d+)?;Database=[^;]+;User Id=[^;]+;Password=[^;]*;?.*$`)

func (s *SQLServerExecutor) validateConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	// Check if it's the new semicolon-separated format
	if sqlServerConnectionStringRegex.MatchString(connectionString) {
		return s.validateSemicolonFormat(connectionString)
	}

	// Check if it's the legacy URL format (for backward compatibility)
	if strings.HasPrefix(connectionString, "sqlserver://") || strings.HasPrefix(connectionString, "mssql://") {
		return ValidateConnectionString(connectionString, []string{"sqlserver", "mssql"})
	}

	return fmt.Errorf("invalid connection string format. Expected: Server=hostname,port;Database=database;User Id=username;Password=password;...")
}

func (s *SQLServerExecutor) validateSemicolonFormat(connectionString string) error {
	params := s.parseConnectionStringParams(connectionString)

	// Validate required parameters
	if params["Server"] == "" {
		return fmt.Errorf("Server parameter is required")
	}
	if params["Database"] == "" {
		return fmt.Errorf("Database parameter is required")
	}
	if params["User Id"] == "" {
		return fmt.Errorf("User Id parameter is required")
	}

	// Validate server format (can include port)
	serverParts := strings.Split(params["Server"], ",")
	if len(serverParts) > 2 {
		return fmt.Errorf("invalid Server format. Expected: hostname or hostname,port")
	}
	if len(serverParts) == 2 {
		port, err := strconv.Atoi(serverParts[1])
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid port number: %s", serverParts[1])
		}
	}

	// Validate boolean parameters if present
	if encrypt := params["Encrypt"]; encrypt != "" {
		if !isValidBooleanString(encrypt) {
			return fmt.Errorf("invalid Encrypt value: %s. Expected: true or false", encrypt)
		}
	}
	if trustCert := params["TrustServerCertificate"]; trustCert != "" {
		if !isValidBooleanString(trustCert) {
			return fmt.Errorf("invalid TrustServerCertificate value: %s. Expected: true or false", trustCert)
		}
	}

	// Validate Connection Timeout if present
	if timeout := params["Connection Timeout"]; timeout != "" {
		timeoutVal, err := strconv.Atoi(timeout)
		if err != nil || timeoutVal < 0 {
			return fmt.Errorf("invalid Connection Timeout value: %s. Expected: positive integer", timeout)
		}
	}

	return nil
}

func isValidBooleanString(value string) bool {
	lower := strings.ToLower(value)
	return lower == "true" || lower == "false" || lower == "yes" || lower == "no"
}

func (s *SQLServerExecutor) parseConnectionStringParams(connectionString string) map[string]string {
	params := make(map[string]string)
	parts := strings.Split(connectionString, ";")

	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			if idx := strings.Index(part, "="); idx > 0 {
				key := strings.TrimSpace(part[:idx])
				value := strings.TrimSpace(part[idx+1:])
				params[key] = value
			}
		}
	}

	return params
}

func (s *SQLServerExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	sqlServerCfg := cfg.(*SQLServerConfig)

	if err := s.validateConnectionString(sqlServerCfg.DatabaseConnectionString); err != nil {
		return fmt.Errorf("invalid database connection string: %w", err)
	}

	if sqlServerCfg.DatabaseQuery != "" {
		if err := s.validateQuery(sqlServerCfg.DatabaseQuery); err != nil {
			return fmt.Errorf("invalid query: %w", err)
		}
	}

	return GenericValidator(sqlServerCfg)
}

func (s *SQLServerExecutor) validateQuery(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	// Convert to uppercase for checking
	upperQuery := strings.ToUpper(query)

	// List of allowed query prefixes
	allowedPrefixes := []string{
		"SELECT",
		"SHOW",
		"DESCRIBE", "DESC",
		"EXPLAIN",
		"WITH",
		"VALUES",
	}

	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upperQuery, prefix) {
			return nil
		}
	}

	return fmt.Errorf("query must start with one of: %s", strings.Join(allowedPrefixes, ", "))
}

func (s *SQLServerExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := s.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*SQLServerConfig)

	s.logger.Debugf("execute sqlserver cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Validate configuration before execution
	if err := s.validateConnectionString(cfg.DatabaseConnectionString); err != nil {
		return DownResult(fmt.Errorf("connection string validation failed: %w", err), startTime, time.Now().UTC())
	}

	// Parse and validate connection string
	dsn, err := s.parseConnectionString(cfg.DatabaseConnectionString)
	if err != nil {
		return DownResult(fmt.Errorf("failed to parse connection string: %w", err), startTime, time.Now().UTC())
	}

	// Open connection
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return DownResult(fmt.Errorf("failed to open SQL Server connection: %w", err), startTime, time.Now().UTC())
	}
	defer db.Close()

	// Set connection timeout using the monitor's configured timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return DownResult(fmt.Errorf("connection failed: %w", err), startTime, time.Now().UTC())
	}

	query := cfg.DatabaseQuery
	if query == "" || strings.TrimSpace(query) == "" {
		query = "SELECT 1"
	} else {
		// Validate query before execution
		if err := s.validateQuery(query); err != nil {
			return DownResult(fmt.Errorf("query validation failed: %w", err), startTime, time.Now().UTC())
		}
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return DownResult(fmt.Errorf("query execution failed: %w", err), startTime, time.Now().UTC())
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return DownResult(fmt.Errorf("failed to get columns: %w", err), startTime, time.Now().UTC())
	}

	// Count rows
	rowCount := 0
	for rows.Next() {
		rowCount++
		// We only need to count, don't need to scan values
	}

	if err := rows.Err(); err != nil {
		return DownResult(fmt.Errorf("error iterating rows: %w", err), startTime, time.Now().UTC())
	}

	endTime := time.Now().UTC()
	ping := endTime.Sub(startTime).Milliseconds()

	s.logger.Infof("SQL Server query successful: %s, ping: %dms", m.Name, ping)
	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Query successful, ping: %dms, columns: %d, rows: %d", ping, len(columns), rowCount),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

func (s *SQLServerExecutor) parseConnectionString(connectionString string) (string, error) {
	// Check if it's the new semicolon-separated format
	if sqlServerConnectionStringRegex.MatchString(connectionString) {
		// It's already in the correct format for go-mssqldb driver
		return connectionString, nil
	}

	// Handle legacy URL format for backward compatibility
	if strings.HasPrefix(connectionString, "sqlserver://") || strings.HasPrefix(connectionString, "mssql://") {
		return s.parseURLConnectionString(connectionString)
	}

	return "", fmt.Errorf("unsupported connection string format")
}

func (s *SQLServerExecutor) parseURLConnectionString(connectionString string) (string, error) {
	// Parse the URL
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check if it's a sqlserver:// or mssql:// URL
	if parsedURL.Scheme != "sqlserver" && parsedURL.Scheme != "mssql" {
		return "", fmt.Errorf("invalid scheme: %s, expected sqlserver:// or mssql://", parsedURL.Scheme)
	}

	// Extract user and password
	var user, password string
	if parsedURL.User != nil {
		user = parsedURL.User.Username()
		if p, ok := parsedURL.User.Password(); ok {
			password = p
		}
	}

	// Extract host and port
	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		port = "1433" // Default SQL Server port
	}

	// Extract database from query parameters
	queryParams := parsedURL.Query()
	database := queryParams.Get("database")
	if database == "" {
		// Try to get database from path
		if parsedURL.Path != "" && parsedURL.Path != "/" {
			database = strings.TrimPrefix(parsedURL.Path, "/")
		}
	}

	// Build connection string in the format expected by go-mssqldb
	var dsnParts []string

	if host != "" {
		dsnParts = append(dsnParts, fmt.Sprintf("server=%s", host))
	}
	if port != "" {
		dsnParts = append(dsnParts, fmt.Sprintf("port=%s", port))
	}
	if user != "" {
		dsnParts = append(dsnParts, fmt.Sprintf("user id=%s", user))
	}
	if password != "" {
		dsnParts = append(dsnParts, fmt.Sprintf("password=%s", password))
	}
	if database != "" {
		dsnParts = append(dsnParts, fmt.Sprintf("database=%s", database))
	}

	// Add other query parameters
	for key, values := range queryParams {
		if key != "database" && len(values) > 0 {
			dsnParts = append(dsnParts, fmt.Sprintf("%s=%s", key, values[0]))
		}
	}

	return strings.Join(dsnParts, ";"), nil
}
