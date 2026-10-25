package shutdown

import (
	"context"

	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// Database is the database a cluster binary stores in, as its container provides it: the SQL
// database for SQLite and PostgreSQL. A binary lists it as a field of its dig.In struct and closes
// it last, once nothing uses it any more. The worker provides none, and closes nothing.
type Database struct {
	dig.In

	SQL *bun.DB `optional:"true"`
}

// Close closes the SQL database as CloseDatabase does for dbType. It closes at once, so it takes
// no time from ctx, which is there for the stop sequence that passes it.
func (d Database) Close(_ context.Context, dbType string, logger *zap.SugaredLogger) error {
	if d.SQL != nil {
		return CloseDatabase(d.SQL, dbType, logger)
	}
	return nil
}
