package app

import (
	"fmt"

	"peekaping/internal/config"
)

// UpgradeToProbesURL is the page that tells an install how to move from the producer, worker and
// ingester to probes. The page is published with the release that retired them.
const UpgradeToProbesURL = "https://docs.peekaping.com/self-hosting/upgrading-to-probes"

// RefuseOnSQLStorage returns the error a producer or an ingester stops with when DB_TYPE names
// SQLite or PostgreSQL, and nil for any other storage, MongoDB included.
//
// On SQLite and PostgreSQL the probes run the checks and the api does the rest: it schedules
// nothing, stores what the probes send, and runs the stale-monitor watchdog and the sweep that sets
// silent probes offline. A producer or an ingester left running there would fight the api for the
// leader lock, or store nothing. MongoDB has no probes (ADR 0001), so its producer, worker and
// ingester run as they always did, until MongoDB storage is removed. takenOverBy says what does the
// service's job now.
func RefuseOnSQLStorage(service, dbType, takenOverBy string) error {
	if !config.IsSQLDBType(dbType) {
		return nil
	}
	return fmt.Errorf("the %s no longer runs on SQLite or PostgreSQL storage (DB_TYPE=%s): %s. "+
		"Remove the %s from this install and run peekaping-probe instead; the steps are at %s "+
		"(the page is published with this release). The %s still runs on MongoDB storage",
		service, dbType, takenOverBy, service, UpgradeToProbesURL, service)
}

// WorkerHasNoWorkNotice is what the worker logs at startup about its job. On SQLite and PostgreSQL
// storage it has none left: the probes run the checks. The worker has no database and reads no
// DB_TYPE of its own, so without one it can't tell and says what is true of both. ok is false for
// MongoDB storage, where the worker runs the checks as it always did and has nothing to say.
func WorkerHasNoWorkNotice(dbType string) (message string, ok bool) {
	switch {
	case config.IsMongoDBType(dbType):
		return "", false
	case config.IsSQLDBType(dbType):
		return fmt.Sprintf("The worker has no work left on SQLite or PostgreSQL storage (DB_TYPE=%s): peekaping-probe instances run the checks, "+
			"and nothing enqueues any for this worker. Remove it from this install; the steps are at %s (the page is published with this release)",
			dbType, UpgradeToProbesURL), true
	default:
		return fmt.Sprintf("The worker has work only on MongoDB storage. If this install keeps its data in SQLite or PostgreSQL, it has no work left: "+
			"peekaping-probe instances run the checks, and nothing enqueues any for this worker. Remove it from this install; the steps are at %s (the page is published with this release)",
			UpgradeToProbesURL), true
	}
}
