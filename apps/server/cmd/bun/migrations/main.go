package migrations

import (
	"embed"

	"github.com/uptrace/bun/migrate"
)

//go:embed *.sql
var sqlMigrations embed.FS

var Migrations = migrate.NewMigrations(
	migrate.WithMigrationsDirectory("cmd/bun/migrations"),
)

func init() {
	if err := Migrations.Discover(sqlMigrations); err != nil {
		panic(err)
	}
}
