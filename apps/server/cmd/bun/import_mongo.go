package main

import (
	"fmt"
	"os"
	"peekaping/internal/mongoimport"

	"github.com/uptrace/bun"
	"github.com/urfave/cli/v2"
)

// newImportMongoCommand is "bun db import-mongo". It copies a MongoDB install
// into the SQLite or PostgreSQL database that DB_TYPE and the other DB_*
// variables name, which must be empty.
func newImportMongoCommand() *cli.Command {
	return &cli.Command{
		Name:  "import-mongo",
		Usage: "copy a MongoDB install's data into an empty SQLite or PostgreSQL database",
		Description: "Reads the MongoDB database named by --mongo-uri and --mongo-db, applies the SQL migrations to the\n" +
			"database that DB_TYPE, DB_NAME and the other DB_* variables name, and copies everything but the login\n" +
			"lockout counters into it in one transaction. The target must be a database that no Peekaping has used:\n" +
			"if it holds any data, nothing is changed. Stop the MongoDB install first, so that nothing is written\n" +
			"while it is copied.",
		Flags: []cli.Flag{
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
			// DB_TYPE=mongo is a valid setting everywhere else; here it would name the source
			cfg, err := loadDBConfig()
			if err != nil {
				return err
			}
			switch cfg.DBType {
			case "sqlite", "postgres", "postgresql":
			default:
				return fmt.Errorf("DB_TYPE=%s: import-mongo copies into SQLite or PostgreSQL, so set DB_TYPE to sqlite or postgres (and DB_NAME and the other DB_* variables) for the new database", cfg.DBType)
			}

			return runWithDB(func(db *bun.DB) error {
				report, err := mongoimport.Run(c.Context, db, mongoimport.Options{
					MongoURI:      c.String("mongo-uri"),
					MongoDatabase: c.String("mongo-db"),
					Progress:      os.Stderr,
				})
				if err != nil {
					return err
				}
				report.Write(os.Stdout)
				copied := 0
				for _, collection := range report.Collections {
					copied += collection.Copied
				}
				fmt.Printf("\nCopied %d rows into the %s database. Start Peekaping with DB_TYPE=%s; users sign in again once.\n", copied, cfg.DBType, cfg.DBType)
				return nil
			})
		},
	}
}
