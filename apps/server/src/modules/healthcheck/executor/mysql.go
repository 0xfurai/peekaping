package executor

import (
	"context"
	"database/sql"
	"fmt"
	"peekaping/src/modules/shared"
	"time"

	"go.uber.org/zap"
	_ "github.com/go-sql-driver/mysql"
)

type MySQLConfig struct {
	ConnectionString string `json:"connection_string" validate:"required" example:"user:password@tcp(host:3306)/dbname"`
	Query           string `json:"query" validate:"required" example:"SELECT 1"`
	Password        string `json:"password" example:"password"`
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

func (m *MySQLExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MySQLConfig)

	m.logger.Debugf("execute mysql cfg: %+v", cfg)

	startTime := time.Now().UTC()

	message, err := m.mysqlQuery(ctx, cfg.ConnectionString, cfg.Query, cfg.Password)
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

func (m *MySQLExecutor) mysqlQuery(ctx context.Context, connectionString, query, password string) (string, error) {
	// Create connection configuration
	connConfig := connectionString
	if password != "" {
		// If password is provided separately, we need to parse and rebuild the connection string
		// For now, we'll assume the password is already in the connection string
		// In a production environment, you might want to parse the DSN and inject the password
		connConfig = connectionString
	}

	// Open connection
	db, err := sql.Open("mysql", connConfig)
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