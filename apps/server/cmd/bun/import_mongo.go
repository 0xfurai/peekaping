package main

import (
	"context"
	"fmt"
	"os"
	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/mongoimport"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v2"
)

// newImportMongoCommand is "bun db import-mongo". It copies a MongoDB install
// into the SQLite or PostgreSQL database that DB_TYPE and the other DB_*
// variables name, which must hold no data of its own.
func newImportMongoCommand() *cli.Command {
	return &cli.Command{
		Name:  "import-mongo",
		Usage: "copy a MongoDB install's data into a SQLite or PostgreSQL database that holds none of its own",
		Description: "Reads the MongoDB database named by --mongo-uri and --mongo-db, applies the SQL migrations to the\n" +
			"database that DB_TYPE, DB_NAME and the other DB_* variables name, and copies everything but the login\n" +
			"lockout counters into it in one transaction. The target must hold no users, monitors or history: if it\n" +
			"does, nothing is changed. A database that Peekaping only started on once is fine. It holds just the\n" +
			"default settings that the first start writes, and the copy replaces them with the install's settings,\n" +
			"the keys that sign login tokens included. Stop the MongoDB install first, so that nothing is written\n" +
			"while it is copied.",
		Flags: []cli.Flag{
			lockTimeoutFlag(),
			&cli.StringFlag{
				Name:     "mongo-uri",
				Usage:    "connection string of the MongoDB server, such as mongodb://user:password@host:27017/?authSource=admin",
				EnvVars:  []string{"MONGO_URI"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "mongo-db",
				Usage:   "name of the MongoDB database the install uses (its DB_NAME); defaults to the database in the URI, then to " + mongoimport.DefaultDatabase,
				EnvVars: []string{"MONGO_DB"},
			},
		},
		Action: func(c *cli.Context) error {
			wait, err := lockTimeout(c)
			if err != nil {
				return err
			}
			return runWithConfiguredDB(requireSQLTarget, func(cfg config.DBConfig, db *bun.DB) error {
				report, err := mongoimport.Run(c.Context, db, mongoimport.Options{
					MongoURI:      c.String("mongo-uri"),
					MongoDatabase: c.String("mongo-db"),
					Progress:      os.Stderr,
					// The same migrate as "bun db migrate", with its lock
					Migrate: func(ctx context.Context, db *bun.DB) error {
						if err := migrate.NewMigrator(db, migrations.Migrations).Init(ctx); err != nil {
							return err
						}
						return migrateUp(ctx, db, wait)
					},
				})
				if err != nil {
					return err
				}
				report.Write(os.Stdout)
				copied := 0
				for _, collection := range report.Collections {
					copied += collection.Copied
				}
				fmt.Printf("\nCopied %d rows into the %s database. The login lockout counters (login_state) are not copied. Users sign in again once.\n", copied, cfg.DBType)
				return nil
			})
		},
	}
}

// requireSQLTarget refuses a DB_TYPE the import can't write to. DB_TYPE=mongo is a valid
// setting everywhere else, and here it would name the install being moved, not the target.
func requireSQLTarget(cfg config.DBConfig) error {
	switch cfg.DBType {
	case "sqlite", "postgres", "postgresql":
		return nil
	}
	return fmt.Errorf("DB_TYPE=%s: import-mongo copies into SQLite or PostgreSQL, so set DB_TYPE to sqlite or postgres (and DB_NAME and the other DB_* variables) for the new database", cfg.DBType)
}
