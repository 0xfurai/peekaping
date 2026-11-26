package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"
)

// Makes push tokens unique across the install (PP-146, docs/adr/0005 section 6): a push URL names
// its monitor by the token alone, and a workspace's monitors will share the database with other
// workspaces'.
//
// A push monitor holds its token twice: in push_token, which is what the push URL is looked up
// by, and in config.pushToken. A monitor saved with no token before this stored an empty
// push_token, and a monitor made through the API once could hold the token in its config only.
// So the token a monitor has is its push_token when that isn't empty, else, for a push monitor,
// its config.pushToken. A monitor with none gets NULL, since the unique index lets any number of
// NULLs in and one empty string only.
//
// Two monitors with one token can't be fixed here: the upgrade doesn't know which should keep
// it, and a new token breaks the push URL of the monitor that gets it. So it stops, before it
// changes anything (the migration check_unique_push_tokens_and_domains checks this first, before
// any other change of the upgrade, and this one checks again), and names the monitors. Their tokens stay out of the error: a token is a
// secret, and the error ends up in logs.
//
// It is done in Go because the config is JSON and SQLite and PostgreSQL don't read it the same
// way. A re-run finds nothing to change.

func init() {
	Migrations.MustRegister(makePushTokensUnique, allowSharedPushTokens)
}

type pushTokenMonitor struct {
	bun.BaseModel `bun:"table:monitors"`

	ID        string  `bun:"id"`
	Name      string  `bun:"name"`
	Type      string  `bun:"type"`
	PushToken *string `bun:"push_token"`
	Config    *string `bun:"config"`
}

// effectiveToken is the token the monitor has, or "" for none
func (m *pushTokenMonitor) effectiveToken() string {
	if m.PushToken != nil && *m.PushToken != "" {
		return *m.PushToken
	}
	if m.Type != "push" || m.Config == nil {
		return ""
	}
	var config struct {
		PushToken string `json:"pushToken"`
	}
	// A config that isn't an object of this shape has no token. Its monitor can't have been
	// pushed to by it.
	if err := json.Unmarshal([]byte(*m.Config), &config); err != nil {
		return ""
	}
	return config.PushToken
}

// sameAs is whether the stored push_token already is the token: NULL for none, else the token
func (m *pushTokenMonitor) sameAs(token string) bool {
	if token == "" {
		return m.PushToken == nil
	}
	return m.PushToken != nil && *m.PushToken == token
}

// loadPushTokenMonitors reads every monitor, in the order of their ids
func loadPushTokenMonitors(ctx context.Context, q bun.IDB) ([]pushTokenMonitor, error) {
	var monitors []pushTokenMonitor
	if err := q.NewSelect().Model(&monitors).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	return monitors, nil
}

// checkPushTokensAreUnique reads the monitors and returns an error that names the ones that share
// a token, or nil. It changes nothing. The check migration runs it before any other change of
// this upgrade, and the migration that makes the tokens unique runs it again.
func checkPushTokensAreUnique(ctx context.Context, q bun.IDB) error {
	monitors, err := loadPushTokenMonitors(ctx, q)
	if err != nil {
		return err
	}
	return sharedPushTokens(monitors)
}

func makePushTokensUnique(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		monitors, err := loadPushTokenMonitors(ctx, tx)
		if err != nil {
			return err
		}
		if err := sharedPushTokens(monitors); err != nil {
			return err
		}

		for i := range monitors {
			m := &monitors[i]
			want := m.effectiveToken()
			if m.sameAs(want) {
				continue
			}
			var value any // NULL for a monitor with no token
			if want != "" {
				value = want
			}
			if _, err := tx.NewUpdate().Table("monitors").Set("push_token = ?", value).Where("id = ?", m.ID).Exec(ctx); err != nil {
				return err
			}
		}

		_, err = tx.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_monitors_push_token ON monitors(push_token)")
		return err
	})
}

// sharedPushTokens is an error that names the monitors that share a token, one group for each
// token, or nil when no token is shared
func sharedPushTokens(monitors []pushTokenMonitor) error {
	byToken := map[string][]pushTokenMonitor{}
	for i := range monitors {
		if token := monitors[i].effectiveToken(); token != "" {
			byToken[token] = append(byToken[token], monitors[i])
		}
	}
	var groups []string
	for _, group := range byToken {
		if len(group) < 2 {
			continue
		}
		names := make([]string, len(group))
		for i, m := range group {
			names[i] = fmt.Sprintf("%q (%s)", m.Name, m.ID)
		}
		groups = append(groups, strings.Join(names, ", "))
	}
	if len(groups) == 0 {
		return nil
	}
	sort.Strings(groups)
	return fmt.Errorf("push tokens must be unique, and these monitors share one: %s. "+
		"In the previous version, give all but one monitor of each group a new push token, then upgrade again",
		strings.Join(groups, "; "))
}

// The unique index goes. The tokens that were filled in or made NULL stay: an empty token and a
// NULL one mean the same to the code before this.
func allowSharedPushTokens(ctx context.Context, db *bun.DB) error {
	_, err := db.ExecContext(ctx, "DROP INDEX IF EXISTS idx_monitors_push_token")
	return err
}
