package infra

import (
	"database/sql"
	"errors"
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/sqlitedb"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bundebug"
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
		sslMode := cfg.DBSSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName, sslMode)

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

		sqldb, err = sqlitedb.Open(dbPath)
		if err != nil {
			return nil, err
		}

		db = bun.NewDB(sqldb, sqlitedialect.New())

		// The settings of a connection are in its DSN (see sqlitedb.DSN). journal_mode and
		// auto_vacuum belong to the database file and are set once, in the order they always had.
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

		// Enable auto_vacuum to prevent database file fragmentation
		// INCREMENTAL allows gradual cleanup
		if _, err := db.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			logger.Warnf("Failed to set auto_vacuum (non-fatal): %v", err)
		}

		// Check on startup to detect existing corruption, as SQLITE_STARTUP_CHECK says. The default,
		// quick, runs quick_check. The full integrity_check reads the whole file: about 30s for
		// 1.5 GB (24h of 1,000 monitors at 20s), and it grows with the heartbeat history.
		// quick_check walks the b-tree pages in about 2s. It misses an index that disagrees with
		// its table, so the full check is one to ask for with "full" or to run by hand (see the
		// recovery page at sqliteRecoveryDocsURL). "off" runs none.
		if err := checkSQLiteOnStartup(db, dbPath, cfg.SQLiteStartupCheck, logger); err != nil {
			// Don't close db: closing SQLite's last connection checkpoints the write-ahead log
			// into the main file and deletes the -wal and -shm files, which the operator is told
			// to keep. The process exits right after this error.
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

// checkSQLiteOnStartup runs the check the mode names and returns an error when SQLite reports
// anything but a single "ok", so startup stops before a process writes to a damaged database.
// "quick" runs PRAGMA quick_check, "full" runs PRAGMA integrity_check, and "off" runs nothing.
// An empty mode is quick, for a config built without one.
func checkSQLiteOnStartup(db *bun.DB, dbPath, mode string, logger *zap.SugaredLogger) error {
	var name, pragma string
	switch mode {
	case "", "quick":
		name, pragma = "quick", "quick_check"
	case "full":
		name, pragma = "full", "integrity_check"
	case "off":
		logger.Info("SQLite startup check skipped: SQLITE_STARTUP_CHECK is off")
		return nil
	default:
		return fmt.Errorf("unsupported SQLITE_STARTUP_CHECK %q. Supported values: quick, full, off", mode)
	}

	lines, err := readSQLiteCheck(db, pragma)
	healthy := len(lines) == 1 && lines[0] == "ok"
	if healthy || len(lines) == 0 {
		if err == nil && healthy {
			logger.Infof("SQLite %s check passed", name)
			return nil
		}
		if err == nil {
			err = errors.New("it returned no result")
		}
		// No finding rows: the database may only be locked, or a damaged header or schema page may
		// fail the query itself. Startup goes on as agreed; its next queries fail on their own.
		logger.Warnf("Failed to run the SQLite %s check (non-fatal): %v", name, err)
		return nil
	}

	logger.Errorf("SQLite %s check found corruption in %s. Peekaping stops here and doesn't change the file.", name, dbPath)
	for _, line := range lines {
		logger.Errorf("%s: %s", pragma, line)
	}
	logger.Errorf("Stop every Peekaping process that uses this database, and its container's restarts. "+
		"Keep the original database file together with its -wal and -shm files; don't repair it in place. "+
		"Restore a tested backup. Without one, salvage the data into a new database with sqlite3 .recover "+
		"and run PRAGMA integrity_check on the result; recovered data can be missing, altered or resurrected. "+
		"See %s", sqliteRecoveryDocsURL)

	// SQLite's first row can be two lines: "*** in database main ***" and the finding
	first := strings.ReplaceAll(lines[0], "\n", " ")
	return fmt.Errorf("SQLite database %s is corrupt: %s. See %s", dbPath, first, sqliteRecoveryDocsURL)
}

// readSQLiteCheck returns every row of PRAGMA quick_check or PRAGMA integrity_check (pragma names
// the one): SQLite reports up to 100 findings, one per row. A corrupt b-tree page ends the
// statement with SQLITE_CORRUPT after its findings, so the rows read before an error are returned
// with it.
func readSQLiteCheck(db *bun.DB, pragma string) ([]string, error) {
	rows, err := db.Query("PRAGMA " + pragma)
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
