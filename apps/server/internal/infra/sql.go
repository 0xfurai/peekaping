package infra

import (
	"database/sql"
	"fmt"
	"peekaping/internal/config"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/extra/bundebug"
	"go.uber.org/zap"

	_ "github.com/go-sql-driver/mysql"
)

func ProvideSQLDB(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*bun.DB, error) {
	var sqldb *sql.DB
	var db *bun.DB
	var err error

	switch cfg.DBType {
	case "postgres", "postgresql":
		// PostgreSQL connection
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)

		sqldb = sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
		db = bun.NewDB(sqldb, pgdialect.New())

		logger.Infof("Connecting to PostgreSQL database: %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	case "mysql":
		// MySQL connection
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)

		sqldb, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
		}

		db = bun.NewDB(sqldb, mysqldialect.New())

		logger.Infof("Connecting to MySQL database: %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	case "sqlite":
		// SQLite connection
		dbPath := cfg.DBName
		if dbPath == "" {
			dbPath = "./data.db" // Default SQLite file path
		}

		// Configure SQLite for concurrent access with WAL mode
		// _journal_mode=WAL: Enable Write-Ahead Logging for better concurrency
		// _busy_timeout=5000: Wait up to 5 seconds when database is locked
		// cache=shared: Share cache between connections
		// mode=rwc: Read-write-create mode
		sqldb, err = sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000", dbPath))
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
		}

		// Set connection pool limits for SQLite to prevent lock contention
		// SQLite works best with a limited number of connections
		sqldb.SetMaxOpenConns(10)   // Maximum open connections
		sqldb.SetMaxIdleConns(5)    // Maximum idle connections
		sqldb.SetConnMaxLifetime(0) // Connections never expire (important for WAL mode)

		db = bun.NewDB(sqldb, sqlitedialect.New())

		logger.Infof("Connecting to SQLite database: %s (WAL mode enabled)", dbPath)

	default:
		return nil, fmt.Errorf("unsupported database type: %s. Supported types: postgres, mysql, sqlite", cfg.DBType)
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.FromEnv(),
	))

	logger.Info("Successfully connected to SQL database")
	return db, nil
}
