package mongoimport

import (
	"context"
	"fmt"
	"slices"
	"time"

	"peekaping/internal/modules/workspace"

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
	// EmailVerifiedAt counts an imported account as verified, as an upgrade does for the users
	// of an install: they were never asked to verify, and nothing changes for them
	EmailVerifiedAt time.Time `bun:"email_verified_at"`
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

			EmailVerifiedAt: time.Now().UTC(),
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

type workspaceSettingRow struct {
	WorkspaceID string    `bun:"workspace_id"`
	Key         string    `bun:"key" max:"255"`
	Value       string    `bun:"value"`
	Type        string    `bun:"type" max:"50"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// unsetDataRetentionDays is the data retention period of an install that never stored one: the
// value it used when the setting was unset, which an upgrade keeps (docs/adr/0005 section 5)
const unsetDataRetentionDays = "365"

// copySettings copies the settings, including the JWT signing secrets, without
// which every session and refresh token would stop working. A setting is
// upserted by its key, so a key should have one document. If it has more, the
// one saved last wins.
//
// The data retention period and the certificate expiry notification days belong to a workspace,
// so they go to the settings of the install's workspace, replacing the defaults it started with.
// An install that never stored a data retention period keeps 365 days there, as an upgrade does.
// An install with no workspace has nothing to hold them, and they are left out.
//
// The target may have been started once, which writes its own default settings
// with new JWT signing secrets. They are deleted first, so that the install's
// secrets and settings are the only ones the target ends with.
func (c *copier) copySettings(ctx context.Context) error {
	report := c.report.collection("setting", "settings")
	if err := c.replaceTargetSettings(ctx); err != nil {
		return err
	}
	chosen, keys, err := c.chooseSettings(ctx, report)
	if err != nil {
		return err
	}
	if err := c.writeSettings(ctx, report, chosen, keys); err != nil {
		return err
	}
	return c.keepUnsetRetention(ctx, chosen)
}

// replaceTargetSettings deletes the settings a first start of the target wrote, and notes how many
func (c *copier) replaceTargetSettings(ctx context.Context) error {
	deleted, err := c.tx.ExecContext(ctx, "DELETE FROM settings")
	if err != nil {
		return fmt.Errorf("replace the settings of the target: %w", err)
	}
	if n, _ := deleted.RowsAffected(); n == 1 {
		c.report.note("the setting that the target had written when Peekaping first started on it was replaced by the install's")
	} else if n > 1 {
		c.report.note("the %d settings that the target had written when Peekaping first started on it were replaced by the install's", n)
	}
	return nil
}

// settingCandidate is the document of a setting that wins its key so far
type settingCandidate struct {
	d       doc
	id      primitive.ObjectID
	updated time.Time
}

// chooseSettings reads the settings and picks the document that wins each key: the one saved
// last. It returns the winners and the keys in the order they were first seen.
func (c *copier) chooseSettings(ctx context.Context, report *CollectionReport) (map[string]settingCandidate, []string, error) {
	chosen := map[string]settingCandidate{}
	var keys []string
	err := c.src.each(ctx, "setting", byID, func(d doc) error {
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
		chosen[key] = settingCandidate{d, id, updated}
		return nil
	})
	return chosen, keys, err
}

// writeSettings writes the winner of each key: to the settings of the workspace for a key that
// belongs to one, else to the settings of the install
func (c *copier) writeSettings(ctx context.Context, report *CollectionReport, chosen map[string]settingCandidate, keys []string) error {
	out := newBatcher[settingRow](c.tx, "settings", report)
	workspaceOut := newBatcher[workspaceSettingRow](c.tx, "workspace_settings", report)
	stored := map[string]bool{}
	workspaceKeys := []string{workspace.DataRetentionPeriodKey, workspace.CertExpiryNotifyDaysKey}
	for _, key := range keys {
		winner := chosen[key]
		var err error
		if slices.Contains(workspaceKeys, key) {
			err = c.copyWorkspaceSetting(ctx, report, workspaceOut, key, winner)
		} else {
			err = c.copyInstallSetting(ctx, report, out, stored, key, winner)
		}
		if err != nil {
			return err
		}
	}
	if err := out.flush(ctx); err != nil {
		return err
	}
	return workspaceOut.flush(ctx)
}

// copyWorkspaceSetting replaces the default of a setting of the install's workspace. Without a
// workspace there is nothing to hold it, and it is left out.
func (c *copier) copyWorkspaceSetting(ctx context.Context, report *CollectionReport, out *batcher[workspaceSettingRow], key string, winner settingCandidate) error {
	if c.workspaceID == "" {
		report.skip("a workspace setting, and the install has no workspace")
		return nil
	}
	created, updated := stamps(report, winner.d, winner.id, "created_at", "updated_at")
	if _, err := c.tx.ExecContext(ctx, "DELETE FROM workspace_settings WHERE workspace_id = ? AND key = ?", string(c.workspaceID), key); err != nil {
		return fmt.Errorf("replace the default %s of the workspace: %w", key, err)
	}
	return out.add(ctx, workspaceSettingRow{WorkspaceID: string(c.workspaceID), Key: key, Value: winner.d.str("value"), Type: winner.d.str("type"), CreatedAt: created, UpdatedAt: updated})
}

// copyInstallSetting copies a setting of the install
func (c *copier) copyInstallSetting(ctx context.Context, report *CollectionReport, out *batcher[settingRow], stored map[string]bool, key string, winner settingCandidate) error {
	created, updated := stamps(report, winner.d, winner.id, "created_at", "updated_at")
	row := settingRow{Key: c.uniqueText(report, "key", stored, key, 255), Value: winner.d.str("value"), Type: winner.d.str("type"), CreatedAt: created, UpdatedAt: updated}
	return out.add(ctx, row)
}

// keepUnsetRetention gives the workspace the 365 days of an install that never stored a data
// retention period
func (c *copier) keepUnsetRetention(ctx context.Context, chosen map[string]settingCandidate) error {
	if c.workspaceID == "" {
		return nil
	}
	if _, stored := chosen[workspace.DataRetentionPeriodKey]; !stored {
		_, err := c.tx.ExecContext(ctx, "UPDATE workspace_settings SET value = ? WHERE workspace_id = ? AND key = ?", unsetDataRetentionDays, string(c.workspaceID), workspace.DataRetentionPeriodKey)
		if err != nil {
			return fmt.Errorf("keep the data retention period of an install that never stored one: %w", err)
		}
		c.report.note("the install never stored a data retention period, so the workspace keeps %s days, as an upgraded install does", unsetDataRetentionDays)
	}
	return nil
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
