package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"peekaping/internal/config"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/extra/bundebug"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/dig"
	"go.uber.org/zap"

	_ "github.com/go-sql-driver/mysql"
)

const sqliteRecoveryDocsURL = "https://docs.peekaping.com/self-hosting/sqlite-recovery"

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

		// Run a quick check on startup to detect existing corruption. The full integrity_check
		// reads the whole file: about 30s for 1.5 GB (24h of 1,000 monitors at 20s), and it grows
		// with the heartbeat history. quick_check walks the b-tree pages in about 2s. It misses an
		// index that disagrees with its table, so the full check stays a manual one.
		if err := checkSQLiteOnStartup(db, dbPath, logger); err != nil {
			_ = db.Close()
			return nil, err
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

// checkSQLiteOnStartup runs PRAGMA quick_check and returns an error when SQLite reports anything
// but a single "ok", so startup stops before a process writes to a damaged database.
func checkSQLiteOnStartup(db *bun.DB, dbPath string, logger *zap.SugaredLogger) error {
	lines, err := readSQLiteQuickCheck(db)
	healthy := len(lines) == 1 && lines[0] == "ok"
	if healthy || len(lines) == 0 {
		if err == nil && healthy {
			return nil
		}
		if err == nil {
			err = errors.New("it returned no result")
		}
		// Not a finding, so not confirmed corruption: the database may only be locked
		logger.Warnf("Failed to run the SQLite quick check (non-fatal): %v", err)
		return nil
	}

	return fmt.Errorf("SQLite database %s is corrupt: %s. See %s", dbPath, lines[0], sqliteRecoveryDocsURL)
}

// readSQLiteQuickCheck returns every row of PRAGMA quick_check: SQLite reports up to 100 findings,
// one per row. A corrupt b-tree page ends the statement with SQLITE_CORRUPT after its findings, so
// the rows read before an error are returned with it.
func readSQLiteQuickCheck(db *bun.DB) ([]string, error) {
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return lines, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
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

	// No integrity check here: it reads the whole file, about 30s for 1.5 GB (24h of 1,000
	// monitors at 20s), which outlasts the shutdown deadline. It couldn't repair anything, and
	// the next start checks again.

	// Close the database connection
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	logger.Info("SQLite database closed gracefully")
	return nil
}

// GracefulDatabaseShutdown performs graceful shutdown for any database type
// It uses the DI container to get the appropriate database connection
func GracefulDatabaseShutdown(container *dig.Container, cfg *config.Config, logger *zap.SugaredLogger) error {
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		// For SQL databases, perform graceful shutdown
		return container.Invoke(func(db *bun.DB) {
			if err := GracefulSQLiteShutdown(db, cfg.DBType, logger); err != nil {
				logger.Errorw("Failed to gracefully shutdown database", "error", err)
			}
		})
	case "mongo", "mongodb":
		// For MongoDB, disconnect the client
		return container.Invoke(func(client *mongo.Client) {
			if err := client.Disconnect(context.Background()); err != nil {
				logger.Errorw("Failed to disconnect MongoDB client", "error", err)
			} else {
				logger.Info("MongoDB client disconnected gracefully")
			}
		})
	default:
		logger.Warnf("Unknown database type for shutdown: %s", cfg.DBType)
		return nil
	}
}
