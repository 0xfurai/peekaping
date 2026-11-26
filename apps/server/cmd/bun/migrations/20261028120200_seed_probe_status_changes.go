package migrations

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Starts the history of every probe that exists at the upgrade (PP-330): one row in
// probe_status_changes with the status the probe has now, at the time of the upgrade. What the
// probe did before is not known.
//
// It is done in Go and not in SQL because the rows need ids, and SQLite and PostgreSQL don't make
// them the same way. It also writes the times the way the server does, so they read back the same.
// A probe that already has a row, as after a re-run, is left alone.

func init() {
	Migrations.MustRegister(seedProbeStatusChanges, clearProbeStatusChanges)
}

type seedProbe struct {
	bun.BaseModel `bun:"table:probes,alias:p"`

	ID     string `bun:"id"`
	Status string `bun:"status"`
}

type seedStatusChange struct {
	bun.BaseModel `bun:"table:probe_status_changes"`

	ID         string    `bun:"id,pk"`
	ProbeID    string    `bun:"probe_id"`
	Status     string    `bun:"status"`
	At         time.Time `bun:"at"`
	RecordedAt time.Time `bun:"recorded_at"`
}

func seedProbeStatusChanges(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var probes []seedProbe
		err := tx.NewSelect().Model(&probes).
			Where("NOT EXISTS (SELECT 1 FROM probe_status_changes c WHERE c.probe_id = p.id)").
			Order("p.id").
			Scan(ctx)
		if err != nil || len(probes) == 0 {
			return err
		}
		now := time.Now().UTC()
		changes := make([]seedStatusChange, len(probes))
		for i, p := range probes {
			changes[i] = seedStatusChange{ID: uuid.NewString(), ProbeID: p.ID, Status: p.Status, At: now, RecordedAt: now}
		}
		_, err = tx.NewInsert().Model(&changes).Exec(ctx)
		return err
	})
}

// The table goes with the migration before this one, so there is nothing to undo here beyond
// emptying it
func clearProbeStatusChanges(ctx context.Context, db *bun.DB) error {
	_, err := db.ExecContext(ctx, "DELETE FROM probe_status_changes")
	return err
}
