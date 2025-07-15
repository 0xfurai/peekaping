package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"peekaping/src/modules/shared"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type MySQLConfig struct {
	ConnectionString string `json:"connection_string" validate:"required" example:"mysql://user:password@host:3306/dbname"`
	Query            string `json:"query" validate:"omitempty" example:"SELECT 1"`
}

type MySQLExecutor struct {
	logger *zap.SugaredLogger
}

func NewMySQLExecutor(logger *zap.SugaredLogger) *MySQLExecutor {
	return &MySQLExecutor{
		logger: logger,
	}
}

func (m *MySQLExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MySQLConfig](configJSON)
}

func (m *MySQLExecutor) Validate(configJSON string) error {
	cfg, err := m.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	mysqlCfg := cfg.(*MySQLConfig)

	if err := ValidateConnectionString(mysqlCfg.ConnectionString, []string{"mysql"}); err != nil {
		return fmt.Errorf("invalid connection string: %w", err)
	}

	if mysqlCfg.Query != "" && strings.TrimSpace(mysqlCfg.Query) != "" {
		if err := m.validateQuery(mysqlCfg.Query); err != nil {
			return fmt.Errorf("invalid query: %w", err)
		}
	}

	return GenericValidator(mysqlCfg)
}

func (m *MySQLExecutor) validateQuery(query string) error {
	if query == "" {
		return fmt.Errorf("query cannot be empty")
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return fmt.Errorf("query cannot be empty or whitespace only")
	}

	// Basic SQL injection prevention - check for dangerous patterns
	lowerQuery := strings.ToLower(trimmedQuery)

	// Allow only SELECT, SHOW, DESCRIBE, EXPLAIN statements for safety
	allowedPrefixes := []string{"select", "show", "describe", "explain", "desc"}
	isAllowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(lowerQuery, prefix) {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return fmt.Errorf("only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed for monitoring queries")
	}

	return nil
}

// parseMySQLURL parses a mysql:// URL and converts it to a DSN format for the Go MySQL driver
func (m *MySQLExecutor) parseMySQLURL(connectionString string) (string, error) {
	// Parse the URL
	u, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check if it's a mysql:// URL
	if u.Scheme != "mysql" {
		return "", fmt.Errorf("connection string must use mysql:// scheme, got: %s", u.Scheme)
	}

	// Extract user and password
	var user, pass string
	if u.User != nil {
		user = u.User.Username()
		if p, ok := u.User.Password(); ok {
			pass = p
		}
	}

	if user == "" {
		return "", fmt.Errorf("username is required in connection string")
	}

	// Extract host and port
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "3306" // Default MySQL port
	}

	// Extract database name
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		return "", fmt.Errorf("database name is required in connection string")
	}

	// Build DSN in the format: user:password@tcp(host:port)/database
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", user, pass, host, port, database)

	// Add query parameters if present
	if u.RawQuery != "" {
		dsn += "?" + u.RawQuery
	}

	return dsn, nil
}

func (m *MySQLExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MySQLConfig)

	m.logger.Debugf("execute mysql cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Validate configuration before execution
	if err := ValidateConnectionString(cfg.ConnectionString, []string{"mysql"}); err != nil {
		return DownResult(fmt.Errorf("connection string validation failed: %w", err), startTime, time.Now().UTC())
	}

	query := cfg.Query
	if query == "" || strings.TrimSpace(query) == "" {
		query = "SELECT 1"
	} else {
		// Validate query before execution
		if err := m.validateQuery(query); err != nil {
			return DownResult(fmt.Errorf("query validation failed: %w", err), startTime, time.Now().UTC())
		}
	}

	message, err := m.mysqlQuery(ctx, cfg.ConnectionString, query, time.Duration(monitor.Timeout)*time.Second)
	endTime := time.Now().UTC()

	if err != nil {
		m.logger.Infof("MySQL query failed: %s, %s", monitor.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("MySQL query failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	ping := endTime.Sub(startTime).Milliseconds()
	m.logger.Infof("MySQL query successful: %s, ping: %dms", monitor.Name, ping)
	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Query successful, ping: %dms, %s", ping, message),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

func (m *MySQLExecutor) mysqlQuery(ctx context.Context, connectionString, query string, timeout time.Duration) (string, error) {
	// Parse the mysql:// URL format and convert to DSN
	dsn, err := m.parseMySQLURL(connectionString)
	if err != nil {
		return "", fmt.Errorf("failed to parse MySQL connection string: %w", err)
	}

	// Open connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", fmt.Errorf("failed to open MySQL connection: %w", err)
	}
	defer db.Close()

	// Set connection timeout using the monitor's configured timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return "", fmt.Errorf("failed to ping MySQL database: %w", err)
	}

	// Execute query
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Count rows
	rowCount := 0
	for rows.Next() {
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error while iterating rows: %w", err)
	}

	return fmt.Sprintf("Rows: %d", rowCount), nil
}
