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
	Query            string `json:"query" validate:"required" example:"SELECT 1"`
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
	return GenericValidator(cfg.(*MySQLConfig))
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

	message, err := m.mysqlQuery(ctx, cfg.ConnectionString, cfg.Query)
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

	m.logger.Infof("MySQL query successful: %s", monitor.Name)
	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

func (m *MySQLExecutor) mysqlQuery(ctx context.Context, connectionString, query string) (string, error) {
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
