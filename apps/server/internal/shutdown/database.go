package shutdown

import (
	"context"

	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// Database is the database the api stores in, as its container provides it. The api lists it as a
// field of its dig.In struct and closes it last, once nothing uses it any more.
type Database struct {
	dig.In

	SQL *bun.DB
}

// Close closes the SQL database as CloseDatabase does for dbType. It closes at once, so it takes
// no time from ctx, which is there for the stop sequence that passes it.
func (d Database) Close(_ context.Context, dbType string, logger *zap.SugaredLogger) error {
	return CloseDatabase(d.SQL, dbType, logger)
}
