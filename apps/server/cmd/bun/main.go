package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"peekaping/cmd/bun/migrations"
	"peekaping/src/config"
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
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						if err := migrator.Lock(c.Context); err != nil {
							return err
						}
						defer migrator.Unlock(c.Context) // nolint:errcheck

						group, err := migrator.Migrate(c.Context)
						if err != nil {
							migrator.Rollback(c.Context)
							return err
						}
						if group.IsZero() {
							fmt.Printf("there are no new migrations to run (database is up to date)\n")
							return nil
						}
						fmt.Printf("migrated to %s\n", group)
						return nil
					})
				},
			},
			{
				Name:  "rollback",
				Usage: "rollback the last migration group",
				Action: func(c *cli.Context) error {
					return runWithDB(func(db *bun.DB) error {
						migrator := migrate.NewMigrator(db, migrations.Migrations)
						if err := migrator.Lock(c.Context); err != nil {
							return err
						}
						defer migrator.Unlock(c.Context) //nolint:errcheck

						group, err := migrator.Rollback(c.Context)
						if err != nil {
							return err
						}
						if group.IsZero() {
							fmt.Printf("there are no groups to roll back\n")
							return nil
						}
						fmt.Printf("rolled back %s\n", group)
						return nil
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

func runWithDB(fn func(*bun.DB) error) error {
	// Load configuration
	cfg, err := config.LoadConfig("../..")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
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

	return fn(db)
}

func connectToDatabase(cfg *config.Config) (*bun.DB, error) {
	var sqldb *sql.DB
	var db *bun.DB
	var err error

	switch cfg.DBType {
	case "postgres", "postgresql":
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&search_path=public",
			cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)
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
		sqldb, err = sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?cache=shared&mode=rwc", dbPath))
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
		}
		db = bun.NewDB(sqldb, sqlitedialect.New())

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DBType)
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
