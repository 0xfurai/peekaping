package probe_notification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:probe_notification_channels,alias:pnc"`

	ProbeID               string    `bun:"probe_id,pk"`
	NotificationChannelID string    `bun:"notification_channel_id,pk"`
	CreatedAt             time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) FindByProbeID(ctx context.Context, probeID string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().Model(&sms).
		Where("probe_id = ?", probeID).
		Order("created_at ASC", "notification_channel_id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]*Model, len(sms))
	for i, sm := range sms {
		models[i] = &Model{ProbeID: sm.ProbeID, NotificationID: sm.NotificationChannelID, CreatedAt: sm.CreatedAt}
	}
	return models, nil
}

func (r *SQLRepositoryImpl) Replace(ctx context.Context, probeID string, notificationIDs []string) error {
	ids, err := canonicalChannelIDs(notificationIDs)
	if err != nil {
		return err
	}
	// A transaction, so that a probe is never left with half of the channels it was given. Inside
	// it, only tx is used: SQLite has one connection, which the transaction holds.
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if len(ids) > 0 {
			var found []string
			err := tx.NewSelect().Table("notification_channels").Column("id").Where("id IN (?)", bun.In(ids)).Scan(ctx, &found)
			if err != nil {
				return err
			}
			exists := make(map[string]bool, len(found))
			for _, id := range found {
				exists[id] = true
			}
			for _, id := range ids {
				if !exists[id] {
					return fmt.Errorf("%w: %s", ErrUnknownChannel, id)
				}
			}
		}
		if _, err := tx.NewDelete().Model((*sqlModel)(nil)).Where("probe_id = ?", probeID).Exec(ctx); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		now := time.Now().UTC()
		links := make([]*sqlModel, len(ids))
		for i, id := range ids {
			links[i] = &sqlModel{ProbeID: probeID, NotificationChannelID: id, CreatedAt: now}
		}
		_, err := tx.NewInsert().Model(&links).Exec(ctx)
		return err
	})
}

func (r *SQLRepositoryImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("notification_channel_id = ?", notificationID).Exec(ctx)
	return err
}

// canonicalChannelIDs returns the ids once each, as lower case UUIDs, in the order given. An id
// that isn't a UUID can't be a channel on a SQL install, and fails with ErrUnknownChannel.
func canonicalChannelIDs(ids []string) ([]string, error) {
	seen := make(map[string]bool, len(ids))
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnknownChannel, id)
		}
		key := parsed.String()
		if !seen[key] {
			seen[key] = true
			canonical = append(canonical, key)
		}
	}
	return canonical, nil
}
