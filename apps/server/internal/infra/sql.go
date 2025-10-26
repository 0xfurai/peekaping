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

		// Configure SQLite for concurrent access
		// cache=shared: Share cache between connections
		// mode=rwc: Read-write-create mode
		sqldb, err = sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?cache=shared&mode=rwc", dbPath))
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
		}

		// Set connection pool limits for SQLite to prevent lock contention
		// SQLite works best with a limited number of connections
		// For multiple processes, use very conservative limits
		sqldb.SetMaxOpenConns(1)    // Force serialized access
		sqldb.SetMaxIdleConns(1)    // Keep one connection alive
		sqldb.SetConnMaxLifetime(0) // Connections never expire (important for WAL mode)

		db = bun.NewDB(sqldb, sqlitedialect.New())

		// Configure SQLite using PRAGMA statements for corruption prevention
		// Set busy_timeout FIRST so subsequent PRAGMA statements can wait for locks
		if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
			logger.Warnf("Failed to set busy_timeout (non-fatal): %v", err)
		}

		// Check current journal mode before trying to change it
		var currentJournalMode string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&currentJournalMode); err != nil {
			logger.Warnf("Failed to check journal_mode (non-fatal): %v", err)
		}

		// Only set WAL mode if it's not already enabled
		if currentJournalMode != "wal" {
			if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
				// If it fails, log warning but don't fail - database might already be in WAL mode
				logger.Warnf("Failed to set journal_mode to WAL (non-fatal, current mode: %s): %v", currentJournalMode, err)
			} else {
				logger.Infof("SQLite journal mode set to WAL")
			}
		} else {
			logger.Infof("SQLite already in WAL mode")
		}

		// Enable synchronous mode for better reliability in multi-process scenarios
		// NORMAL is a good balance between performance and safety
		// For critical data, consider FULL, but it's slower
		if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
			logger.Warnf("Failed to set synchronous mode (non-fatal): %v", err)
		}

		// Enable foreign key constraints (important for data integrity)
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			logger.Warnf("Failed to enable foreign keys (non-fatal): %v", err)
		}

		// Use memory for temporary tables to reduce disk I/O and corruption risk
		if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
			logger.Warnf("Failed to set temp_store (non-fatal): %v", err)
		}

		// Control WAL automatic checkpointing (default is 1000 pages)
		// This prevents the WAL file from growing too large
		if _, err := db.Exec("PRAGMA wal_autocheckpoint=1000"); err != nil {
			logger.Warnf("Failed to set wal_autocheckpoint (non-fatal): %v", err)
		}

		// Set cache size (negative value means KB, positive means pages)
		// -64000 = 64MB cache (good for most applications)
		if _, err := db.Exec("PRAGMA cache_size=-64000"); err != nil {
			logger.Warnf("Failed to set cache_size (non-fatal): %v", err)
		}

		// Enable auto_vacuum to prevent database file fragmentation
		// INCREMENTAL allows gradual cleanup
		if _, err := db.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			logger.Warnf("Failed to set auto_vacuum (non-fatal): %v", err)
		}

		// Run integrity check on startup to detect existing corruption
		var integrityResult string
		if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrityResult); err != nil {
			logger.Warnf("Failed to run integrity check (non-fatal): %v", err)
		} else if integrityResult != "ok" {
			logger.Errorf("Database integrity check FAILED: %s - database may be corrupted!", integrityResult)
			logger.Error("Consider restoring from backup or running 'PRAGMA integrity_check' manually")
		} else {
			logger.Info("Database integrity check passed")
		}

		logger.Infof("Connecting to SQLite database: %s (WAL mode enabled, corruption prevention active)", dbPath)

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

// GracefulSQLiteShutdown performs a graceful shutdown of SQLite database
// This checkpoints the WAL file and ensures data integrity
func GracefulSQLiteShutdown(db *bun.DB, dbType string, logger *zap.SugaredLogger) error {
	if dbType != "sqlite" {
		return nil
	}

	logger.Info("Performing graceful SQLite shutdown...")

	// Checkpoint the WAL file to ensure all changes are written to the main database
	// TRUNCATE mode checkpoints and truncates the WAL file
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		logger.Warnf("Failed to checkpoint WAL (non-fatal): %v", err)
	} else {
		logger.Info("WAL checkpoint completed successfully")
	}

	// Perform incremental vacuum to clean up fragmented space
	if _, err := db.Exec("PRAGMA incremental_vacuum"); err != nil {
		logger.Warnf("Failed to perform incremental vacuum (non-fatal): %v", err)
	}

	// Run integrity check before shutdown
	var integrityResult string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrityResult); err != nil {
		logger.Warnf("Failed to run shutdown integrity check (non-fatal): %v", err)
	} else if integrityResult != "ok" {
		logger.Errorf("Shutdown integrity check FAILED: %s", integrityResult)
	} else {
		logger.Info("Shutdown integrity check passed")
	}

	// Close the database connection
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	logger.Info("SQLite database closed gracefully")
	return nil
}
