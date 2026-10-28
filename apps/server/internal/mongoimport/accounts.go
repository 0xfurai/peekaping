package mongoimport

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type userRow struct {
	ID             string    `bun:"id"`
	Email          string    `bun:"email" max:"255"`
	Password       string    `bun:"password" max:"255"`
	Active         bool      `bun:"active"`
	TwoFASecret    string    `bun:"twofa_secret" max:"64"`
	TwoFAStatus    bool      `bun:"twofa_status"`
	TwoFALastToken string    `bun:"twofa_last_token" max:"6"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// copyUsers copies the users with their password hashes and two-factor secrets,
// so they log in as before. Users keep their timestamps under createdAt and
// updatedAt, unlike every other collection.
func (c *copier) copyUsers(ctx context.Context) error {
	report := c.report.collection("users", "users")
	out := newBatcher[userRow](c.tx, "users", report)
	emails, stored := map[string]bool{}, map[string]bool{}
	err := c.src.each(ctx, "users", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		email := d.str("email")
		if emails[email] {
			report.skip("email already used by an earlier user")
			return nil
		}
		emails[email] = true
		created, updated := stamps(report, d, id, "createdAt", "updatedAt")
		return out.add(ctx, userRow{
			ID:             uuidOf(id),
			Email:          c.uniqueText(report, "email", stored, email, 255),
			Password:       d.str("password"),
			Active:         d.boolean("active"),
			TwoFASecret:    d.str("twofa_secret"),
			TwoFAStatus:    d.boolean("twofa_status"),
			TwoFALastToken: d.str("twofa_last_token"),
			CreatedAt:      created,
			UpdatedAt:      updated,
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}

type settingRow struct {
	Key       string    `bun:"key" max:"255"`
	Value     string    `bun:"value"`
	Type      string    `bun:"type" max:"50"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// copySettings copies the settings, including the JWT signing secrets, without
// which every session and refresh token would stop working. A setting is
// upserted by its key, so a key should have one document. If it has more, the
// one saved last wins.
//
// The target may have been started once, which writes its own default settings
// with new JWT signing secrets. They are deleted first, so that the install's
// secrets and settings are the only ones the target ends with.
func (c *copier) copySettings(ctx context.Context) error {
	report := c.report.collection("setting", "settings")
	deleted, err := c.tx.ExecContext(ctx, "DELETE FROM settings")
	if err != nil {
		return fmt.Errorf("replace the settings of the target: %w", err)
	}
	if n, _ := deleted.RowsAffected(); n == 1 {
		c.report.note("the setting that the target had written when Peekaping first started on it was replaced by the install's")
	} else if n > 1 {
		c.report.note("the %d settings that the target had written when Peekaping first started on it were replaced by the install's", n)
	}
	type candidate struct {
		d       doc
		id      primitive.ObjectID
		updated time.Time
	}
	chosen := map[string]candidate{}
	var keys []string
	err = c.src.each(ctx, "setting", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		key := d.str("key")
		updated, _ := d.when("updated_at")
		earlier, seen := chosen[key]
		if !seen {
			keys = append(keys, key)
		} else {
			report.skip("an older document of the same key")
			if !updated.After(earlier.updated) {
				return nil
			}
		}
		chosen[key] = candidate{d, id, updated}
		return nil
	})
	if err != nil {
		return err
	}
	out := newBatcher[settingRow](c.tx, "settings", report)
	stored := map[string]bool{}
	for _, key := range keys {
		winner := chosen[key]
		created, updated := stamps(report, winner.d, winner.id, "created_at", "updated_at")
		row := settingRow{Key: c.uniqueText(report, "key", stored, key, 255), Value: winner.d.str("value"), Type: winner.d.str("type"), CreatedAt: created, UpdatedAt: updated}
		if err := out.add(ctx, row); err != nil {
			return err
		}
	}
	return out.flush(ctx)
}

type apiKeyRow struct {
	ID            string     `bun:"id"`
	WorkspaceID   string     `bun:"workspace_id"`
	Name          string     `bun:"name" max:"255"`
	KeyHash       string     `bun:"key_hash" max:"255"`
	DisplayKey    string     `bun:"display_key" max:"20"`
	LastUsed      *time.Time `bun:"last_used"`
	ExpiresAt     *time.Time `bun:"expires_at"`
	UsageCount    int64      `bun:"usage_count"`
	MaxUsageCount *int64     `bun:"max_usage_count"`
	CreatedAt     time.Time  `bun:"created_at"`
	UpdatedAt     time.Time  `bun:"updated_at"`
}

// copyAPIKeys copies the API keys with their hashes. The tokens in use carry the
// ObjectID of their key, which key validation maps to the UUID the key gets here.
func (c *copier) copyAPIKeys(ctx context.Context) error {
	report := c.report.collection("api_keys", "api_keys")
	out := newBatcher[apiKeyRow](c.tx, "api_keys", report)
	err := c.src.each(ctx, "api_keys", byID, func(d doc) error {
		report.Read++
		id, ok := d.id()
		if !ok {
			report.skip(reasonNoObjectID)
			return nil
		}
		created, updated := stamps(report, d, id, "createdAt", "updatedAt")
		return out.add(ctx, apiKeyRow{
			ID:            uuidOf(id),
			WorkspaceID:   string(c.workspaceID),
			Name:          d.str("name"),
			KeyHash:       d.str("key_hash"),
			DisplayKey:    d.str("display_key"),
			LastUsed:      d.whenPtr("last_used"),
			ExpiresAt:     d.whenPtr("expires_at"),
			UsageCount:    d.integer("usage_count"),
			MaxUsageCount: d.int64Ptr("max_usage_count"),
			CreatedAt:     created,
			UpdatedAt:     updated,
		})
	})
	if err != nil {
		return err
	}
	return out.flush(ctx)
}
