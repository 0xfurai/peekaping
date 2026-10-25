// Package shutdown holds the tools the server binaries stop with: the time their steps are given,
// the count of the HTTP handlers still running, and the close of the database that comes last.
// cmd/peekaping and cmd/api share them so that both stop the same way (ADR 0002). The signal
// handling that starts a shutdown and the wait that keeps a step within its time are in package
// lifecycle, which has no storage or queue behind it and which the remote probe uses too.
package shutdown

import (
	"fmt"
	"time"

	"peekaping/internal/infra"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Timeouts bound the steps of a shutdown. Each step has a share of its own, so that a step
// that runs late doesn't take the time of the steps after it: the background jobs get theirs
// however long the HTTP server took.
type Timeouts struct {
	// HTTP is how long the HTTP server waits for the requests in flight
	HTTP time.Duration
	// Jobs is how long the background jobs have to stop, and the HTTP handlers still running
	// once HTTP is up have to return
	Jobs time.Duration
	// EventBus is how long the event bus has to handle the events queued
	EventBus time.Duration
}

// Total is the longest the shutdown takes, from the signal
func (t Timeouts) Total() time.Duration { return t.HTTP + t.Jobs + t.EventBus }

// CloseDatabase closes the database. SQLite checkpoints its write-ahead log first.
func CloseDatabase(db *bun.DB, dbType string, logger *zap.SugaredLogger) error {
	if dbType == "sqlite" {
		return infra.GracefulSQLiteShutdown(db, dbType, logger)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}
