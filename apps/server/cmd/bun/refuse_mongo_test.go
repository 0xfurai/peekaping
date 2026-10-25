package main

import (
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// AC1: "bun db migrate" and the other commands that open the database refuse DB_TYPE=mongo, which
// names MongoDB storage that was removed. They stop before they connect to anything.
func TestCommandsRefuseMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB"} {
		for _, command := range []string{"init", "migrate", "rollback", "status"} {
			t.Run(dbType+" "+command, func(t *testing.T) {
				t.Setenv("DB_TYPE", dbType)
				t.Setenv("DB_NAME", "peekaping")
				t.Setenv("DB_HOST", "localhost")
				t.Setenv("DB_PORT", "27017")
				t.Setenv("DB_USER", "peekaping")
				t.Setenv("DB_PASS", "password")
				app := &cli.App{Name: "bun", Commands: []*cli.Command{newDBCommand()}}

				err := app.Run([]string{"bun", "db", command})

				require.ErrorIs(t, err, config.ErrMongoStorageRemoved)
				assert.ErrorContains(t, err, "DB_TYPE="+dbType+": MongoDB storage was removed.")
				assert.ErrorContains(t, err, "https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql")
			})
		}
	}
}
