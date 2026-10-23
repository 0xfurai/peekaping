package shutdown

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// Database is the database a cluster binary stores in, as its container provides it: the SQL
// database for SQLite and PostgreSQL, or the MongoDB client for DB_TYPE=mongo, which cluster mode
// still supports (ADR 0001). A binary lists it as a field of its dig.In struct and closes it
// last, once nothing uses it any more. The worker provides neither, and closes nothing.
type Database struct {
	dig.In

	SQL   *bun.DB       `optional:"true"`
	Mongo *mongo.Client `optional:"true"`
}

// Close closes the database. A SQL database closes as CloseDatabase does for dbType, and a
// MongoDB client disconnects within ctx, which bounds its wait for the connections in use.
func (d Database) Close(ctx context.Context, dbType string, logger *zap.SugaredLogger) error {
	switch {
	case d.SQL != nil:
		return CloseDatabase(d.SQL, dbType, logger)
	case d.Mongo != nil:
		if err := d.Mongo.Disconnect(ctx); err != nil {
			return fmt.Errorf("failed to disconnect the MongoDB client: %w", err)
		}
		logger.Info("MongoDB client disconnected gracefully")
	}
	return nil
}
