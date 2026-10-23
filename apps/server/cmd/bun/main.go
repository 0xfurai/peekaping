package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/extra/bundebug"
	"github.com/uptrace/bun/migrate"

	"github.com/urfave/cli/v2"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	app := &cli.App{
		Name: "bun",
		Commands: []*cli.Command{
			newDBCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func newDBCommand() *cli.Command {
	return &cli.Command{
		Name:  "db",
		Usage: "database migrations",
		Subcommands: []*cli.Command{
			{
				Name:  "init",
				Usage: "create migration tables",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						return migrator.Init(c.Context)
					})
				},
			},
			{
				Name:  "migrate",
				Usage: "migrate database",
				Description: "On PostgreSQL, migrate holds a lock while it runs, and a second migrate waits for it to finish, " +
					"up to --lock-timeout (default 10m). A migrate that is killed loses the lock with its connection.",
				Flags: []cli.Flag{lockTimeoutFlag()},
				Action: func(c *cli.Context) error {
					wait, err := lockTimeout(c)
					if err != nil {
						return err
					}
					return runWithDB(func(db *bun.DB) error {
						return migrateUp(c.Context, db, wait)
					})
				},
			},
			{
				Name:        "rollback",
				Usage:       "rollback the last migration group",
				Description: "Takes the same lock as migrate and waits for it the same way.",
				Flags:       []cli.Flag{lockTimeoutFlag()},
				Action: func(c *cli.Context) error {
					wait, err := lockTimeout(c)
					if err != nil {
						return err
					}
					return runWithDB(func(db *bun.DB) error {
						return migrateDown(c.Context, db, wait)
					})
				},
			},
			{
				Name:  "lock",
				Usage: "lock migrations",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						return migrator.Lock(c.Context)
					})
				},
			},
			{
				Name:  "unlock",
				Usage: "unlock migrations",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						return migrator.Unlock(c.Context)
					})
				},
			},
			{
				Name:  "create_go",
				Usage: "create Go migration",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						name := strings.Join(c.Args().Slice(), "_")
						mf, err := migrator.CreateGoMigration(c.Context, name)
						if err != nil {
							return err
						}
						fmt.Printf("created migration %s (%s)\n", mf.Name, mf.Path)
						return nil
					})
				},
			},
			{
				Name:  "create_sql",
				Usage: "create up and down SQL migrations",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						name := strings.Join(c.Args().Slice(), "_")
						files, err := migrator.CreateSQLMigrations(c.Context, name)
						if err != nil {
							return err
						}

						for _, mf := range files {
							fmt.Printf("created migration %s (%s)\n", mf.Name, mf.Path)
						}

						return nil
					})
				},
			},
			{
				Name:  "create_tx_sql",
				Usage: "create up and down transactional SQL migrations",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						name := strings.Join(c.Args().Slice(), "_")
						files, err := migrator.CreateTxSQLMigrations(c.Context, name)
						if err != nil {
							return err
						}

						for _, mf := range files {
							fmt.Printf("created transaction migration %s (%s)\n", mf.Name, mf.Path)
						}

						return nil
					})
				},
			},
			{
				Name:  "status",
				Usage: "print migrations status",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						ms, err := migrator.MigrationsWithStatus(c.Context)
						if err != nil {
							return err
						}
						fmt.Printf("migrations: %s\n", ms)
						fmt.Printf("unapplied migrations: %s\n", ms.Unapplied())
						fmt.Printf("last migration group: %s\n", ms.LastGroup())
						return nil
					})
				},
			},
			newImportMongoCommand(),
			{
				Name:  "mark_applied",
				Usage: "mark migrations as applied without actually running them",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						group, err := migrator.Migrate(c.Context, migrate.WithNopMigration())
						if err != nil {
							return err
						}
						if group.IsZero() {
							fmt.Printf("there are no new migrations to mark as applied\n")
							return nil
						}
						fmt.Printf("marked as applied %s\n", group)
						return nil
					})
				},
			},
		},
	}
}

// loadDBConfig reads the DB_* settings from the environment, and the .env file two levels up.
func loadDBConfig() (config.DBConfig, error) {
	cfg, err := config.LoadConfig[config.DBConfig]("../..")
	if err != nil {
		return cfg, fmt.Errorf("failed to load config: %w", err)
	}

	err = config.ValidateDatabaseCustomRules(&cfg)
	if err != nil {
		return cfg, fmt.Errorf("failed to validate database custom rules: %w", err)
	}
	return cfg, nil
}

func runWithDB(fn func(*bun.DB) error) error {
	return runWithConfiguredDB(nil, func(_ config.DBConfig, db *bun.DB) error { return fn(db) })
}

// runWithConfiguredDB is runWithDB for a command that needs the settings too. It calls
// accept, if there is one, with the settings before it connects, so that a command can
// refuse a database type it doesn't work with.
func runWithConfiguredDB(accept func(config.DBConfig) error, fn func(config.DBConfig, *bun.DB) error) error {
	cfg, err := loadDBConfig()
	if err != nil {
		return err
	}
	if accept != nil {
		if err := accept(cfg); err != nil {
			return err
		}
	}

	// Connect to database
	db, err := connectToDatabase(&cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Add query hook for debugging
	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithEnabled(false),
		bundebug.FromEnv(),
	))

	return fn(cfg, db)
}

func connectToDatabase(cfg *config.DBConfig) (*bun.DB, error) {
	var sqldb *sql.DB
	var db *bun.DB
	var err error

	switch cfg.DBType {
	case "postgres", "postgresql":
		sslMode := cfg.DBSSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&search_path=public",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName, sslMode)
		sqldb = sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
		db = bun.NewDB(sqldb, pgdialect.New())

	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)
		sqldb, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
		}
		db = bun.NewDB(sqldb, mysqldialect.New())

	case "sqlite":
		dbPath := cfg.DBName
		if dbPath == "" {
			dbPath = "./data.db"
		}
		// Configure SQLite for concurrent access
		// Use same settings as main application for consistency
		sqldb, err = sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?cache=shared&mode=rwc", dbPath))
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
		}

		// Set connection pool limits for SQLite
		// Using same conservative limits as main app to prevent lock contention
		sqldb.SetMaxOpenConns(1)
		sqldb.SetMaxIdleConns(1)
		sqldb.SetConnMaxLifetime(0)

		db = bun.NewDB(sqldb, sqlitedialect.New())

		// Configure SQLite PRAGMA settings for corruption prevention
		// Must match main application settings for consistency
		db.Exec("PRAGMA busy_timeout=5000")
		db.Exec("PRAGMA journal_mode=WAL")
		db.Exec("PRAGMA synchronous=NORMAL")
		db.Exec("PRAGMA foreign_keys=ON")
		db.Exec("PRAGMA temp_store=MEMORY")
		db.Exec("PRAGMA wal_autocheckpoint=1000")
		db.Exec("PRAGMA cache_size=-64000")
		db.Exec("PRAGMA auto_vacuum=INCREMENTAL")

		fmt.Printf("Connected to SQLite database: %s (WAL mode enabled)\n", dbPath)

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DBType)
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
