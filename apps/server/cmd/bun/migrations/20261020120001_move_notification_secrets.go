package migrations

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uptrace/bun"
)

// Moves the credentials that existing rows keep inside config into the new secret column,
// so responses stop carrying them.
//
// The key list is copied here instead of imported from the application on purpose: a
// migration has to keep doing what it did the day it ran, even if the list changes later.

func init() {
	Migrations.MustRegister(moveNotificationSecrets, noNotificationSecretMove)
}

var notificationSecretKeyParts = []string{
	"password",
	"token",
	"secret",
	"api_key",
	"apikey",
	"_key",
	"webhook_url",
	"integration_url",
	"grafana_oncall_url",
	"additional_headers",
}

type notificationChannelRow struct {
	ID     string `bun:"id"`
	Config string `bun:"config"`
}

func moveNotificationSecrets(ctx context.Context, db *bun.DB) error {
	var rows []notificationChannelRow
	if err := db.NewSelect().
		Table("notification_channels").
		Column("id", "config").
		// config is JSONB on PostgreSQL and TEXT on SQLite, and comparing JSONB against
		// an empty string makes PostgreSQL reject it as invalid JSON. Empty rows are
		// skipped in the loop instead.
		Where("config IS NOT NULL").
		Scan(ctx, &rows); err != nil {
		return err
	}

	for _, row := range rows {
		if row.Config == "" {
			continue
		}
		configMap := map[string]any{}
		if err := json.Unmarshal([]byte(row.Config), &configMap); err != nil {
			// A config that cannot be parsed is left untouched rather than dropped.
			continue
		}

		secrets := map[string]any{}
		changed := false
		for key, value := range configMap {
			if !isNotificationSecretKey(key) {
				continue
			}
			delete(configMap, key)
			changed = true
			if text, ok := value.(string); ok && text != "" {
				secrets[key] = text
			}
		}
		if !changed {
			continue
		}

		config, err := json.Marshal(configMap)
		if err != nil {
			continue
		}
		if len(configMap) == 0 {
			config = []byte("{}")
		}

		query := db.NewUpdate().
			Table("notification_channels").
			Set("config = ?", string(config)).
			Where("id = ?", row.ID)
		if len(secrets) > 0 {
			secret, err := json.Marshal(secrets)
			if err != nil {
				continue
			}
			query = query.Set("secret = ?", string(secret))
		}
		if _, err := query.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// noNotificationSecretMove leaves the moved credentials in the secret column. Putting them
// back inside config would restore the exposure this migration removes.
func noNotificationSecretMove(context.Context, *bun.DB) error { return nil }

func isNotificationSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range notificationSecretKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}
