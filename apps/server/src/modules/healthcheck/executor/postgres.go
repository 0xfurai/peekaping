package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"peekaping/src/modules/shared"
	"strings"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

type PostgresConfig struct {
	DatabaseConnectionString string `json:"database_connection_string" validate:"required" example:"postgres://user:password@localhost:5432/database"`
	DatabaseQuery           string `json:"database_query" validate:"omitempty" example:"SELECT 1"`
}

type PostgresExecutor struct {
	logger *zap.SugaredLogger
}

func NewPostgresExecutor(logger *zap.SugaredLogger) *PostgresExecutor {
	return &PostgresExecutor{
		logger: logger,
	}
}

func (p *PostgresExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PostgresConfig](configJSON)
}

func (p *PostgresExecutor) Validate(configJSON string) error {
	cfg, err := p.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	pgCfg := cfg.(*PostgresConfig)
	
	// Validate connection string format
	if err := p.validateConnectionString(pgCfg.DatabaseConnectionString); err != nil {
		return fmt.Errorf("invalid database connection string: %w", err)
	}
	
	return GenericValidator(pgCfg)
}

func (p *PostgresExecutor) validateConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	// Parse the connection string to validate format
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check for required components
	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		return fmt.Errorf("connection string must use postgres:// or postgresql:// scheme")
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fmt.Errorf("connection string must include host")
	}

	if parsedURL.User == nil {
		return fmt.Errorf("connection string must include username")
	}

	if parsedURL.Path == "" || parsedURL.Path == "/" {
		return fmt.Errorf("connection string must include database name")
	}

	return nil
}

func (p *PostgresExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := p.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*PostgresConfig)

	p.logger.Debugf("execute postgres cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Parse connection string and create config
	config, err := p.parseConnectionString(cfg.DatabaseConnectionString)
	if err != nil {
		return DownResult(fmt.Errorf("failed to parse connection string: %w", err), startTime, time.Now().UTC())
	}

	// Fix #3868, handle SSL parameter as string
	if sslStr, ok := config["sslmode"]; ok && sslStr != "" {
		// Keep SSL mode as string for pgdriver
		config["sslmode"] = sslStr
	} else {
		config["sslmode"] = "disable"
	}

	// Check for empty password
	if config["password"] == "" {
		return DownResult(fmt.Errorf("password is undefined"), startTime, time.Now().UTC())
	}

	// Create PostgreSQL connector
	connector := pgdriver.NewConnector(
		pgdriver.WithNetwork("tcp"),
		pgdriver.WithAddr(fmt.Sprintf("%s:%s", config["host"], config["port"])),
		pgdriver.WithUser(config["user"]),
		pgdriver.WithPassword(config["password"]),
		pgdriver.WithDatabase(config["dbname"]),
		pgdriver.WithInsecure(config["sslmode"] == "disable"),
	)

	// Create database connection
	db := sql.OpenDB(connector)
	defer db.Close()

	// Set connection timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return DownResult(fmt.Errorf("connection failed: %w", err), startTime, time.Now().UTC())
	}

	// Determine query to execute
	query := cfg.DatabaseQuery
	if query == "" || strings.TrimSpace(query) == "" {
		query = "SELECT 1"
	}

	// Execute query
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		endTime := time.Now().UTC()
		p.logger.Infof("PostgreSQL query failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Query failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}
	defer rows.Close()

	// Verify that query returned successfully
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			endTime := time.Now().UTC()
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Query error: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	}

	endTime := time.Now().UTC()
	ping := endTime.Sub(startTime).Milliseconds()

	p.logger.Infof("PostgreSQL query successful: %s, ping: %dms", m.Name, ping)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Query successful, ping: %dms", ping),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

func (p *PostgresExecutor) parseConnectionString(connectionString string) (map[string]string, error) {
	config := make(map[string]string)

	// Parse URL
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check if this is a proper PostgreSQL URL
	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		return nil, fmt.Errorf("invalid scheme: %s", parsedURL.Scheme)
	}

	// Extract components
	if parsedURL.User != nil {
		config["user"] = parsedURL.User.Username()
		if password, ok := parsedURL.User.Password(); ok {
			config["password"] = password
		}
	}

	if parsedURL.Host != "" {
		host := parsedURL.Host
		if strings.Contains(host, ":") {
			parts := strings.Split(host, ":")
			config["host"] = parts[0]
			config["port"] = parts[1]
		} else {
			config["host"] = host
			config["port"] = "5432" // Default PostgreSQL port
		}
	}

	if parsedURL.Path != "" && parsedURL.Path != "/" {
		config["dbname"] = strings.TrimPrefix(parsedURL.Path, "/")
	}

	// Parse query parameters
	queryParams := parsedURL.Query()
	for key, values := range queryParams {
		if len(values) > 0 {
			config[key] = values[0]
		}
	}

	return config, nil
}