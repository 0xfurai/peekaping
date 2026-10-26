package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"peekaping/internal/modules/shared"
	"strings"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

type PostgresConfig struct {
	DatabaseConnectionString string `json:"database_connection_string" validate:"required" example:"postgres://user:password@localhost:5432/database"`
	DatabaseQuery            string `json:"database_query" validate:"omitempty" example:"SELECT 1"`
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

	if err := ValidateConnectionString(pgCfg.DatabaseConnectionString, []string{"postgres", "postgresql"}); err != nil {
		return fmt.Errorf("invalid database connection string: %w", err)
	}

	if pgCfg.DatabaseQuery != "" {
		if err := p.validateQuery(pgCfg.DatabaseQuery); err != nil {
			return fmt.Errorf("invalid query: %w", err)
		}
	}

	return GenericValidator(pgCfg)
}

func (p *PostgresExecutor) validateQuery(query string) error {
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
	// PostgreSQL specific: also allow WITH (for CTEs) and VALUES
	allowedPrefixes := []string{"select", "show", "describe", "explain", "desc", "with", "values"}
	isAllowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(lowerQuery, prefix) {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return fmt.Errorf("only SELECT, SHOW, DESCRIBE, EXPLAIN, WITH, and VALUES statements are allowed for monitoring queries")
	}

	return nil
}

func (p *PostgresExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := p.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*PostgresConfig)

	startTime := time.Now().UTC()

	// Validate configuration before execution, and before connecting: a
	// query that isn't allowed is Down even when nothing listens.
	if err := ValidateConnectionString(cfg.DatabaseConnectionString, []string{"postgres", "postgresql"}); err != nil {
		return DownResult(fmt.Errorf("connection string validation failed: %w", err), startTime, time.Now().UTC())
	}

	query := cfg.DatabaseQuery
	if strings.TrimSpace(query) == "" {
		query = "SELECT 1"
	} else if err := p.validateQuery(query); err != nil {
		return DownResult(fmt.Errorf("query validation failed: %w", err), startTime, time.Now().UTC())
	}

	connector, err := newPostgresConnector(cfg.DatabaseConnectionString)
	if err != nil {
		return DownResult(fmt.Errorf("failed to parse connection string: %w", err), startTime, time.Now().UTC())
	}

	db := sql.OpenDB(connector)
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return DownResult(fmt.Errorf("connection failed: %w", err), startTime, time.Now().UTC())
	}

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

// newPostgresConnector builds the connector for a postgres:// connection
// string. pgdriver reads the string itself, so it maps sslmode to a TLS
// configuration, whatever its case: allow, prefer and require encrypt without
// checking the certificate (allow and prefer don't fall back to plain text, as
// libpq's do: a server without TLS is Down), and verify-ca and verify-full check
// it against the system's roots.
//
// Only the user, password, host, port, database and sslmode reach pgdriver. Any
// other query parameter is ignored, as it always has been here: pgdriver sends
// the ones it doesn't know to the server as SET <name> TO <value>, which would
// run statements the query validator never saw and turn a stray ?foo=bar into
// Down. That also drops sslrootcert, which names a file on the Peekaping server
// rather than on the user's machine, and host, which pgdriver reads as a Unix
// socket when it is a path. A missing or empty sslmode means no TLS, as it
// always has here; pgdriver's own default would be TLS without checking the
// certificate.
func newPostgresConnector(connectionString string) (connector *pgdriver.Connector, err error) {
	u, err := url.Parse(connectionString)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string format: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("invalid scheme: %s", u.Scheme)
	}
	if u.User == nil {
		return nil, fmt.Errorf("password is undefined")
	}
	if password, _ := u.User.Password(); password == "" {
		return nil, fmt.Errorf("password is undefined")
	}

	port := u.Port()
	if port == "" {
		port = "5432"
	}
	addr := net.JoinHostPort(u.Hostname(), port)
	// pgdriver is case sensitive about sslmode, and this check wasn't: anything
	// but exactly "disable" connected over TLS.
	sslMode := strings.ToLower(u.Query().Get("sslmode"))
	// The port is always in the host: pgdriver takes the name it verifies the
	// certificate against from it, and keeps an IPv6 host's brackets without one.
	dsn := url.URL{Scheme: u.Scheme, User: u.User, Host: addr, Path: u.Path}
	if sslMode != "" {
		dsn.RawQuery = url.Values{"sslmode": {sslMode}}.Encode()
	}

	// pgdriver panics on a string it can't use, such as an unknown sslmode.
	defer func() {
		if r := recover(); r != nil {
			connector, err = nil, fmt.Errorf("%v", r)
		}
	}()
	options := []pgdriver.Option{pgdriver.WithDSN(dsn.String())}
	if sslMode == "" {
		options = append(options, pgdriver.WithInsecure(true))
	}
	return pgdriver.NewConnector(options...), nil
}
